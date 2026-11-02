package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"AI-DB/internal/agent"
	"AI-DB/internal/app"
	"AI-DB/internal/config"
	"AI-DB/internal/db"
	"AI-DB/internal/store"
)

// App is the Wails-bound desktop facade. It is a thin adapter: every bound method
// forwards to the shared app.Service using the implicit LocalPrincipal, so the
// Wails bindings — and therefore the desktop frontend — are unchanged while the
// real logic now lives in internal/app (shared with the server build).
type App struct {
	ctx context.Context
	mgr *db.Manager
	svc *app.Service
}

// NewApp creates the application.
func NewApp() *App {
	return &App{
		mgr: db.NewManager(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Build the desktop config store, falling back to a temp dir and then to
	// in-memory exactly as before. app.Service tolerates a nil store: reads use
	// defaults and writes report "config store not ready".
	var cs store.ConfigStore
	s, err := config.NewStore()
	if err != nil {
		fallback := filepath.Join(os.TempDir(), "aidb")
		fmt.Println("config store error, falling back to", fallback, ":", err)
		s, err = config.NewStoreAt(fallback)
		if err != nil {
			fmt.Println("config fallback failed:", err)
		}
	}
	if s != nil {
		cs = store.NewFileStore(s)
	}
	a.svc = app.NewService(cs, a.mgr)

	// Reproduce the legacy desktop self-heal: if the local config file is corrupt
	// or undecryptable, prime in-memory defaults so reads work and the next write
	// overwrites the bad file (the pre-refactor startup fell back to defaults and
	// kept the store). A nil store (config dir unwritable) is left alone — Service
	// already yields defaults for reads and "not ready" for writes. The server
	// build never primes; a transient MySQLStore load error must surface, not be
	// frozen into an empty doc. See Service.PrimeDefaults.
	if cs != nil {
		if _, err := cs.Load(ctx, a.local().UserID); err != nil {
			fmt.Println("config load error, using in-memory defaults:", err)
			a.svc.PrimeDefaults(a.local())
		}
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.mgr.CloseAll()
}

// bgCtx is the long-lived desktop context (Wails cancels it on shutdown).
func (a *App) bgCtx() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// local is the desktop's implicit single-user principal.
func (a *App) local() app.Principal { return app.LocalPrincipal() }

// —— connections ——

// ListConnections returns saved connections without passwords. The Wails binding
// signature has no error return, so a load failure degrades to an empty slice —
// never nil — preserving the frontend's array contract and matching pre-refactor
// behaviour (startup fell back to empty defaults).
func (a *App) ListConnections() []config.Connection {
	out, err := a.svc.ListConnections(a.bgCtx(), a.local())
	if err != nil || out == nil {
		return []config.Connection{}
	}
	return out
}

// UpsertConnection creates or updates a connection profile.
// Empty / masked password on update keeps the existing secret.
func (a *App) UpsertConnection(c config.Connection) (config.Connection, error) {
	return a.svc.UpsertConnection(a.bgCtx(), a.local(), c)
}

// DeleteConnection removes a connection and disconnects it.
func (a *App) DeleteConnection(id string) error {
	return a.svc.DeleteConnection(a.bgCtx(), a.local(), id)
}

// TestConnection tests connectivity using the provided profile.
// Empty password with an existing id reuses the stored password.
func (a *App) TestConnection(c config.Connection) error {
	return a.svc.TestConnection(a.bgCtx(), a.local(), c)
}

// Connect opens a live driver for the saved connection id.
func (a *App) Connect(id string) error {
	return a.svc.Connect(a.bgCtx(), a.local(), id)
}

// Disconnect closes a live driver.
func (a *App) Disconnect(id string) error {
	return a.svc.Disconnect(a.bgCtx(), a.local(), id)
}

// —— object tree / query ——

// ListObjects returns top-level browser nodes (schemas / redis db); expand via ExpandObject.
func (a *App) ListObjects(connectionID string) ([]db.ObjectNode, error) {
	return a.svc.ListObjects(a.bgCtx(), a.local(), connectionID)
}

// ExpandObject loads children for an object-tree path (schema → folders → tables/indexes…).
func (a *App) ExpandObject(connectionID string, path []string) ([]db.ObjectNode, error) {
	return a.svc.ExpandObject(a.bgCtx(), a.local(), connectionID, path)
}

// ExecuteQuery runs SQL or a Redis command.
func (a *App) ExecuteQuery(connectionID, query string) (*db.QueryResult, error) {
	return a.svc.ExecuteQuery(a.bgCtx(), a.local(), connectionID, query)
}

// CreateDatabaseOptions returns charset/collation (MySQL) or encoding (Postgres)
// candidates for the create-database dialog.
func (a *App) CreateDatabaseOptions(connectionID string) (db.CreateDatabaseOptions, error) {
	return a.svc.CreateDatabaseOptions(a.bgCtx(), a.local(), connectionID)
}

// CreateDatabase creates a database and optionally a login account that is
// granted on it (MySQL: CREATE USER + GRANT ALL; Postgres: ROLE + OWNER).
func (a *App) CreateDatabase(connectionID string, spec db.CreateDatabaseSpec) (*db.CreateDatabaseResult, error) {
	return a.svc.CreateDatabase(a.bgCtx(), a.local(), connectionID, spec)
}

// SetActiveSchema remembers the workbench-selected database/schema for an open connection.
func (a *App) SetActiveSchema(connectionID, schema string) error {
	return a.svc.SetActiveSchema(a.bgCtx(), a.local(), connectionID, schema)
}

// IsMutatingQuery reports whether the query looks like a write/DDL/admin command.
func (a *App) IsMutatingQuery(connectionID, query string) bool {
	b, _ := a.svc.IsMutatingQuery(a.bgCtx(), a.local(), connectionID, query)
	return b
}

// WritesAllowed reports whether mutating queries are enabled in settings.
func (a *App) WritesAllowed() bool {
	b, _ := a.svc.WritesAllowed(a.bgCtx(), a.local())
	return b
}

// —— LLM ——

// GetLLMConfig returns active LLM settings with masked API key (plus safety knobs).
func (a *App) GetLLMConfig() config.LLMConfig {
	cfg, _ := a.svc.GetLLMConfig(a.bgCtx(), a.local())
	return cfg
}

// ListLLMProfiles returns UI-safe LLM provider cards. As with ListConnections the
// binding has no error return, so a failure degrades to an empty slice, never nil.
func (a *App) ListLLMProfiles() []config.LLMProfileView {
	out, err := a.svc.ListLLMProfiles(a.bgCtx(), a.local())
	if err != nil || out == nil {
		return []config.LLMProfileView{}
	}
	return out
}

// SaveLLMProfileRequest is create/update input for one provider.
//
// Kept in package main so the Wails-generated binding namespace stays main.*;
// the app.Service takes the equivalent decomposed arguments.
type SaveLLMProfileRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
	ClearKey bool   `json:"clearKey"`
}

// SaveLLMProfile creates or updates a provider profile.
func (a *App) SaveLLMProfile(req SaveLLMProfileRequest) (config.LLMProfileView, error) {
	return a.svc.SaveLLMProfile(a.bgCtx(), a.local(), req.ID, req.Name, req.BaseURL, req.Model, req.APIKey, req.ClearKey)
}

// DeleteLLMProfile removes a provider (at least one must remain).
func (a *App) DeleteLLMProfile(id string) error {
	return a.svc.DeleteLLMProfile(a.bgCtx(), a.local(), id)
}

// ActivateLLMProfile switches the active provider for the next agent turn.
func (a *App) ActivateLLMProfile(id string) error {
	return a.svc.ActivateLLMProfile(a.bgCtx(), a.local(), id)
}

// SaveLLMSafety updates global safety knobs (writes / row / step limits).
func (a *App) SaveLLMSafety(allowWrites bool, maxRows, maxAgentSteps int) error {
	return a.svc.SaveLLMSafety(a.bgCtx(), a.local(), allowWrites, maxRows, maxAgentSteps)
}

// LLMTestResult is the outcome of a connectivity probe.
//
// Kept in package main so the Wails-generated binding namespace stays main.*;
// the app.Service returns the equivalent decomposed fields.
type LLMTestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Model      string `json:"model"`
	DurationMs int64  `json:"durationMs"`
}

// TestLLMProfile probes chat completions. Empty form fields fall back to the saved profile.
func (a *App) TestLLMProfile(profileID, baseURL, model, apiKey string) LLMTestResult {
	ok, message, resolvedModel, durationMs := a.svc.TestLLMProfile(a.bgCtx(), a.local(), profileID, baseURL, model, apiKey)
	return LLMTestResult{OK: ok, Message: message, Model: resolvedModel, DurationMs: durationMs}
}

// SaveLLMConfig updates safety knobs and the active profile endpoint (compat).
func (a *App) SaveLLMConfig(llm config.LLMConfig) error {
	return a.svc.SaveLLMConfig(a.bgCtx(), a.local(), llm)
}

// —— agent ——

// AgentChatRequest is the frontend payload for an agent turn.
//
// Kept in package main so the Wails-generated binding namespace stays main.*;
// the app.Service takes the equivalent decomposed arguments.
type AgentChatRequest struct {
	ConnectionID    string          `json:"connectionId"`
	History         []agent.Message `json:"history"`
	Message         string          `json:"message"`
	ConfirmWrites   bool            `json:"confirmWrites"`
	PreferredSchema string          `json:"preferredSchema"`
}

// AgentChat runs one user message through the multi-step agent.
func (a *App) AgentChat(req AgentChatRequest) (*agent.ChatResponse, error) {
	return a.svc.AgentChat(a.bgCtx(), a.local(), req.ConnectionID, req.History, req.Message, req.ConfirmWrites, req.PreferredSchema)
}
