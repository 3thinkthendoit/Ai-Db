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

// dmlSnapshotDriver is implemented by MySQL/Postgres drivers.
type dmlSnapshotDriver interface {
	ExecuteDMLSnapshot(ctx context.Context, query string) (*db.QueryResult, *db.RowSnapshot, error)
	ExecuteRollbackSnapshot(ctx context.Context, snap *db.RowSnapshot) (*db.QueryResult, error)
}

func execDDLStatements(ctx context.Context, drv db.Driver, sqlText string) (*db.QueryResult, error) {
	parts := strings.Split(sqlText, ";")
	var last *db.QueryResult
	ran := 0
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		res, err := drv.Execute(ctx, stmt, 1, true)
		if err != nil {
			return nil, err
		}
		last = res
		ran++
	}
	if ran == 0 {
		return nil, fmt.Errorf("empty DDL")
	}
	if last == nil {
		return &db.QueryResult{Message: fmt.Sprintf("OK, %d statements", ran)}, nil
	}
	last.Message = fmt.Sprintf("OK, %d DDL statements", ran)
	return last, nil
}

func (s *Service) mutationGateEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hardening.MutationGate && s.mutations != nil
}

func (s *Service) auditQuery(ctx context.Context, p Principal, connID, sqlText string, mutating bool, outcome, detail string, durationMs int64, mutationID string) {
	if s.mutations == nil {
		return
	}
	_ = s.mutations.InsertQueryAudit(ctx, store.QueryAuditRow{
		ID:           uuid.NewString(),
		TS:           time.Now().UTC(),
		ActorID:      p.UserID,
		ActorName:    p.Username,
		ActorRole:    p.Role,
		ConnectionID: connID,
		SQLText:      sqlText,
		Mutating:     mutating,
		Outcome:      outcome,
		Detail:       detail,
		DurationMs:   durationMs,
		MutationID:   mutationID,
	})
}

