// Package app is the transport-agnostic application layer shared by the desktop
// (Wails bridge) and server (HTTP) adapters. Every method takes the caller
// Principal so state can be partitioned per user.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/agent"
	"AI-DB/internal/config"
	"AI-DB/internal/db"
	"AI-DB/internal/egress"
	"AI-DB/internal/store"

	"github.com/google/uuid"
)

// Service is the shared application layer. It carries no per-request state in
// its fields: the caller Principal on each method selects the tenant, and each
// tenant's config document is cached in mem keyed by UserID. A single Service
// instance therefore safely serves every user of the server build, while the
// desktop build only ever uses the "local" key.
type Service struct {
	store store.ConfigStore
	mgr   *db.Manager
	agent *agent.Service

	mu        sync.Mutex
	mem       map[string]*config.AppConfig
	hardening Hardening
}

// Hardening carries the server-only safety knobs. The zero value is the desktop
// posture and changes nothing: no egress policy (connect anywhere, incl.
// localhost), no query timeout, and an empty host still defaults to 127.0.0.1.
// The server injects a non-zero Hardening at startup; per-user/global connection
// caps live on db.Manager (see Manager.SetLimits), not here.
type Hardening struct {
	// Egress vetoes user-supplied hosts before any dial (SSRF guard). Nil = allow all.
	Egress *egress.Policy
	// QueryTimeout bounds a single interactive query and each agent tool call. 0 = no timeout.
	QueryTimeout time.Duration
	// RequireExplicitHost rejects an empty host instead of defaulting it to
	// 127.0.0.1, which on a server would ambiguously mean "the server itself".
	RequireExplicitHost bool
}

// SetHardening installs the server safety knobs and forwards the query timeout to
// the agent runner. It must be called at startup, before serving requests. The
// desktop build never calls it and keeps the zero-value (unrestricted) posture.
func (s *Service) SetHardening(h Hardening) {
	s.mu.Lock()
	s.hardening = h
	ag := s.agent
	s.mu.Unlock()
	if ag != nil {
		ag.SetQueryTimeout(h.QueryTimeout)
	}
}

// NewService wires the service to a config store and connection manager. A nil
// store is tolerated: reads fall back to in-memory defaults and writes report
// "config store not ready", mirroring the desktop build's behaviour when its
// config directory is unwritable.
func NewService(st store.ConfigStore, mgr *db.Manager) *Service {
	return &Service{
		store: st,
		mgr:   mgr,
		agent: agent.NewService(mgr),
		mem:   make(map[string]*config.AppConfig),
	}
}

// internalError marks a config-store (persistence) failure — an infrastructure
// fault such as the backing DB being unreachable, the pool exhausted, or an I/O
// error, as opposed to a business/validation error or a user-facing result like a
// TestConnection probe outcome. Only store.Load/store.Save failures are wrapped.
//
// The server adapter uses IsInternalError to log the detail and return a generic
// message, so a transient backend fault never leaks internal addresses (e.g. the
// operator's MySQL host) to an authenticated tenant. The desktop adapter surfaces
// errors unchanged — its store is a local file the single user owns — and the
// wrapped Error() text is identical, so desktop behaviour is byte-for-byte the
// same. This is purely an advisory classification for the HTTP layer.
type internalError struct{ err error }

func (e *internalError) Error() string { return e.err.Error() }
func (e *internalError) Unwrap() error { return e.err }

// IsInternalError reports whether err (or anything it wraps) originated in the
// config-store persistence layer rather than in business logic.
func IsInternalError(err error) bool {
	var ie *internalError
	return errors.As(err, &ie)
}

