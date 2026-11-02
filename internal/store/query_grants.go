package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"AI-DB/internal/config"
)

// QueryGrant is an effective table-level query privilege.
type QueryGrant struct {
	ID            string
	CreatedAt     time.Time
	GranteeID     string
	GranteeName   string
	ConnectionID  string
	DatabaseName  string
	TableName     string // "*" = all tables in database
	GrantedByID   string
	GrantedByName string
}

// PrivilegeRequest is a pending/decided table access application.
type PrivilegeRequest struct {
	ID             string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ApplicantID    string
	ApplicantName  string
	ConnectionID   string
	DatabaseName   string
	TableName      string
	Remark         string
	Status         string
	ReviewerID     string
	ReviewerName   string
	ReviewNote     string
}

// ConnectionMeta is a connection row loadable across owners (for grant holders).
type ConnectionMeta struct {
	ID         string
	OwnerID    string
	Name       string
	Type       string
	Host       string
	Port       int
	User       string
	Password   string
	Database   string
	AuthSource string
	SSLMode    string
}

// QueryGrantStore is server-only privilege + cross-owner connection lookup.
type QueryGrantStore interface {
	InsertGrant(ctx context.Context, g QueryGrant) error
	DeleteGrant(ctx context.Context, id string) error
	ListGrants(ctx context.Context, granteeID string, limit int) ([]QueryGrant, error)
	HasGrant(ctx context.Context, granteeID, connectionID, databaseName, tableName string) (bool, error)
	ListGrantedConnectionIDs(ctx context.Context, granteeID string) ([]string, error)
	InsertPrivilegeRequest(ctx context.Context, r PrivilegeRequest) error
	GetPrivilegeRequest(ctx context.Context, id string) (*PrivilegeRequest, error)
	ListPrivilegeRequests(ctx context.Context, applicantID, status string, limit int) ([]PrivilegeRequest, error)
	UpdatePrivilegeRequest(ctx context.Context, r PrivilegeRequest) error
	// ClaimPrivilegeRequestStatus CAS-updates status; returns false if from-status did not match.
	ClaimPrivilegeRequestStatus(ctx context.Context, id, fromStatus, toStatus, reviewerID, reviewerName, note string) (bool, error)
	GetConnectionByID(ctx context.Context, id string) (*ConnectionMeta, error)
	ListAllConnections(ctx context.Context, limit int) ([]ConnectionMeta, error)
}

func (s *MySQLStore) InsertGrant(ctx context.Context, g QueryGrant) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO query_grants
  (id, created_at, grantee_id, grantee_name, connection_id, database_name, table_name, granted_by_id, granted_by_name)
VALUES (?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE granted_by_id=VALUES(granted_by_id), granted_by_name=VALUES(granted_by_name), created_at=VALUES(created_at)`,
		g.ID, g.CreatedAt, g.GranteeID, g.GranteeName, g.ConnectionID, g.DatabaseName, g.TableName, g.GrantedByID, g.GrantedByName)
	return err
}

func (s *MySQLStore) DeleteGrant(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM query_grants WHERE id = ?`, id)
	return err
}

