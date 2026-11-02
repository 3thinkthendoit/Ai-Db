package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AI-DB/internal/config"
	"AI-DB/internal/db"
	"AI-DB/internal/store"

	"github.com/google/uuid"
)

func roleBypassesGrants(role string) bool {
	return role == "admin" || role == "dba"
}

func (s *Service) grantStore() store.QueryGrantStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grants
}

// resolveConnLocked returns an owned connection, or a granted catalog connection.
// Caller must hold s.mu.
func (s *Service) resolveConnLocked(ctx context.Context, p Principal, id string) (config.Connection, error) {
	if c, err := s.findConnLocked(ctx, p, id); err == nil {
		return c, nil
	}
	if s.grants == nil || roleBypassesGrants(p.Role) {
		// admin/dba without ownership: still try catalog load for shared ops
		if s.grants != nil && roleBypassesGrants(p.Role) {
			meta, err := s.grants.GetConnectionByID(ctx, id)
			if err != nil {
				return config.Connection{}, fmt.Errorf("connection not found: %s", id)
			}
			return meta.ToConfigConnection(), nil
		}
		return config.Connection{}, fmt.Errorf("connection not found: %s", id)
	}
	ids, err := s.grants.ListGrantedConnectionIDs(ctx, p.UserID)
	if err != nil {
		return config.Connection{}, err
	}
	ok := false
	for _, x := range ids {
		if x == id {
			ok = true
			break
		}
	}
	if !ok {
		return config.Connection{}, fmt.Errorf("connection not found: %s", id)
	}
	meta, err := s.grants.GetConnectionByID(ctx, id)
	if err != nil {
		return config.Connection{}, err
	}
	return meta.ToConfigConnection(), nil
}