// ensureCfgLocked returns the caller's cached config, lazily loading it from the
// store on first touch.
//
// A store Load failure is NOT cached and is returned to the caller. Freezing a
// transient failure (request-context cancellation on browser refresh, a DB blip,
// or a corrupt/undecryptable document) into an empty in-memory config would let a
// later Save persist that empty document over the user's real data — for the
// MySQLStore, a full DELETE+INSERT that silently wipes every connection. By
// surfacing the error instead, write paths abort before reaching Save and the
// next request retries the load.
//
// A nil store (desktop config dir unwritable) still yields cached in-memory
// defaults so reads work; writes then fail in persistLocked.
func (s *Service) ensureCfgLocked(ctx context.Context, p Principal) (*config.AppConfig, error) {
	if cfg, ok := s.mem[p.UserID]; ok {
		config.NormalizeLLMProfiles(cfg)
		return cfg, nil
	}
	if s.store == nil {
		cfg := &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
		s.mem[p.UserID] = cfg
		config.NormalizeLLMProfiles(cfg)
		return cfg, nil
	}
	loaded, err := s.store.Load(ctx, p.UserID)
	if err != nil {
		return nil, &internalError{err}
	}
	if loaded == nil {
		loaded = &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
	}
	s.mem[p.UserID] = loaded
	config.NormalizeLLMProfiles(loaded)
	return loaded, nil
}

// persistLocked writes the caller's cached config back to the store.
func (s *Service) persistLocked(ctx context.Context, p Principal) error {
	if s.store == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return err
	}
	if err := s.store.Save(ctx, p.UserID, cfg); err != nil {
		return &internalError{err}
	}
	return nil
}

// findConnLocked returns the caller's connection by id. Because it only scans the
// caller's own cached document it doubles as the ownership check: a user can never
// resolve another tenant's connection id.
func (s *Service) findConnLocked(ctx context.Context, p Principal, id string) (config.Connection, error) {
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return config.Connection{}, err
	}
	for _, c := range cfg.Connections {
		if c.ID == id {
			return c, nil
		}
	}
	return config.Connection{}, fmt.Errorf("connection not found: %s", id)
}

// cloneCfg returns a deep copy of cfg. Connection and LLMProfile are value
// structs, so copying the two slices yields a fully independent document. Write
// paths snapshot with this before mutating the cached doc, then restore the
// snapshot if persistLocked fails — otherwise the in-memory copy diverges from
// the store (e.g. a deleted-but-unpersisted connection vanishes from the UI yet
// "revives" after a restart, and a retry can never remove it because the cache
// no longer lists it).
func cloneCfg(cfg *config.AppConfig) *config.AppConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if cfg.Connections != nil {
		out.Connections = make([]config.Connection, len(cfg.Connections))
		copy(out.Connections, cfg.Connections)
	}
	if cfg.LLMProfiles != nil {
		out.LLMProfiles = make([]config.LLMProfile, len(cfg.LLMProfiles))
		copy(out.LLMProfiles, cfg.LLMProfiles)
	}
	return &out
}

// PrimeDefaults pre-seeds a user's in-memory config with empty defaults WITHOUT
// touching the store, so a later Save overwrites whatever is on disk. It is a
// no-op if the config is already cached.
//
// Only the desktop adapter may call this, and only after its own FileStore load
// has already failed (a corrupt or undecryptable config.json). It reproduces the
// legacy desktop self-heal: reads return defaults and the next write replaces the
// bad file, instead of every write aborting on the surfaced load error forever.
//
// The server build must NOT call it. A MySQLStore load failure is usually
// transient (a DB blip or a cancelled request context); freezing it into an empty
// document here would let the next Save persist that empty doc over the user's
// real data. Server load failures must surface and be retried, per ensureCfgLocked.
func (s *Service) PrimeDefaults(p Principal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.mem[p.UserID]; ok {
		return
	}
	cfg := &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
	config.NormalizeLLMProfiles(cfg)
	s.mem[p.UserID] = cfg
}

// —— connections ——

// ListConnections returns the caller's saved connections without passwords.
func (s *Service) ListConnections(ctx context.Context, p Principal) ([]config.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]config.Connection, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		out = append(out, config.PublicConnection(c))
	}
	return out, nil
}