func (s *MySQLStore) ListGrants(ctx context.Context, granteeID string, limit int) ([]QueryGrant, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if granteeID == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, created_at, grantee_id, grantee_name, connection_id, database_name, table_name, granted_by_id, granted_by_name
FROM query_grants ORDER BY created_at DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, created_at, grantee_id, grantee_name, connection_id, database_name, table_name, granted_by_id, granted_by_name
FROM query_grants WHERE grantee_id = ? ORDER BY created_at DESC LIMIT ?`, granteeID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueryGrant
	for rows.Next() {
		var g QueryGrant
		if err := rows.Scan(&g.ID, &g.CreatedAt, &g.GranteeID, &g.GranteeName, &g.ConnectionID, &g.DatabaseName, &g.TableName, &g.GrantedByID, &g.GrantedByName); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *MySQLStore) HasGrant(ctx context.Context, granteeID, connectionID, databaseName, tableName string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1) FROM query_grants
WHERE grantee_id = ? AND connection_id = ? AND database_name = ?
  AND (table_name = '*' OR table_name = ?)`,
		granteeID, connectionID, databaseName, tableName).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *MySQLStore) ListGrantedConnectionIDs(ctx context.Context, granteeID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT connection_id FROM query_grants WHERE grantee_id = ?`, granteeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *MySQLStore) InsertPrivilegeRequest(ctx context.Context, r PrivilegeRequest) error {
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO query_privilege_requests
  (id, created_at, updated_at, applicant_id, applicant_name, connection_id, database_name, table_name, remark, status, reviewer_id, reviewer_name, review_note)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.CreatedAt, r.UpdatedAt, r.ApplicantID, r.ApplicantName, r.ConnectionID, r.DatabaseName, r.TableName,
		r.Remark, r.Status, r.ReviewerID, r.ReviewerName, r.ReviewNote)
	return err
}

func (s *MySQLStore) GetPrivilegeRequest(ctx context.Context, id string) (*PrivilegeRequest, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, created_at, updated_at, applicant_id, applicant_name, connection_id, database_name, table_name, remark, status, reviewer_id, reviewer_name, review_note
FROM query_privilege_requests WHERE id = ?`, id)
	var r PrivilegeRequest
	if err := row.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.ApplicantID, &r.ApplicantName, &r.ConnectionID, &r.DatabaseName, &r.TableName,
		&r.Remark, &r.Status, &r.ReviewerID, &r.ReviewerName, &r.ReviewNote); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *MySQLStore) ListPrivilegeRequests(ctx context.Context, applicantID, status string, limit int) ([]PrivilegeRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
SELECT id, created_at, updated_at, applicant_id, applicant_name, connection_id, database_name, table_name, remark, status, reviewer_id, reviewer_name, review_note
FROM query_privilege_requests WHERE 1=1`
	args := []any{}
	if applicantID != "" {
		q += " AND applicant_id = ?"
		args = append(args, applicantID)
	}
	if status != "" {
		q += " AND status = ?"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrivilegeRequest
	for rows.Next() {
		var r PrivilegeRequest
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.ApplicantID, &r.ApplicantName, &r.ConnectionID, &r.DatabaseName, &r.TableName,
			&r.Remark, &r.Status, &r.ReviewerID, &r.ReviewerName, &r.ReviewNote); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) UpdatePrivilegeRequest(ctx context.Context, r PrivilegeRequest) error {
	r.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE query_privilege_requests SET updated_at=?, status=?, reviewer_id=?, reviewer_name=?, review_note=? WHERE id=?`,
		r.UpdatedAt, r.Status, r.ReviewerID, r.ReviewerName, r.ReviewNote, r.ID)
	return err
}

func (s *MySQLStore) ClaimPrivilegeRequestStatus(ctx context.Context, id, fromStatus, toStatus, reviewerID, reviewerName, note string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE query_privilege_requests
SET updated_at=?, status=?, reviewer_id=?, reviewer_name=?, review_note=?
WHERE id=? AND status=?`,
		time.Now().UTC(), toStatus, reviewerID, reviewerName, note, id, fromStatus)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *MySQLStore) GetConnectionByID(ctx context.Context, id string) (*ConnectionMeta, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, owner_id, name, type, host, port, user, password, `+"`database`"+`, auth_source, ssl_mode
FROM connections WHERE id = ?`, id)
	var m ConnectionMeta
	var encPass string
	if err := row.Scan(&m.ID, &m.OwnerID, &m.Name, &m.Type, &m.Host, &m.Port, &m.User, &encPass, &m.Database, &m.AuthSource, &m.SSLMode); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("connection not found: %s", id)
		}
		return nil, err
	}
	pass, err := s.cipher.Decrypt(encPass)
	if err != nil {
		return nil, fmt.Errorf("decrypt connection password: %w", err)
	}
	m.Password = pass
	return &m, nil
}

func (s *MySQLStore) ListAllConnections(ctx context.Context, limit int) ([]ConnectionMeta, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, owner_id, name, type, host, port, user, password, `+"`database`"+`, auth_source, ssl_mode
FROM connections ORDER BY name, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectionMeta
	for rows.Next() {
		var m ConnectionMeta
		var encPass string
		if err := rows.Scan(&m.ID, &m.OwnerID, &m.Name, &m.Type, &m.Host, &m.Port, &m.User, &encPass, &m.Database, &m.AuthSource, &m.SSLMode); err != nil {
			return nil, err
		}
		// Catalog listing never returns secrets to callers of ListAllConnections — leave empty.
		m.Password = ""
		out = append(out, m)
	}
	return out, rows.Err()
}

// ToConfigConnection maps meta into config.Connection (with password when loaded).
func (m ConnectionMeta) ToConfigConnection() config.Connection {
	return config.Connection{
		ID: m.ID, Name: m.Name, Type: config.ConnType(m.Type),
		Host: m.Host, Port: m.Port, User: m.User, Password: m.Password,
		Database: m.Database, AuthSource: m.AuthSource, SSLMode: m.SSLMode,
	}
}
