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

// MutationView is the API projection of a mutation request (no huge surprises).
type MutationView struct {
	ID           string  `json:"id"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
	OwnerID      string  `json:"ownerId"`
	OwnerName    string  `json:"ownerName"`
	ConnectionID string  `json:"connectionId"`
	ConnType     string  `json:"connType"`
	SQLText      string  `json:"sqlText"`
	StmtClass    string  `json:"stmtClass"`
	Status       string  `json:"status"`
	Rollbackable bool    `json:"rollbackable"`
	ReviewerName string  `json:"reviewerName"`
	ReviewNote   string  `json:"reviewNote"`
	ExecutedAt   string  `json:"executedAt,omitempty"`
	RolledBackAt string  `json:"rolledBackAt,omitempty"`
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
		ConnectionID: m.ConnectionID, ConnType: m.ConnType, SQLText: m.SQLText,
		StmtClass: m.StmtClass, Status: m.Status, Rollbackable: m.Rollbackable,
		ReviewerName: m.ReviewerName, ReviewNote: m.ReviewNote,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: m.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if m.ExecutedAt != nil {
		v.ExecutedAt = m.ExecutedAt.UTC().Format(time.RFC3339Nano)
	}
	if m.RolledBackAt != nil {
		v.RolledBackAt = m.RolledBackAt.UTC().Format(time.RFC3339Nano)
	}
	return v
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

	// Redis/Mongo writes: forbidden in v1 (no rollback story).
	if class == db.StmtOther || connType == config.ConnRedis || connType == config.ConnMongo {
		err := fmt.Errorf("write commands on %s are disabled on the server; use a SQL connection for audited DML", connType)
		s.auditQuery(ctx, p, connectionID, query, true, "denied", err.Error(), 0, "")
		return nil, err
	}

	if !cfg.LLM.AllowWrites {
		err := fmt.Errorf("writes are disabled in settings; enable Allow Writes to submit changes for DBA approval")
		s.auditQuery(ctx, p, connectionID, query, true, "denied", err.Error(), 0, "")
		return nil, err
	}

	id := uuid.NewString()
	rollbackable := class == db.StmtDML
	if rollbackable {
		if _, err := db.ParseSimpleDML(query); err != nil {
			rollbackable = false
		}
	}
	s.mu.Lock()
	activeSchema := s.activeSchemas[connectionID]
	s.mu.Unlock()
	row := store.MutationRow{
		ID: id, OwnerID: p.UserID, OwnerName: p.Username,
		ConnectionID: connectionID, ConnType: string(connType),
		ActiveSchema: activeSchema,
		SQLText: query, StmtClass: string(class), Status: "pending",
		Rollbackable: rollbackable,
	}
	if err := s.mutations.InsertMutation(ctx, row); err != nil {
		s.auditQuery(ctx, p, connectionID, query, true, "error", err.Error(), 0, "")
		return nil, fmt.Errorf("submit mutation: %w", err)
	}
	s.auditQuery(ctx, p, connectionID, query, true, "pending", "awaiting DBA approval", 0, id)
	return &db.QueryResult{
		Columns:  []string{"status", "mutation_id", "rollbackable"},
		Rows:     [][]string{{"pending_approval", id, fmt.Sprintf("%v", rollbackable)}},
		RowCount: 1,
		Message:  fmt.Sprintf("Submitted for DBA approval (id=%s)", id),
	}, nil
}

// ListMutations returns mutation requests visible to the caller.
// DBA sees all (optionally filtered by status); others see only their own.
func (s *Service) ListMutations(ctx context.Context, p Principal, status string, limit int) ([]MutationView, error) {
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	ownerID := p.UserID
	if p.Role == "dba" {
		ownerID = ""
	}
	rows, err := s.mutations.ListMutations(ctx, ownerID, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]MutationView, 0, len(rows))
	for i := range rows {
		out = append(out, mutationView(&rows[i]))
	}
	return out, nil
}

// ListQueryAudit returns audit rows. DBA sees all; others see own.
func (s *Service) ListQueryAudit(ctx context.Context, p Principal, limit int) ([]QueryAuditView, error) {
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	actorID := p.UserID
	if p.Role == "dba" {
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

// ApproveMutation executes a pending mutation as DBA (dual control: not self).
func (s *Service) ApproveMutation(ctx context.Context, p Principal, mutationID, note string) (*db.QueryResult, error) {
	if p.Role != "dba" {
		return nil, fmt.Errorf("only DBA can approve mutations")
	}
	if s.mutations == nil {
		return nil, fmt.Errorf("mutation store not available")
	}
	m, err := s.mutations.GetMutation(ctx, mutationID)
	if err != nil {
		return nil, err
	}
	if m.Status != "pending" {
		return nil, fmt.Errorf("mutation is %s, not pending", m.Status)
	}
	if m.OwnerID == p.UserID {
		return nil, fmt.Errorf("DBA cannot approve their own mutation request")
	}

	// CAS claim before touching the target DB — prevents double-exec.
	ok, err := s.mutations.ClaimMutationStatus(ctx, m.ID, "pending", "executing", p.UserID, p.Username)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("mutation already claimed by another reviewer")
	}

	owner := Principal{UserID: m.OwnerID, Username: m.OwnerName, Role: "user"}
	s.mu.Lock()
	if _, err := s.findConnLocked(ctx, owner, m.ConnectionID); err != nil {
		s.mu.Unlock()
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "executing", "pending", "", "")
		return nil, fmt.Errorf("owner connection: %w", err)
	}
	timeout := s.hardening.QueryTimeout
	s.mu.Unlock()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Revert to pending so DBA can retry; "failed" would permanently dead-end the request.
	failPending := func(execErr error) (*db.QueryResult, error) {
		_, _ = s.mutations.ClaimMutationStatus(ctx, m.ID, "executing", "pending", "", "")
		s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "error", execErr.Error(), 0, m.ID)
		return nil, execErr
	}

	if err := s.ensureOpenForOwner(ctx, owner, m.ConnectionID, m.ActiveSchema); err != nil {
		return failPending(err)
	}
	drv, release, err := s.mgr.Acquire(m.ConnectionID)
	if err != nil {
		return failPending(err)
	}
	defer release()

	start := time.Now()
	var res *db.QueryResult
	var snapJSON string
	rollbackable := false

	switch db.StmtClass(m.StmtClass) {
	case db.StmtDML:
		// Prefer snapshot path when the statement is simple + rollbackable; otherwise
		// plain exec (non-rollbackable) so JOIN/INSERT…SELECT are not stuck forever.
		if m.Rollbackable {
			if _, perr := db.ParseSimpleDML(m.SQLText); perr == nil {
				snapper, ok := drv.(dmlSnapshotDriver)
				if !ok {
					return failPending(fmt.Errorf("connection type does not support rollbackable DML"))
				}
				var snap *db.RowSnapshot
				res, snap, err = snapper.ExecuteDMLSnapshot(ctx, m.SQLText)
				if err != nil {
					return failPending(fmt.Errorf("execute (snapshot): %w", err))
				}
				snapJSON, err = db.MarshalSnapshot(snap)
				if err != nil {
					return failPending(err)
				}
				rollbackable = true
				break
			}
		}
		res, err = drv.Execute(ctx, m.SQLText, 1, true)
		if err != nil {
			return failPending(err)
		}
		rollbackable = false
	case db.StmtDDL:
		res, err = execDDLStatements(ctx, drv, m.SQLText)
		if err != nil {
			return failPending(err)
		}
		rollbackable = false
	default:
		return failPending(fmt.Errorf("unsupported stmt class %s", m.StmtClass))
	}

	now := time.Now().UTC()
	m.Status = "executed"
	m.Rollbackable = rollbackable
	m.BeforeImage = snapJSON
	m.ReviewerID = p.UserID
	m.ReviewerName = p.Username
	m.ReviewNote = strings.TrimSpace(note)
	m.ExecutedAt = &now
	if err := s.mutations.UpdateMutation(ctx, *m); err != nil {
		// Target DB already changed — leave status executing/failed for operator follow-up.
		s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "persist_error", err.Error(), time.Since(start).Milliseconds(), m.ID)
		return nil, fmt.Errorf("persist mutation after execute: %w", err)
	}
	s.auditQuery(ctx, p, m.ConnectionID, m.SQLText, true, "executed", note, time.Since(start).Milliseconds(), m.ID)
	return res, nil
}

// RejectMutation marks a pending mutation rejected by DBA.
func (s *Service) RejectMutation(ctx context.Context, p Principal, mutationID, note string) error {
	if p.Role != "dba" {
		return fmt.Errorf("only DBA can reject mutations")
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
	if p.Role != "dba" {
		return nil, fmt.Errorf("only DBA can roll back mutations")
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