// UpsertConnection creates or updates a connection profile. An empty or masked
// password on update keeps the existing secret.
func (s *Service) UpsertConnection(ctx context.Context, p Principal, c config.Connection) (config.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return c, err
	}
	snap := cloneCfg(cfg)
	if c.Type == "" {
		return c, fmt.Errorf("type is required")
	}
	if c.Name == "" {
		return c, fmt.Errorf("name is required")
	}
	if c.Host == "" {
		if s.hardening.RequireExplicitHost {
			return c, fmt.Errorf("host is required")
		}
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = config.DefaultPort(c.Type)
	}
	keepPassword := c.Password == "" || strings.HasPrefix(c.Password, "••••")
	if c.ID == "" {
		c.ID = uuid.NewString()
		if keepPassword {
			c.Password = ""
		}
		cfg.Connections = append(cfg.Connections, c)
	} else {
		found := false
		for i := range cfg.Connections {
			if cfg.Connections[i].ID == c.ID {
				if keepPassword {
					c.Password = cfg.Connections[i].Password
				}
				cfg.Connections[i] = c
				found = true
				break
			}
		}
		if !found {
			if keepPassword {
				c.Password = ""
			}
			cfg.Connections = append(cfg.Connections, c)
		}
	}
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return config.PublicConnection(c), err
	}
	return config.PublicConnection(c), nil
}

// DeleteConnection removes one of the caller's connections and disconnects any
// live driver. Ownership is verified BEFORE touching the shared manager: an id
// that is not in the caller's own config is a no-op that never calls Disconnect,
// so one tenant can never force-drop another tenant's live driver (the manager
// is global and keyed by connection id). The disconnect runs after the delete is
// persisted and outside the lock, matching the slow-I/O-outside-lock pattern used
// by Connect/ExecuteQuery.
func (s *Service) DeleteConnection(ctx context.Context, p Principal, id string) error {
	s.mu.Lock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	next := make([]config.Connection, 0, len(cfg.Connections))
	found := false
	for _, c := range cfg.Connections {
		if c.ID == id {
			found = true
			continue
		}
		next = append(next, c)
	}
	if !found {
		// Not owned by the caller (or already gone): no-op, never touch mgr.
		s.mu.Unlock()
		return nil
	}
	snap := cloneCfg(cfg)
	cfg.Connections = next
	err = s.persistLocked(ctx, p)
	if err != nil {
		s.mem[p.UserID] = snap // roll back under the lock; cache must not diverge from store
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	_ = s.mgr.Disconnect(id)
	return nil
}

// TestConnection probes connectivity for a profile. An empty/masked password with
// an existing id reuses the stored secret. The lock is released before the
// network dial.
func (s *Service) TestConnection(ctx context.Context, p Principal, c config.Connection) error {
	s.mu.Lock()
	if c.ID != "" && (c.Password == "" || strings.HasPrefix(c.Password, "••••")) {
		if existing, err := s.findConnLocked(ctx, p, c.ID); err == nil {
			c.Password = existing.Password
			if c.User == "" {
				c.User = existing.User
			}
		}
	}
	pol := s.hardening.Egress
	s.mu.Unlock()
	// Veto the host before dialling: TestConnection reports success/failure and is
	// therefore a prime SSRF probe oracle. A nil policy (desktop) allows all.
	if err := pol.AllowHost(ctx, c.Host); err != nil {
		return err
	}
	return db.TestConnection(ctx, c)
}

// Connect opens a live driver for one of the caller's saved connections.
func (s *Service) Connect(ctx context.Context, p Principal, id string) error {
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, id)
	pol := s.hardening.Egress
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := pol.AllowHost(ctx, c.Host); err != nil {
		return err
	}
	return s.mgr.Connect(ctx, p.UserID, c)
}

// Disconnect closes a live driver. Ownership is verified first so one tenant can
// never force-drop another tenant's live driver (the manager is global and keyed
// by connection id); a non-owned id returns "connection not found" and never
// touches the manager.
func (s *Service) Disconnect(ctx context.Context, p Principal, id string) error {
	s.mu.Lock()
	_, err := s.findConnLocked(ctx, p, id)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.mgr.Disconnect(id)
}

// —— object tree / query ——

// ListObjects returns top-level browser nodes (schemas / redis db). Ownership of
// connectionID is verified before touching the global manager.
func (s *Service) ListObjects(ctx context.Context, p Principal, connectionID string) ([]db.ObjectNode, error) {
	s.mu.Lock()
	_, err := s.findConnLocked(ctx, p, connectionID)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	drv, release, err := s.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.ListObjects(ctx)
}