func (s *Service) filterObjectsByGrants(ctx context.Context, p Principal, connectionID string, nodes []db.ObjectNode) ([]db.ObjectNode, error) {
	if roleBypassesGrants(p.Role) || s.grantStore() == nil {
		return nodes, nil
	}
	grants, err := s.grants.ListGrants(ctx, p.UserID, 500)
	if err != nil {
		return nil, err
	}
	allowedDB := map[string]bool{}
	for _, g := range grants {
		if g.ConnectionID != connectionID {
			continue
		}
		allowedDB[g.DatabaseName] = true
	}
	if len(allowedDB) == 0 {
		return []db.ObjectNode{}, nil
	}
	out := make([]db.ObjectNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Kind == "database" || n.Kind == "schema" {
			if allowedDB[n.Name] {
				out = append(out, n)
			}
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) filterExpandByGrants(ctx context.Context, p Principal, connectionID string, path []string, nodes []db.ObjectNode) ([]db.ObjectNode, error) {
	if roleBypassesGrants(p.Role) || s.grantStore() == nil {
		return nodes, nil
	}
	if len(path) == 0 {
		return nodes, nil
	}
	dbName := path[0]
	grants, err := s.grants.ListGrants(ctx, p.UserID, 500)
	if err != nil {
		return nil, err
	}
	allTables := false
	allowed := map[string]bool{}
	for _, g := range grants {
		if g.ConnectionID != connectionID || g.DatabaseName != dbName {
			continue
		}
		if g.TableName == "*" {
			allTables = true
			break
		}
		allowed[g.TableName] = true
	}
	if allTables {
		return nodes, nil
	}
	if len(allowed) == 0 {
		return []db.ObjectNode{}, nil
	}
	// Filter table/view/collection leaves; keep folders.
	out := make([]db.ObjectNode, 0, len(nodes))
	for _, n := range nodes {
		switch n.Kind {
		case "table", "view", "collection":
			if allowed[n.Name] {
				out = append(out, n)
			}
		default:
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *Service) requireSchemaGrant(ctx context.Context, p Principal, connectionID, schema string) error {
	if roleBypassesGrants(p.Role) || s.grantStore() == nil {
		return nil
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return fmt.Errorf("no schema selected; grant required")
	}
	ok, err := s.grants.HasGrant(ctx, p.UserID, connectionID, schema, "*")
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	// Any table grant in this database also permits schema-scoped reads (tree already filtered).
	grants, err := s.grants.ListGrants(ctx, p.UserID, 500)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.ConnectionID == connectionID && g.DatabaseName == schema {
			return nil
		}
	}
	return fmt.Errorf("no query grant for database %q", schema)
}

// GrantView is the API projection of a query grant.
type GrantView struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"createdAt"`
	GranteeID     string `json:"granteeId"`
	GranteeName   string `json:"granteeName"`
	ConnectionID  string `json:"connectionId"`
	DatabaseName  string `json:"databaseName"`
	TableName     string `json:"tableName"`
	GrantedByName string `json:"grantedByName"`
}

// PrivilegeRequestView is the API projection of a privilege application.
type PrivilegeRequestView struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	ApplicantID   string `json:"applicantId"`
	ApplicantName string `json:"applicantName"`
	ConnectionID  string `json:"connectionId"`
	DatabaseName  string `json:"databaseName"`
	TableName     string `json:"tableName"`
	Remark        string `json:"remark"`
	Status        string `json:"status"`
	ReviewerName  string `json:"reviewerName"`
	ReviewNote    string `json:"reviewNote"`
}

// ConnectionCatalogItem is a shared connection visible for privilege apply / admin.
type ConnectionCatalogItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

func grantView(g store.QueryGrant) GrantView {
	return GrantView{
		ID: g.ID, GranteeID: g.GranteeID, GranteeName: g.GranteeName,
		ConnectionID: g.ConnectionID, DatabaseName: g.DatabaseName, TableName: g.TableName,
		GrantedByName: g.GrantedByName,
		CreatedAt:     g.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func privView(r store.PrivilegeRequest) PrivilegeRequestView {
	return PrivilegeRequestView{
		ID: r.ID, ApplicantID: r.ApplicantID, ApplicantName: r.ApplicantName,
		ConnectionID: r.ConnectionID, DatabaseName: r.DatabaseName, TableName: r.TableName,
		Remark: r.Remark, Status: r.Status, ReviewerName: r.ReviewerName, ReviewNote: r.ReviewNote,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// ListConnectionCatalog returns connections for privilege apply (no secrets).
func (s *Service) ListConnectionCatalog(ctx context.Context, p Principal) ([]ConnectionCatalogItem, error) {
	gs := s.grantStore()
	if gs == nil {
		// Desktop / no grant store: fall back to caller's own connections.
		conns, err := s.ListConnections(ctx, p)
		if err != nil {
			return nil, err
		}
		out := make([]ConnectionCatalogItem, 0, len(conns))
		for _, c := range conns {
			out = append(out, ConnectionCatalogItem{ID: c.ID, Name: c.Name, Type: string(c.Type), Host: c.Host, Port: c.Port})
		}
		return out, nil
	}
	metas, err := gs.ListAllConnections(ctx, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionCatalogItem, 0, len(metas))
	for _, m := range metas {
		out = append(out, ConnectionCatalogItem{ID: m.ID, Name: m.Name, Type: m.Type, Host: m.Host, Port: m.Port})
	}
	return out, nil
}

// ListGrants lists grants. Admin/DBA see all (empty grantee filter); users see own.
func (s *Service) ListGrants(ctx context.Context, p Principal, limit int) ([]GrantView, error) {
	gs := s.grantStore()
	if gs == nil {
		return nil, fmt.Errorf("grant store not available")
	}
	grantee := p.UserID
	if roleBypassesGrants(p.Role) {
		grantee = ""
	}
	rows, err := gs.ListGrants(ctx, grantee, limit)
	if err != nil {
		return nil, err
	}
	out := make([]GrantView, 0, len(rows))
	for _, g := range rows {
		out = append(out, grantView(g))
	}
	return out, nil
}

// SubmitPrivilegeRequest creates pending table access applications (one per table).
func (s *Service) SubmitPrivilegeRequest(ctx context.Context, p Principal, connectionID, databaseName, tablesCSV, remark string) error {
	gs := s.grantStore()
	if gs == nil {
		return fmt.Errorf("grant store not available")
	}
	connectionID = strings.TrimSpace(connectionID)
	databaseName = strings.TrimSpace(databaseName)
	if connectionID == "" || databaseName == "" {
		return fmt.Errorf("connection and database are required")
	}
	if _, err := gs.GetConnectionByID(ctx, connectionID); err != nil {
		return fmt.Errorf("connection not found")
	}
	tables := splitTableList(tablesCSV)
	if len(tables) == 0 {
		return fmt.Errorf("at least one table is required")
	}
	for _, table := range tables {
		row := store.PrivilegeRequest{
			ID: uuid.NewString(), ApplicantID: p.UserID, ApplicantName: p.Username,
			ConnectionID: connectionID, DatabaseName: databaseName, TableName: table,
			Remark: strings.TrimSpace(remark), Status: "pending",
		}
		if err := gs.InsertPrivilegeRequest(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func splitTableList(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// ListPrivilegeRequests lists applications. Admin/DBA see all unless mineOnly.
func (s *Service) ListPrivilegeRequests(ctx context.Context, p Principal, status string, limit int, mineOnly bool) ([]PrivilegeRequestView, error) {
	gs := s.grantStore()
	if gs == nil {
		return nil, fmt.Errorf("grant store not available")
	}
	applicant := p.UserID
	if roleBypassesGrants(p.Role) && !mineOnly {
		applicant = ""
	}
	rows, err := gs.ListPrivilegeRequests(ctx, applicant, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]PrivilegeRequestView, 0, len(rows))
	for _, r := range rows {
		out = append(out, privView(r))
	}
	return out, nil
}

// ApprovePrivilegeRequest grants table access (admin or dba).
// Grant is written first, then status is CAS-flipped pending→approved so a failed
// grant insert never leaves an approved ticket without access, and concurrent
// reviewers cannot double-claim.
func (s *Service) ApprovePrivilegeRequest(ctx context.Context, p Principal, id, note string) error {
	if !roleBypassesGrants(p.Role) {
		return fmt.Errorf("only admin or dba can approve privilege requests")
	}
	gs := s.grantStore()
	if gs == nil {
		return fmt.Errorf("grant store not available")
	}
	r, err := gs.GetPrivilegeRequest(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != "pending" {
		return fmt.Errorf("request is %s, not pending", r.Status)
	}
	note = strings.TrimSpace(note)
	if err := gs.InsertGrant(ctx, store.QueryGrant{
		ID: uuid.NewString(), GranteeID: r.ApplicantID, GranteeName: r.ApplicantName,
		ConnectionID: r.ConnectionID, DatabaseName: r.DatabaseName, TableName: r.TableName,
		GrantedByID: p.UserID, GrantedByName: p.Username,
	}); err != nil {
		return err
	}
	ok, err := gs.ClaimPrivilegeRequestStatus(ctx, id, "pending", "approved", p.UserID, p.Username, note)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("request already claimed by another reviewer")
	}
	return nil
}

// RejectPrivilegeRequest rejects a pending application.
func (s *Service) RejectPrivilegeRequest(ctx context.Context, p Principal, id, note string) error {
	if !roleBypassesGrants(p.Role) {
		return fmt.Errorf("only admin or dba can reject privilege requests")
	}
	gs := s.grantStore()
	if gs == nil {
		return fmt.Errorf("grant store not available")
	}
	r, err := gs.GetPrivilegeRequest(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != "pending" {
		return fmt.Errorf("request is %s, not pending", r.Status)
	}
	ok, err := gs.ClaimPrivilegeRequestStatus(ctx, id, "pending", "rejected", p.UserID, p.Username, strings.TrimSpace(note))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("request already claimed by another reviewer")
	}
	return nil
}

// RevokeGrant deletes an effective grant (admin or dba).
func (s *Service) RevokeGrant(ctx context.Context, p Principal, id string) error {
	if !roleBypassesGrants(p.Role) {
		return fmt.Errorf("only admin or dba can revoke grants")
	}
	gs := s.grantStore()
	if gs == nil {
		return fmt.Errorf("grant store not available")
	}
	return gs.DeleteGrant(ctx, id)
}
