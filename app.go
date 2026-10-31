package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/agent"
	"AI-DB/internal/config"
	"AI-DB/internal/db"

	"github.com/google/uuid"
)

// App is the Wails-bound application facade.
type App struct {
	ctx   context.Context
	store *config.Store
	mgr   *db.Manager
	agent *agent.Service

	mu  sync.Mutex
	cfg *config.AppConfig
}

// NewApp creates the application.
func NewApp() *App {
	return &App{
		mgr: db.NewManager(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	store, err := config.NewStore()
	if err != nil {
		fallback := filepath.Join(os.TempDir(), "aidb")
		fmt.Println("config store error, falling back to", fallback, ":", err)
		store, err = config.NewStoreAt(fallback)
		if err != nil {
			fmt.Println("config fallback failed:", err)
			a.mu.Lock()
			a.cfg = &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
			a.mu.Unlock()
			a.agent = agent.NewService(a.mgr)
			return
		}
	}
	a.store = store
	cfg, err := store.Load()
	if err != nil {
		fmt.Println("config load error:", err)
		cfg = &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.agent = agent.NewService(a.mgr)
}

func (a *App) shutdown(ctx context.Context) {
	a.mgr.CloseAll()
}

func (a *App) ensureCfgLocked() *config.AppConfig {
	if a.cfg == nil {
		a.cfg = &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
	}
	config.NormalizeLLMProfiles(a.cfg)
	return a.cfg
}

func (a *App) persistLocked() error {
	if a.store == nil {
		return fmt.Errorf("config store not ready")
	}
	return a.store.Save(a.ensureCfgLocked())
}

func (a *App) findConnLocked(id string) (config.Connection, error) {
	for _, c := range a.ensureCfgLocked().Connections {
		if c.ID == id {
			return c, nil
		}
	}
	return config.Connection{}, fmt.Errorf("connection not found: %s", id)
}

func (a *App) bgCtx() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// ListConnections returns saved connections without passwords.
func (a *App) ListConnections() []config.Connection {
	a.mu.Lock()
	defer a.mu.Unlock()
	src := a.ensureCfgLocked().Connections
	out := make([]config.Connection, 0, len(src))
	for _, c := range src {
		out = append(out, config.PublicConnection(c))
	}
	return out
}

// UpsertConnection creates or updates a connection profile.
// Empty / masked password on update keeps the existing secret.
func (a *App) UpsertConnection(c config.Connection) (config.Connection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
	if c.Type == "" {
		return c, fmt.Errorf("type is required")
	}
	if c.Name == "" {
		return c, fmt.Errorf("name is required")
	}
	if c.Host == "" {
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
	if err := a.persistLocked(); err != nil {
		return config.PublicConnection(c), err
	}
	return config.PublicConnection(c), nil
}

// DeleteConnection removes a connection and disconnects it.
func (a *App) DeleteConnection(id string) error {
	_ = a.mgr.Disconnect(id)
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
	next := make([]config.Connection, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		if c.ID != id {
			next = append(next, c)
		}
	}
	cfg.Connections = next
	return a.persistLocked()
}

// TestConnection tests connectivity using the provided profile.
// Empty password with an existing id reuses the stored password.
func (a *App) TestConnection(c config.Connection) error {
	a.mu.Lock()
	if c.ID != "" && (c.Password == "" || strings.HasPrefix(c.Password, "••••")) {
		if existing, err := a.findConnLocked(c.ID); err == nil {
			c.Password = existing.Password
			if c.User == "" {
				c.User = existing.User
			}
		}
	}
	a.mu.Unlock()
	return db.TestConnection(a.bgCtx(), c)
}

// Connect opens a live driver for the saved connection id.
func (a *App) Connect(id string) error {
	a.mu.Lock()
	c, err := a.findConnLocked(id)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return a.mgr.Connect(a.bgCtx(), c)
}

// Disconnect closes a live driver.
func (a *App) Disconnect(id string) error {
	return a.mgr.Disconnect(id)
}

// ListObjects returns top-level browser nodes (schemas / redis db); expand via ExpandObject.
func (a *App) ListObjects(connectionID string) ([]db.ObjectNode, error) {
	drv, release, err := a.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.ListObjects(a.bgCtx())
}

// ExpandObject loads children for an object-tree path (schema → folders → tables/indexes…).
func (a *App) ExpandObject(connectionID string, path []string) ([]db.ObjectNode, error) {
	drv, release, err := a.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.ExpandObject(a.bgCtx(), path)
}

// ExecuteQuery runs SQL or a Redis command.
func (a *App) ExecuteQuery(connectionID, query string) (*db.QueryResult, error) {
	a.mu.Lock()
	cfg := *a.ensureCfgLocked()
	a.mu.Unlock()
	drv, release, err := a.mgr.Acquire(connectionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return drv.Execute(a.bgCtx(), query, cfg.LLM.MaxRows, cfg.LLM.AllowWrites)
}

// SetActiveSchema remembers the workbench-selected database/schema for an open connection.
func (a *App) SetActiveSchema(connectionID, schema string) error {
	return a.mgr.SetActiveSchema(connectionID, schema)
}

// GetLLMConfig returns active LLM settings with masked API key (plus safety knobs).
func (a *App) GetLLMConfig() config.LLMConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return config.MaskedLLM(a.ensureCfgLocked().LLM)
}

// ListLLMProfiles returns UI-safe LLM provider cards.
func (a *App) ListLLMProfiles() []config.LLMProfileView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return config.ProfileViews(a.ensureCfgLocked())
}

// SaveLLMProfileRequest is create/update input for one provider.
type SaveLLMProfileRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
	ClearKey bool  `json:"clearKey"`
}

// SaveLLMProfile creates or updates a provider profile.
func (a *App) SaveLLMProfile(req SaveLLMProfileRequest) (config.LLMProfileView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()

	name := strings.TrimSpace(req.Name)
	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	model := strings.TrimSpace(req.Model)
	if name == "" {
		return config.LLMProfileView{}, fmt.Errorf("name is required")
	}
	if base == "" {
		return config.LLMProfileView{}, fmt.Errorf("base URL is required")
	}
	if model == "" {
		return config.LLMProfileView{}, fmt.Errorf("model is required")
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = uuid.NewString()
		key := strings.TrimSpace(req.APIKey)
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
		for i, p := range cfg.LLMProfiles {
			if p.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return config.LLMProfileView{}, fmt.Errorf("profile not found")
		}
		p := cfg.LLMProfiles[idx]
		p.Name, p.BaseURL, p.Model = name, base, model
		if req.ClearKey {
			p.APIKey = ""
		} else if k := strings.TrimSpace(req.APIKey); k != "" && !strings.HasPrefix(k, "••••") {
			p.APIKey = k
		}
		cfg.LLMProfiles[idx] = p
	}

	config.SyncActiveLLM(cfg)
	if err := a.persistLocked(); err != nil {
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
func (a *App) DeleteLLMProfile(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
	if len(cfg.LLMProfiles) <= 1 {
		return fmt.Errorf("keep at least one LLM profile")
	}
	next := make([]config.LLMProfile, 0, len(cfg.LLMProfiles)-1)
	found := false
	for _, p := range cfg.LLMProfiles {
		if p.ID == id {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return fmt.Errorf("profile not found")
	}
	cfg.LLMProfiles = next
	if cfg.ActiveLLMID == id {
		cfg.ActiveLLMID = cfg.LLMProfiles[0].ID
	}
	config.SyncActiveLLM(cfg)
	return a.persistLocked()
}

// ActivateLLMProfile switches the active provider for the next agent turn.
func (a *App) ActivateLLMProfile(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
	if !hasProfileID(cfg, id) {
		return fmt.Errorf("profile not found")
	}
	cfg.ActiveLLMID = id
	config.SyncActiveLLM(cfg)
	return a.persistLocked()
}

func hasProfileID(cfg *config.AppConfig, id string) bool {
	for _, p := range cfg.LLMProfiles {
		if p.ID == id {
			return true
		}
	}
	return false
}

// SaveLLMSafety updates global safety knobs (writes / row / step limits).
func (a *App) SaveLLMSafety(allowWrites bool, maxRows, maxAgentSteps int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
	if maxRows <= 0 {
		return fmt.Errorf("maxRows must be >= 1")
	}
	if maxAgentSteps <= 0 {
		return fmt.Errorf("maxAgentSteps must be >= 1")
	}
	cfg.LLM.AllowWrites = allowWrites
	cfg.LLM.MaxRows = maxRows
	cfg.LLM.MaxAgentSteps = maxAgentSteps
	return a.persistLocked()
}

// LLMTestResult is the outcome of a connectivity probe.
type LLMTestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Model      string `json:"model"`
	DurationMs int64  `json:"durationMs"`
}

// TestLLMProfile probes chat completions. Empty form fields fall back to the saved profile.
func (a *App) TestLLMProfile(profileID, baseURL, model, apiKey string) LLMTestResult {
	a.mu.Lock()
	cfg := a.ensureCfgLocked()
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	mod := strings.TrimSpace(model)
	key := strings.TrimSpace(apiKey)
	if strings.HasPrefix(key, "••••") {
		key = ""
	}
	if profileID != "" {
		found := false
		for _, p := range cfg.LLMProfiles {
			if p.ID == profileID {
				found = true
				if base == "" {
					base = p.BaseURL
				}
				if mod == "" {
					mod = p.Model
				}
				if key == "" {
					key = p.APIKey
				}
				break
			}
		}
		if !found {
			a.mu.Unlock()
			return LLMTestResult{Message: "profile not found"}
		}
	}
	a.mu.Unlock()

	if base == "" || mod == "" {
		return LLMTestResult{Message: "请先填写 Base URL 与模型名"}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(a.bgCtx(), 20*time.Second)
	defer cancel()
	client := agent.NewClient(config.LLMConfig{BaseURL: base, APIKey: key, Model: mod})
	_, err := client.Chat(ctx, []agent.Message{{Role: "user", Content: "ping"}}, nil)
	out := LLMTestResult{Model: mod, DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		out.Message = err.Error()
		return out
	}
	out.OK = true
	out.Message = "连接成功"
	return out
}

// SaveLLMConfig updates safety knobs and the active profile endpoint (compat).
func (a *App) SaveLLMConfig(llm config.LLMConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.ensureCfgLocked()
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
	cfg.LLM.AllowWrites = llm.AllowWrites
	cfg.LLM.MaxRows = llm.MaxRows
	cfg.LLM.MaxAgentSteps = llm.MaxAgentSteps

	// Mirror endpoint onto active profile.
	for i, p := range cfg.LLMProfiles {
		if p.ID == cfg.ActiveLLMID {
			p.BaseURL = strings.TrimRight(strings.TrimSpace(llm.BaseURL), "/")
			p.Model = strings.TrimSpace(llm.Model)
			p.APIKey = llm.APIKey
			cfg.LLMProfiles[i] = p
			break
		}
	}
	config.SyncActiveLLM(cfg)
	return a.persistLocked()
}

// AgentChatRequest is the frontend payload for an agent turn.
type AgentChatRequest struct {
	ConnectionID     string          `json:"connectionId"`
	History          []agent.Message `json:"history"`
	Message          string          `json:"message"`
	ConfirmWrites    bool            `json:"confirmWrites"`
	PreferredSchema  string          `json:"preferredSchema"`
}

// AgentChat runs one user message through the multi-step agent.
func (a *App) AgentChat(req AgentChatRequest) (*agent.ChatResponse, error) {
	if strings.TrimSpace(req.Message) == "" {
		return nil, fmt.Errorf("message is empty")
	}
	a.mu.Lock()
	c, err := a.findConnLocked(req.ConnectionID)
	llm := a.ensureCfgLocked().LLM
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if a.agent == nil {
		a.agent = agent.NewService(a.mgr)
	}
	return a.agent.Run(a.bgCtx(), llm, c, req.History, req.Message, req.ConfirmWrites, req.PreferredSchema)
}

// IsMutatingQuery reports whether the query looks like a write/DDL/admin command.
func (a *App) IsMutatingQuery(connectionID, query string) bool {
	a.mu.Lock()
	c, err := a.findConnLocked(connectionID)
	a.mu.Unlock()
	if err != nil {
		return false
	}
	if c.Type == config.ConnRedis {
		return db.EnsureRedisReadOnly(query, false) != nil
	}
	if c.Type == config.ConnMongo {
		return db.MongoQueryMutating(query)
	}
	return db.EnsureReadOnly(query, false) != nil
}

// WritesAllowed reports whether mutating queries are enabled in settings.
func (a *App) WritesAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureCfgLocked().LLM.AllowWrites
}