// ExpandObject loads children for an object-tree path. Ownership of connectionID
// is verified before touching the global manager.
func (s *Service) ExpandObject(ctx context.Context, p Principal, connectionID string, path []string) ([]db.ObjectNode, error) {
	s.mu.Lock()
	_, err := s.findConnLocked(ctx, p, connectionID)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	drv, release, err := s.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.ExpandObject(ctx, path)
}

// ExecuteQuery runs SQL or a Redis command under the caller's safety knobs.
// Ownership of connectionID is verified first so a tenant can never run queries
// against another tenant's live driver.
func (s *Service) ExecuteQuery(ctx context.Context, p Principal, connectionID, query string) (*db.QueryResult, error) {
	s.mu.Lock()
	if _, err := s.findConnLocked(ctx, p, connectionID); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	loaded, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cfg := *loaded
	timeout := s.hardening.QueryTimeout
	s.mu.Unlock()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	drv, release, err := s.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.Execute(ctx, query, cfg.LLM.MaxRows, cfg.LLM.AllowWrites)
}

// CreateDatabaseOptions returns the dialog dropdown candidates: MySQL charsets
// with their collations (one read-only introspection query), Postgres encodings
// (compile-time fixed list). Other connection types have no such semantics.
func (s *Service) CreateDatabaseOptions(ctx context.Context, p Principal, connectionID string) (db.CreateDatabaseOptions, error) {
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, connectionID)
	s.mu.Unlock()
	if err != nil {
		return db.CreateDatabaseOptions{}, err
	}
	switch c.Type {
	case config.ConnMySQL:
		drv, release, err := s.mgr.Acquire(connectionID)
		if err != nil {
			return db.CreateDatabaseOptions{}, err
		}
		defer release()
		res, err := drv.Execute(ctx, db.MySQLCollationsSQL, 4000, false)
		if err != nil {
			return db.CreateDatabaseOptions{}, err
		}
		return db.GroupMySQLCollations(res), nil
	case config.ConnPostgres:
		return db.CreateDatabaseOptions{Encodings: db.PGEncodings()}, nil
	default:
		return db.CreateDatabaseOptions{}, fmt.Errorf("create database is not supported for %s connections", c.Type)
	}
}

// CreateDatabase runs the ordered creation steps (database → account → grant).
// DDL is not transactional, so a mid-flow failure returns OK=false naming the
// failed step while earlier steps stay applied. Requires Allow Writes.
func (s *Service) CreateDatabase(ctx context.Context, p Principal, connectionID string, spec db.CreateDatabaseSpec) (*db.CreateDatabaseResult, error) {
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, connectionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	loaded, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	allowWrites := loaded.LLM.AllowWrites
	timeout := s.hardening.QueryTimeout
	s.mu.Unlock()
	if !allowWrites {
		return nil, fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): CREATE DATABASE")
	}
	steps, err := db.BuildCreateDatabaseStatements(c.Type, spec)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	drv, release, err := s.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	res := &db.CreateDatabaseResult{OK: true}
	for _, step := range steps {
		if _, err := drv.Execute(ctx, step.SQL, 1, true); err != nil {
			res.OK = false
			res.FailedStep = step.Label
			res.Error = err.Error()
			return res, nil
		}
		res.Completed = append(res.Completed, step.Label)
	}
	return res, nil
}

// SetActiveSchema remembers the workbench-selected database/schema. Ownership of
// connectionID is verified before touching the global manager.
func (s *Service) SetActiveSchema(ctx context.Context, p Principal, connectionID, schema string) error {
	s.mu.Lock()
	_, err := s.findConnLocked(ctx, p, connectionID)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.mgr.SetActiveSchema(connectionID, schema)
}

// IsMutatingQuery reports whether the query looks like a write/DDL/admin command.
// An unknown connection yields false (matching the desktop behaviour).
func (s *Service) IsMutatingQuery(ctx context.Context, p Principal, connectionID, query string) (bool, error) {
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, connectionID)
	s.mu.Unlock()
	if err != nil {
		return false, nil
	}
	if c.Type == config.ConnRedis {
		return db.EnsureRedisReadOnly(query, false) != nil, nil
	}
	if c.Type == config.ConnMongo {
		return db.MongoQueryMutating(query), nil
	}
	return db.EnsureReadOnly(query, false) != nil, nil
}