// MutationView is the API projection of a SQL change ticket.
type MutationView struct {
	ID             string `json:"id"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	OwnerID        string `json:"ownerId"`
	OwnerName      string `json:"ownerName"`
	ConnectionID   string `json:"connectionId"`
	ConnectionName string `json:"connectionName"`
	ConnectionHost string `json:"connectionHost"`
	ConnectionPort int    `json:"connectionPort"`
	ConnType       string `json:"connType"`
	ActiveSchema   string `json:"activeSchema"`
	Title          string `json:"title"`
	Remark         string `json:"remark"`
	DemandURL      string `json:"demandUrl"`
	SQLText        string `json:"sqlText"`
	StmtClass      string `json:"stmtClass"`
	LintResult     string `json:"lintResult"`
	Status         string `json:"status"`
	Rollbackable   bool   `json:"rollbackable"`
	ReviewerName   string `json:"reviewerName"`
	ReviewNote     string `json:"reviewNote"`
	ApprovedAt     string `json:"approvedAt,omitempty"`
	ExecutedAt     string `json:"executedAt,omitempty"`
	RolledBackAt   string `json:"rolledBackAt,omitempty"`
}

// QueryAuditView is the API projection of an audit row.
type QueryAuditView struct {
	ID           string `json:"id"`
	TS           string `json:"ts"`
	ActorName    string `json:"actorName"`
	ActorRole    string `json:"actorRole"`
	ConnectionID string `json:"connectionId"`
	SQLText      string `json:"sqlText"`
	Mutating     bool   `json:"mutating"`
	Outcome      string `json:"outcome"`
	Detail       string `json:"detail"`
	DurationMs   int64  `json:"durationMs"`
	MutationID   string `json:"mutationId"`
}

func mutationView(m *store.MutationRow) MutationView {
	v := MutationView{
		ID: m.ID, OwnerID: m.OwnerID, OwnerName: m.OwnerName,
		ConnectionID: m.ConnectionID, ConnType: m.ConnType, ActiveSchema: m.ActiveSchema,
		Title: m.Title, Remark: m.Remark, DemandURL: m.DemandURL,
		SQLText: m.SQLText, StmtClass: m.StmtClass, LintResult: m.LintResult,
		Status: m.Status, Rollbackable: m.Rollbackable,
		ReviewerName: m.ReviewerName, ReviewNote: m.ReviewNote,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: m.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if m.ApprovedAt != nil {
		v.ApprovedAt = m.ApprovedAt.UTC().Format(time.RFC3339Nano)
	}
	if m.ExecutedAt != nil {
		v.ExecutedAt = m.ExecutedAt.UTC().Format(time.RFC3339Nano)
	}
	if m.RolledBackAt != nil {
		v.RolledBackAt = m.RolledBackAt.UTC().Format(time.RFC3339Nano)
	}
	return v
}

func defaultTicketTitle(class db.StmtClass, query string) string {
	q := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	runes := []rune(q)
	if len(runes) > 80 {
		q = string(runes[:80]) + "…"
	}
	if q == "" {
		return string(class) + " ticket"
	}
	return q
}

// submitMutationTicket inserts a pending ticket after lint. Blocks on lint errors.
func (s *Service) submitMutationTicket(ctx context.Context, p Principal, connectionID, query string, connType config.ConnType, title, remark, demandURL string) (*db.QueryResult, error) {
	class := db.ClassifySQL(connType, query)
	if class == db.StmtOther || connType == config.ConnRedis || connType == config.ConnMongo {
		err := fmt.Errorf("write commands on %s are disabled on the server; use a SQL connection for audited DML", connType)
		s.auditQuery(ctx, p, connectionID, query, true, "denied", err.Error(), 0, "")
		return nil, err
	}
	if !db.IsMutatingClass(class) {
		return nil, fmt.Errorf("not a mutating statement")
	}
	findings := db.LintSQL(connType, query)
	lintSummary := db.FormatLintSummary(findings)
	if db.LintBlocks(findings) {
		s.auditQuery(ctx, p, connectionID, query, true, "lint_block", lintSummary, 0, "")
		return nil, fmt.Errorf("lint blocked submission: %s", lintSummary)
	}
	rollbackable := class == db.StmtDML
	if rollbackable {
		if _, err := db.ParseSimpleDML(query); err != nil {
			rollbackable = false
		}
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultTicketTitle(class, query)
	}
	id := uuid.NewString()
	s.mu.Lock()
	activeSchema := s.activeSchemas[connectionID]
	s.mu.Unlock()
	row := store.MutationRow{
		ID: id, OwnerID: p.UserID, OwnerName: p.Username,
		ConnectionID: connectionID, ConnType: string(connType),
		ActiveSchema: activeSchema,
		Title: title, Remark: strings.TrimSpace(remark), DemandURL: strings.TrimSpace(demandURL),
		SQLText: query, StmtClass: string(class), LintResult: lintSummary,
		Status: "pending", Rollbackable: rollbackable,
	}
	if err := s.mutations.InsertMutation(ctx, row); err != nil {
		s.auditQuery(ctx, p, connectionID, query, true, "error", err.Error(), 0, "")
		return nil, fmt.Errorf("submit ticket: %w", err)
	}
	s.auditQuery(ctx, p, connectionID, query, true, "pending", "awaiting DBA approval; "+lintSummary, 0, id)
	return &db.QueryResult{
		Columns:  []string{"status", "mutation_id", "rollbackable", "lint"},
		Rows:     [][]string{{"pending_approval", id, fmt.Sprintf("%v", rollbackable), lintSummary}},
		RowCount: 1,
		Message:  fmt.Sprintf("Submitted change ticket for DBA approval (id=%s)", id),
	}, nil
}

// SubmitMutation creates an explicit SQL change ticket (Archery-style submit).
// title and content (stored as remark) are required; demandURL is optional.
func (s *Service) SubmitMutation(ctx context.Context, p Principal, connectionID, query, title, content, demandURL string) (*db.QueryResult, error) {
	if !s.mutationGateEnabled() {
		return nil, fmt.Errorf("mutation store not available")
	}
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" || content == "" {
		return nil, fmt.Errorf("title and content are required")
	}
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, connectionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	connType := c.Type
	s.mu.Unlock()
	// Submitting a ticket only queues for DBA review; Allow Writes gates live execution, not ticket intake.
	return s.submitMutationTicket(ctx, p, connectionID, query, connType, title, content, demandURL)
}

// SubmitOrExecuteQuery is the mutation-gate-aware query entry: reads run
// immediately (and are audited); writes become pending mutation requests.
func (s *Service) executeQueryGated(ctx context.Context, p Principal, connectionID, query string, cfg config.AppConfig, connType config.ConnType) (*db.QueryResult, error) {
	class := db.ClassifySQL(connType, query)
	mutating := db.IsMutatingClass(class)
	start := time.Now()

	if !mutating {
		drv, release, err := s.mgr.Acquire(connectionID)
		if err != nil {
			s.auditQuery(ctx, p, connectionID, query, false, "error", err.Error(), 0, "")
			return nil, err
		}
		defer release()
		res, err := drv.Execute(ctx, query, cfg.LLM.MaxRows, false)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			s.auditQuery(ctx, p, connectionID, query, false, "error", err.Error(), dur, "")
			return nil, err
		}
		s.auditQuery(ctx, p, connectionID, query, false, "ok", "", dur, "")
		return res, nil
	}

	// Run path is read-only under the mutation gate; writers must use SubmitMutation.
	err := fmt.Errorf("run only allows queries; submit a change ticket for write/DDL statements")
	s.auditQuery(ctx, p, connectionID, query, true, "denied", err.Error(), 0, "")
	return nil, err
}

// ListMutations returns mutation tickets visible to the caller.
// DBA sees all unless mineOnly; others always see only their own.
func (s *Service) ListMutations(ctx context.Context, p Principal, status string, limit int, mineOnly bool) ([]MutationView, error) {
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	ownerID := p.UserID
	if roleBypassesGrants(p.Role) && !mineOnly {
		ownerID = ""
	}
	rows, err := s.mutations.ListMutations(ctx, ownerID, status, limit)
	if err != nil {
		return nil, err
	}
	type connDisp struct {
		name string
		host string
		port int
	}
	cache := map[string]connDisp{}
	out := make([]MutationView, 0, len(rows))
	for i := range rows {
		v := mutationView(&rows[i])
		if d, ok := cache[v.ConnectionID]; ok {
			v.ConnectionName, v.ConnectionHost, v.ConnectionPort = d.name, d.host, d.port
		} else if d := s.connectionDisplay(ctx, v.ConnectionID); d.name != "" || d.host != "" {
			cache[v.ConnectionID] = d
			v.ConnectionName, v.ConnectionHost, v.ConnectionPort = d.name, d.host, d.port
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) connectionDisplay(ctx context.Context, connectionID string) (d struct {
	name string
	host string
	port int
}) {
	if connectionID == "" || s.grants == nil {
		return d
	}
	meta, err := s.grants.GetConnectionByID(ctx, connectionID)
	if err != nil || meta == nil {
		return d
	}
	d.name, d.host, d.port = meta.Name, meta.Host, meta.Port
	return d
}

// ListQueryAudit returns audit rows. DBA sees all; others see own.
func (s *Service) ListQueryAudit(ctx context.Context, p Principal, limit int) ([]QueryAuditView, error) {
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	actorID := p.UserID
	if roleBypassesGrants(p.Role) {
		actorID = ""
	}
	rows, err := s.mutations.ListQueryAudit(ctx, actorID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]QueryAuditView, 0, len(rows))
	for _, r := range rows {
		out = append(out, QueryAuditView{
			ID: r.ID, TS: r.TS.UTC().Format(time.RFC3339Nano),
			ActorName: r.ActorName, ActorRole: r.ActorRole, ConnectionID: r.ConnectionID,
			SQLText: r.SQLText, Mutating: r.Mutating, Outcome: r.Outcome,
			Detail: r.Detail, DurationMs: r.DurationMs, MutationID: r.MutationID,
		})
	}
	return out, nil
}

// ensureOpenForOwner opens the owner's connection only if it is not already live,
// so a concurrent user session is not kicked. Applies ActiveSchema when set.
func (s *Service) ensureOpenForOwner(ctx context.Context, owner Principal, connID, activeSchema string) error {
	if _, err := s.mgr.Get(connID); err != nil {
		if err := s.Connect(ctx, owner, connID); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	}
	if strings.TrimSpace(activeSchema) != "" {
		if err := s.mgr.SetActiveSchema(connID, activeSchema); err != nil {
			return fmt.Errorf("set schema %q: %w", activeSchema, err)
		}
	}
	return nil
}

// ApproveMutation marks a pending ticket approved (does not execute). Dual control: not self.
func (s *Service) ApproveMutation(ctx context.Context, p Principal, mutationID, note string) error {
	if !roleBypassesGrants(p.Role) {
		return fmt.Errorf("only admin or DBA can approve mutations")
	}
	if s.mutations == nil {
		return fmt.Errorf("mutation store not available")
	}
	m, err := s.mutations.GetMutation(ctx, mutationID)
	if err != nil {
		return err
	}
	if m.Status != "pending" {
		return fmt.Errorf("mutation is %s, not pending", m.Status)
	}
	if m.OwnerID == p.UserID {
		return fmt.Errorf("DBA cannot approve their own mutation request")
	}
	ok, err := s.mutations.ClaimMutationStatus(ctx, m.ID, "pending", "approved", p.UserID, p.Username)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("mutation already claimed by another reviewer")
	}
	now := time.Now().UTC()
	m.Status = "approved"
	m.ReviewerID = p.UserID
	m.ReviewerName = p.Username
	m.ReviewNote = strings.TrimSpace(note)
	m.ApprovedAt = &now
	if err := s.mutations.UpdateMutation(ctx, *m); err != nil {
		return err
	}
	s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "approved", note, 0, m.ID)
	return nil
}

// ExecuteMutation runs an approved ticket as DBA.
func (s *Service) ExecuteMutation(ctx context.Context, p Principal, mutationID, note string) (*db.QueryResult, error) {
	if !roleBypassesGrants(p.Role) {
		return nil, fmt.Errorf("only admin or DBA can execute mutations")
	}
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	m, err := s.mutations.GetMutation(ctx, mutationID)
	if err != nil {
		return nil, err
	}
	if m.Status != "approved" {
		return nil, fmt.Errorf("mutation is %s, not approved", m.Status)
	}
	if m.OwnerID == p.UserID {
		return nil, fmt.Errorf("DBA cannot execute their own mutation request")
	}

	ok, err := s.mutations.ClaimMutationStatus(ctx, m.ID, "approved", "executing", p.UserID, p.Username)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("mutation already claimed by another executor")
	}

	owner := Principal{UserID: m.OwnerID, Username: m.OwnerName, Role: "user"}
	s.mu.Lock()
	if _, err := s.findConnLocked(ctx, owner, m.ConnectionID); err != nil {
		s.mu.Unlock()
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "executing", "approved", p.UserID, p.Username)
		return nil, fmt.Errorf("owner connection: %w", err)
	}
	timeout := s.hardening.QueryTimeout
	s.mu.Unlock()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Revert to approved so DBA can retry execute.
	failApproved := func(execErr error) (*db.QueryResult, error) {
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "executing", "approved", p.UserID, p.Username)
		s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "error", execErr.Error(), 0, m.ID)
		return nil, execErr
	}

	if err := s.ensureOpenForOwner(ctx, owner, m.ConnectionID, m.ActiveSchema); err != nil {
		return failApproved(err)
	}
	drv, release, err := s.mgr.Acquire(m.ConnectionID)
	if err != nil {
		return failApproved(err)
	}
	defer release()

	start := time.Now()
	var res *db.QueryResult
	var snapJSON string
	rollbackable := false

	switch db.StmtClass(m.StmtClass) {
	case db.StmtDML:
		if m.Rollbackable {
			if _, perr := db.ParseSimpleDML(m.SQLText); perr == nil {
				snapper, ok := drv.(dmlSnapshotDriver)
				if !ok {
					return failApproved(fmt.Errorf("connection type does not support rollbackable DML"))
				}
				var snap *db.RowSnapshot
				res, snap, err = snapper.ExecuteDMLSnapshot(ctx, m.SQLText)
				if err != nil {
					return failApproved(fmt.Errorf("execute (snapshot): %w", err))
				}
				snapJSON, err = db.MarshalSnapshot(snap)
				if err != nil {
					return failApproved(err)
				}
				rollbackable = true
				break
			}
		}
		res, err = drv.Execute(ctx, m.SQLText, 1, true)
		if err != nil {
			return failApproved(err)
		}
		rollbackable = false
	case db.StmtDDL:
		res, err = execDDLStatements(ctx, drv, m.SQLText)
		if err != nil {
			return failApproved(err)
		}
		rollbackable = false
	default:
		return failApproved(fmt.Errorf("unsupported stmt class %s", m.StmtClass))
	}

	now := time.Now().UTC()
	m.Status = "executed"
	m.Rollbackable = rollbackable
	m.BeforeImage = snapJSON
	m.ReviewerID = p.UserID
	m.ReviewerName = p.Username
	if note = strings.TrimSpace(note); note != "" {
		m.ReviewNote = note
	}
	m.ExecutedAt = &now
	if err := s.mutations.UpdateMutation(ctx, *m); err != nil {
		s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "persist_error", err.Error(), time.Since(start).Milliseconds(), m.ID)
		return nil, fmt.Errorf("persist mutation after execute: %w", err)
	}
	s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "executed", note, time.Since(start).Milliseconds(), m.ID)
	return res, nil
}

// RejectMutation marks a pending mutation rejected by DBA.
func (s *Service) RejectMutation(ctx context.Context, p Principal, mutationID, note string) error {
	if !roleBypassesGrants(p.Role) {
		return fmt.Errorf("only admin or DBA can reject mutations")
	}
	if s.mutations == nil {
		return fmt.Errorf("mutation store not available")
	}
	m, err := s.mutations.GetMutation(ctx, mutationID)
	if err != nil {
		return err
	}
	if m.Status != "pending" {
		return fmt.Errorf("mutation is %s, not pending", m.Status)
	}
	// CAS so reject cannot clobber an in-flight approve.
	ok, err := s.mutations.ClaimMutationStatus(ctx, m.ID, "pending", "rejected", p.UserID, p.Username)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("mutation already claimed by another reviewer")
	}
	m.Status = "rejected"
	m.ReviewerID = p.UserID
	m.ReviewerName = p.Username
	m.ReviewNote = strings.TrimSpace(note)
	if err := s.mutations.UpdateMutation(ctx, *m); err != nil {
		return err
	}
	s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "rejected", note, 0, m.ID)
	return nil
}

// RollbackMutation applies compensating SQL for an executed rollbackable mutation.
func (s *Service) RollbackMutation(ctx context.Context, p Principal, mutationID, note string) (*db.QueryResult, error) {
	if !roleBypassesGrants(p.Role) {
		return nil, fmt.Errorf("only admin or DBA can roll back mutations")
	}
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	m, err := s.mutations.GetMutation(ctx, mutationID)
	if err != nil {
		return nil, err
	}
	if m.Status != "executed" {
		return nil, fmt.Errorf("mutation is %s, not executed", m.Status)
	}
	if !m.Rollbackable || m.BeforeImage == "" {
		return nil, fmt.Errorf("mutation is not rollbackable")
	}
	snap, err := db.UnmarshalSnapshot(m.BeforeImage)
	if err != nil {
		return nil, err
	}

	ok, err := s.mutations.ClaimMutationStatus(ctx, m.ID, "executed", "rolling_back", p.UserID, p.Username)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("mutation already being rolled back")
	}

	owner := Principal{UserID: m.OwnerID, Username: m.OwnerName, Role: "user"}
	s.mu.Lock()
	if _, err := s.findConnLocked(ctx, owner, m.ConnectionID); err != nil {
		s.mu.Unlock()
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "rolling_back", "executed", p.UserID, p.Username)
		return nil, err
	}
	timeout := s.hardening.QueryTimeout
	s.mu.Unlock()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	failRestore := func(execErr error) (*db.QueryResult, error) {
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "rolling_back", "executed", p.UserID, p.Username)
		s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "rollback_error", execErr.Error(), 0, m.ID)
		return nil, execErr
	}

	if err := s.ensureOpenForOwner(ctx, owner, m.ConnectionID, m.ActiveSchema); err != nil {
		return failRestore(err)
	}
	drv, release, err := s.mgr.Acquire(m.ConnectionID)
	if err != nil {
		return failRestore(err)
	}
	defer release()

	snapper, ok := drv.(dmlSnapshotDriver)
	if !ok {
		return failRestore(fmt.Errorf("driver does not support rollback"))
	}
	start := time.Now()
	res, err := snapper.ExecuteRollbackSnapshot(ctx, snap)
	if err != nil {
		return failRestore(err)
	}
	now := time.Now().UTC()
	m.Status = "rolled_back"
	m.RolledBackAt = &now
	if note != "" {
		m.ReviewNote = strings.TrimSpace(note)
	}
	m.ReviewerID = p.UserID
	m.ReviewerName = p.Username
	if err := s.mutations.UpdateMutation(ctx, *m); err != nil {
		s.auditQuery(ctx, p, m.ConnectionID, "-- rollback of "+m.ID, true, "persist_error", err.Error(), time.Since(start).Milliseconds(), m.ID)
		return nil, fmt.Errorf("persist rollback after execute: %w", err)
	}
	s.auditQuery(ctx, p, m.ConnectionID, "-- rollback of "+m.ID, true, "rolled_back", note, time.Since(start).Milliseconds(), m.ID)
	return res, nil
}
