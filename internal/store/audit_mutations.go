package store

import (
	"context"
	"database/sql"
	"time"
)

// QueryAuditRow is one durable query-audit entry.
type QueryAuditRow struct {
	ID           string
	TS           time.Time
	ActorID      string
	ActorName    string
	ActorRole    string
	ConnectionID string
	SQLText      string
	Mutating     bool
	Outcome      string
	Detail       string
	DurationMs   int64
	MutationID   string
}

// MutationRow is a pending/executed mutation request.
type MutationRow struct {
	ID            string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	OwnerID       string
	OwnerName     string
	ConnectionID  string
	ConnType      string
	ActiveSchema  string
	SQLText       string
	StmtClass     string
	Status        string
	Rollbackable  bool
	BeforeImage   string
	ReviewerID    string
	ReviewerName  string
	ReviewNote    string
	ExecutedAt    *time.Time
	RolledBackAt  *time.Time
}

// InsertQueryAudit appends one audit row.
func (s *MySQLStore) InsertQueryAudit(ctx context.Context, row QueryAuditRow) error {
	if row.TS.IsZero() {
		row.TS = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO query_audit
  (id, ts, actor_id, actor_name, actor_role, connection_id, sql_text, mutating, outcome, detail, duration_ms, mutation_id)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.TS, row.ActorID, row.ActorName, row.ActorRole, row.ConnectionID,
		row.SQLText, boolToTiny(row.Mutating), row.Outcome, truncateRunes(row.Detail, 512),
		row.DurationMs, row.MutationID)
	return err
}

// ListQueryAudit returns newest-first audit rows. empty actorID = all (DBA).
func (s *MySQLStore) ListQueryAudit(ctx context.Context, actorID string, limit int) ([]QueryAuditRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if actorID == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, ts, actor_id, actor_name, actor_role, connection_id, sql_text, mutating, outcome, detail, duration_ms, mutation_id
FROM query_audit ORDER BY ts DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT id, ts, actor_id, actor_name, actor_role, connection_id, sql_text, mutating, outcome, detail, duration_ms, mutation_id
FROM query_audit WHERE actor_id = ? ORDER BY ts DESC LIMIT ?`, actorID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueryAuditRow
	for rows.Next() {
		var r QueryAuditRow
		var mut int
		if err := rows.Scan(&r.ID, &r.TS, &r.ActorID, &r.ActorName, &r.ActorRole, &r.ConnectionID,
			&r.SQLText, &mut, &r.Outcome, &r.Detail, &r.DurationMs, &r.MutationID); err != nil {
			return nil, err
		}
		r.Mutating = mut != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertMutation creates a pending mutation request.
func (s *MySQLStore) InsertMutation(ctx context.Context, row MutationRow) error {
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	if row.Status == "" {
		row.Status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mutation_requests
  (id, created_at, updated_at, owner_id, owner_name, connection_id, conn_type, active_schema, sql_text, stmt_class,
   status, rollbackable, before_image, reviewer_id, reviewer_name, review_note, executed_at, rolled_back_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.CreatedAt, row.UpdatedAt, row.OwnerID, row.OwnerName, row.ConnectionID, row.ConnType, row.ActiveSchema,
		row.SQLText, row.StmtClass, row.Status, boolToTiny(row.Rollbackable), nullStr(row.BeforeImage),
		row.ReviewerID, row.ReviewerName, row.ReviewNote, row.ExecutedAt, row.RolledBackAt)
	return err
}

// GetMutation loads one mutation by id.
func (s *MySQLStore) GetMutation(ctx context.Context, id string) (*MutationRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, created_at, updated_at, owner_id, owner_name, connection_id, conn_type, active_schema, sql_text, stmt_class,
       status, rollbackable, before_image, reviewer_id, reviewer_name, review_note, executed_at, rolled_back_at
FROM mutation_requests WHERE id = ?`, id)
	return scanMutation(row)
}

// ListMutations lists by status. empty status = all. empty ownerID = all owners (DBA).
func (s *MySQLStore) ListMutations(ctx context.Context, ownerID, status string, limit int) ([]MutationRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
SELECT id, created_at, updated_at, owner_id, owner_name, connection_id, conn_type, active_schema, sql_text, stmt_class,
       status, rollbackable, before_image, reviewer_id, reviewer_name, review_note, executed_at, rolled_back_at
FROM mutation_requests WHERE 1=1`
	args := []any{}
	if ownerID != "" {
		q += " AND owner_id = ?"
		args = append(args, ownerID)
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
	var out []MutationRow
	for rows.Next() {
		m, err := scanMutationRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// UpdateMutation persists status / snapshot / reviewer fields.
func (s *MySQLStore) UpdateMutation(ctx context.Context, row MutationRow) error {
	row.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE mutation_requests SET
  updated_at=?, status=?, rollbackable=?, before_image=?,
  reviewer_id=?, reviewer_name=?, review_note=?, executed_at=?, rolled_back_at=?
WHERE id=?`,
		row.UpdatedAt, row.Status, boolToTiny(row.Rollbackable), nullStr(row.BeforeImage),
		row.ReviewerID, row.ReviewerName, row.ReviewNote, row.ExecutedAt, row.RolledBackAt, row.ID)
	return err
}

// ClaimMutationStatus atomically moves a row from fromStatus to toStatus.
// Returns false when no row matched (lost the race or wrong state).
func (s *MySQLStore) ClaimMutationStatus(ctx context.Context, id, fromStatus, toStatus, reviewerID, reviewerName string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE mutation_requests SET status=?, reviewer_id=?, reviewer_name=?, updated_at=?
WHERE id=? AND status=?`,
		toStatus, reviewerID, reviewerName, time.Now().UTC(), id, fromStatus)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanMutation(row scannable) (*MutationRow, error) {
	var m MutationRow
	var rb int
	var before sql.NullString
	var execAt, rbAt sql.NullTime
	if err := row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt, &m.OwnerID, &m.OwnerName, &m.ConnectionID, &m.ConnType,
		&m.ActiveSchema, &m.SQLText, &m.StmtClass, &m.Status, &rb, &before, &m.ReviewerID, &m.ReviewerName, &m.ReviewNote,
		&execAt, &rbAt); err != nil {
		return nil, err
	}
	m.Rollbackable = rb != 0
	if before.Valid {
		m.BeforeImage = before.String
	}
	if execAt.Valid {
		t := execAt.Time
		m.ExecutedAt = &t
	}
	if rbAt.Valid {
		t := rbAt.Time
		m.RolledBackAt = &t
	}
	return &m, nil
}

func scanMutationRows(rows *sql.Rows) (*MutationRow, error) {
	return scanMutation(rows)
}

func boolToTiny(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