// WritesAllowed reports whether mutating queries are enabled for the caller.
func (s *Service) WritesAllowed(ctx context.Context, p Principal) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return false, err
	}
	return cfg.LLM.AllowWrites, nil
}

// —— LLM ——

// GetLLMConfig returns the caller's active LLM settings with a masked API key.
func (s *Service) GetLLMConfig(ctx context.Context, p Principal) (config.LLMConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return config.LLMConfig{}, err
	}
	return config.MaskedLLM(cfg.LLM), nil
}

// ListLLMProfiles returns UI-safe LLM provider cards.
func (s *Service) ListLLMProfiles(ctx context.Context, p Principal) ([]config.LLMProfileView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return nil, err
	}
	return config.ProfileViews(cfg), nil
}

// allowLLMBase applies the SSRF egress veto to an LLM endpoint URL. The BaseURL
// is a tenant-controlled egress target exactly like a DB host, and the LLM
// probe/agent paths echo the response body (or its error text) back to the
// caller — so an unrestricted BaseURL is an even better cloud-metadata / loopback
// oracle than TestConnection. It is checked at every point a BaseURL is persisted
// or dialled. A nil *egress.Policy (the desktop posture) allows everything, so
// the local single-user build keeps its "point at any endpoint" behaviour.
//
// The egress pointer is read under the lock but AllowHost (which may do a DNS
// lookup) runs after unlocking, so callers may invoke this either before taking
// s.mu or after releasing it — never while holding it.
func (s *Service) allowLLMBase(ctx context.Context, base string) error {
	s.mu.Lock()
	pol := s.hardening.Egress
	s.mu.Unlock()
	if pol == nil {
		return nil
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil // no endpoint to dial; callers validate empties themselves
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("egress: invalid LLM base URL %q: %w", base, err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("egress: LLM base URL %q has no host", base)
	}
	return pol.AllowHost(ctx, host)
}

// SaveLLMProfile creates or updates a provider profile. The decomposed arguments
// mirror main.SaveLLMProfileRequest; keeping that DTO in package main preserves
// the existing Wails binding namespace (Phase 3 migrates it).
func (s *Service) SaveLLMProfile(ctx context.Context, p Principal, id, name, baseURL, model, apiKey string, clearKey bool) (config.LLMProfileView, error) {
	// SSRF: veto the endpoint before it is persisted (and later dialled by
	// AgentChat/TestLLMProfile). Done before taking the lock because AllowHost may
	// perform a DNS lookup. A nil policy (desktop) allows everything.
	if err := s.allowLLMBase(ctx, baseURL); err != nil {
		return config.LLMProfileView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return config.LLMProfileView{}, err
	}

	name = strings.TrimSpace(name)
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	if name == "" {
		return config.LLMProfileView{}, fmt.Errorf("name is required")
	}
	if base == "" {
		return config.LLMProfileView{}, fmt.Errorf("base URL is required")
	}
	if model == "" {
		return config.LLMProfileView{}, fmt.Errorf("model is required")
	}

	snap := cloneCfg(cfg)
	id = strings.TrimSpace(id)
	if id == "" {
		id = uuid.NewString()
		key := strings.TrimSpace(apiKey)
		if strings.HasPrefix(key, "••••") {
			key = ""
		}
		cfg.LLMProfiles = append(cfg.LLMProfiles, config.LLMProfile{
			ID: id, Name: name, BaseURL: base, Model: model, APIKey: key,
		})
		if cfg.ActiveLLMID == "" {
			cfg.ActiveLLMID = id
		}
	} else {
		idx := -1
		for i, prof := range cfg.LLMProfiles {
			if prof.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return config.LLMProfileView{}, fmt.Errorf("profile not found")
		}
		prof := cfg.LLMProfiles[idx]
		prof.Name, prof.BaseURL, prof.Model = name, base, model
		if clearKey {
			prof.APIKey = ""
		} else if k := strings.TrimSpace(apiKey); k != "" && !strings.HasPrefix(k, "••••") {
			prof.APIKey = k
		}
		cfg.LLMProfiles[idx] = prof
	}

	config.SyncActiveLLM(cfg)
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return config.LLMProfileView{}, err
	}
	for _, v := range config.ProfileViews(cfg) {
		if v.ID == id {
			return v, nil
		}
	}
	return config.LLMProfileView{ID: id, Name: name, BaseURL: base, Model: model}, nil
}

// DeleteLLMProfile removes a provider (at least one must remain).
func (s *Service) DeleteLLMProfile(ctx context.Context, p Principal, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return err
	}
	if len(cfg.LLMProfiles) <= 1 {
		return fmt.Errorf("keep at least one LLM profile")
	}
	next := make([]config.LLMProfile, 0, len(cfg.LLMProfiles)-1)
	found := false
	for _, prof := range cfg.LLMProfiles {
		if prof.ID == id {
			found = true
			continue
		}
		next = append(next, prof)
	}
	if !found {
		return fmt.Errorf("profile not found")
	}
	snap := cloneCfg(cfg)
	cfg.LLMProfiles = next
	if cfg.ActiveLLMID == id {
		cfg.ActiveLLMID = cfg.LLMProfiles[0].ID
	}
	config.SyncActiveLLM(cfg)
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return err
	}
	return nil
}

// ActivateLLMProfile switches the active provider for the next agent turn.
func (s *Service) ActivateLLMProfile(ctx context.Context, p Principal, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return err
	}
	if !hasProfileID(cfg, id) {
		return fmt.Errorf("profile not found")
	}
	snap := cloneCfg(cfg)
	cfg.ActiveLLMID = id
	config.SyncActiveLLM(cfg)
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return err
	}
	return nil
}

func hasProfileID(cfg *config.AppConfig, id string) bool {
	for _, prof := range cfg.LLMProfiles {
		if prof.ID == id {
			return true
		}
	}
	return false
}

// SaveLLMSafety updates the caller's safety knobs (writes / row / step limits).
func (s *Service) SaveLLMSafety(ctx context.Context, p Principal, allowWrites bool, maxRows, maxAgentSteps int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return err
	}
	if maxRows <= 0 {
		return fmt.Errorf("maxRows must be >= 1")
	}
	if maxAgentSteps <= 0 {
		return fmt.Errorf("maxAgentSteps must be >= 1")
	}
	snap := cloneCfg(cfg)
	cfg.LLM.AllowWrites = allowWrites
	cfg.LLM.MaxRows = maxRows
	cfg.LLM.MaxAgentSteps = maxAgentSteps
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return err
	}
	return nil
}

// TestLLMProfile probes chat completions. Empty form fields fall back to the saved
// profile. It returns the decomposed main.LLMTestResult fields (ok, message,
// model, durationMs) and never produces a Go error, matching the desktop contract;
// a config load failure is reported through the message field. The lock is
// released before the network probe.
func (s *Service) TestLLMProfile(ctx context.Context, p Principal, profileID, baseURL, model, apiKey string) (bool, string, string, int64) {
	s.mu.Lock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		s.mu.Unlock()
		return false, err.Error(), "", 0
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	mod := strings.TrimSpace(model)
	key := strings.TrimSpace(apiKey)
	if strings.HasPrefix(key, "••••") {
		key = ""
	}
	if profileID != "" {
		found := false
		for _, prof := range cfg.LLMProfiles {
			if prof.ID == profileID {
				found = true
				if base == "" {
					base = prof.BaseURL
				}
				if mod == "" {
					mod = prof.Model
				}
				if key == "" {
					key = prof.APIKey
				}
				break
			}
		}
		if !found {
			s.mu.Unlock()
			return false, "profile not found", "", 0
		}
	}
	s.mu.Unlock()

	if base == "" || mod == "" {
		return false, "请先填写 Base URL 与模型名", "", 0
	}
	// SSRF: the probe dialls base and echoes the response/error back, so veto the
	// host first. A nil policy (desktop) allows everything.
	if err := s.allowLLMBase(ctx, base); err != nil {
		return false, err.Error(), mod, 0
	}

	start := time.Now()
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := agent.NewClient(config.LLMConfig{BaseURL: base, APIKey: key, Model: mod})
	_, err = client.Chat(tctx, []agent.Message{{Role: "user", Content: "ping"}}, nil)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return false, err.Error(), mod, durationMs
	}
	return true, "连接成功", mod, durationMs
}

// SaveLLMConfig updates safety knobs and mirrors the endpoint onto the active
// profile (legacy compatibility entry point).
func (s *Service) SaveLLMConfig(ctx context.Context, p Principal, llm config.LLMConfig) error {
	// SSRF: this legacy entry mirrors llm.BaseURL onto the active profile, which
	// AgentChat later dialls. Veto it before persisting. Nil policy = desktop.
	if err := s.allowLLMBase(ctx, llm.BaseURL); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		return err
	}
	prev := cfg.LLM
	if llm.APIKey == "" || strings.HasPrefix(llm.APIKey, "••••") {
		llm.APIKey = prev.APIKey
	}
	if llm.MaxRows <= 0 {
		llm.MaxRows = config.DefaultLLM().MaxRows
	}
	if llm.MaxAgentSteps <= 0 {
		llm.MaxAgentSteps = config.DefaultLLM().MaxAgentSteps
	}
	snap := cloneCfg(cfg)
	cfg.LLM.AllowWrites = llm.AllowWrites
	cfg.LLM.MaxRows = llm.MaxRows
	cfg.LLM.MaxAgentSteps = llm.MaxAgentSteps

	// Mirror endpoint onto active profile.
	for i, prof := range cfg.LLMProfiles {
		if prof.ID == cfg.ActiveLLMID {
			prof.BaseURL = strings.TrimRight(strings.TrimSpace(llm.BaseURL), "/")
			prof.Model = strings.TrimSpace(llm.Model)
			prof.APIKey = llm.APIKey
			cfg.LLMProfiles[i] = prof
			break
		}
	}
	config.SyncActiveLLM(cfg)
	if err := s.persistLocked(ctx, p); err != nil {
		s.mem[p.UserID] = snap
		return err
	}
	return nil
}

// —— agent ——

// AgentChat runs one user message through the multi-step agent. The decomposed
// arguments mirror main.AgentChatRequest; keeping that DTO in package main
// preserves the existing Wails binding namespace (Phase 3 migrates it).
func (s *Service) AgentChat(ctx context.Context, p Principal, connectionID string, history []agent.Message, message string, confirmWrites bool, preferredSchema string) (*agent.ChatResponse, error) {
	return s.agentChat(ctx, p, connectionID, history, message, confirmWrites, preferredSchema, nil)
}

// AgentChatStream is AgentChat with a per-step emit callback so the server can
// stream live progress over SSE. A nil emit behaves exactly like AgentChat. The
// desktop Wails binding still calls AgentChat (unchanged signature and behavior);
// this streaming sibling is server-only and is deliberately NOT a bound method,
// so frontend/wailsjs stays byte-for-byte identical.
func (s *Service) AgentChatStream(ctx context.Context, p Principal, connectionID string, history []agent.Message, message string, confirmWrites bool, preferredSchema string, emit func(agent.StepEvent)) (*agent.ChatResponse, error) {
	return s.agentChat(ctx, p, connectionID, history, message, confirmWrites, preferredSchema, emit)
}

func (s *Service) agentChat(ctx context.Context, p Principal, connectionID string, history []agent.Message, message string, confirmWrites bool, preferredSchema string, emit func(agent.StepEvent)) (*agent.ChatResponse, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("message is empty")
	}
	s.mu.Lock()
	c, err := s.findConnLocked(ctx, p, connectionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cfg, err := s.ensureCfgLocked(ctx, p)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	llm := cfg.LLM
	s.mu.Unlock()
	// SSRF: AgentChat dialls the active profile's BaseURL and surfaces the LLM
	// error text to the caller, so veto the host before running. Nil policy
	// (desktop) allows everything.
	if err := s.allowLLMBase(ctx, llm.BaseURL); err != nil {
		return nil, err
	}
	return s.agent.RunStream(ctx, llm, c, history, message, confirmWrites, preferredSchema, emit)
}
