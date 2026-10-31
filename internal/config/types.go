package config

// ConnType is the database engine kind.
type ConnType string

const (
	ConnMySQL    ConnType = "mysql"
	ConnPostgres ConnType = "postgres"
	ConnRedis    ConnType = "redis"
	ConnMongo    ConnType = "mongo"
)

// Connection is a saved database connection profile.
type Connection struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        ConnType `json:"type"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	Password    string   `json:"password"`
	PasswordSet bool     `json:"passwordSet,omitempty"`
	Database    string   `json:"database"`
	// AuthSource is MongoDB authentication database (e.g. admin). Empty → driver default.
	AuthSource string `json:"authSource,omitempty"`
	// SSLMode is PostgreSQL sslmode (disable/allow/prefer/require/verify-ca/verify-full).
	// Empty → driver default (prefer).
	SSLMode string `json:"sslMode,omitempty"`
}

// LLMProfile is one saved OpenAI-compatible endpoint/model/key set.
type LLMProfile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey string `json:"apiKey"`
	Model  string `json:"model"`
}

// LLMProfileView is the UI-safe view of a profile (no raw API key).
type LLMProfileView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"hasApiKey"`
	Active    bool   `json:"active"`
}

// LLMConfig holds the active LLM endpoint plus global safety knobs.
// BaseURL/APIKey/Model mirror the active profile (kept for agent runtime).
type LLMConfig struct {
	BaseURL       string `json:"baseUrl"`
	APIKey        string `json:"apiKey"`
	Model         string `json:"model"`
	AllowWrites   bool   `json:"allowWrites"`
	MaxRows       int    `json:"maxRows"`
	MaxAgentSteps int    `json:"maxAgentSteps"`
}

// AppConfig is the persisted application config.
type AppConfig struct {
	Connections  []Connection  `json:"connections"`
	LLMProfiles  []LLMProfile  `json:"llmProfiles"`
	ActiveLLMID  string        `json:"activeLLMId"`
	LLM          LLMConfig     `json:"llm"`
}

// DefaultLLMProfileID is the migrated/default profile id (stable across reloads).
const DefaultLLMProfileID = "default"

// DefaultLLMProfileName is the locale-neutral label for the migrated profile.
const DefaultLLMProfileName = "Default"

// DefaultLLM returns sensible MVP defaults.
func DefaultLLM() LLMConfig {
	return LLMConfig{
		BaseURL:       "https://api.openai.com/v1",
		APIKey:        "",
		Model:         "gpt-4o-mini",
		AllowWrites:   false,
		MaxRows:       500,
		MaxAgentSteps: 12,
	}
}

// DefaultPort returns the usual port for a connection type.
func DefaultPort(t ConnType) int {
	switch t {
	case ConnMySQL:
		return 3306
	case ConnPostgres:
		return 5432
	case ConnRedis:
		return 6379
	case ConnMongo:
		return 27017
	default:
		return 0
	}
}

// PublicConnection returns a copy safe for the UI (password cleared).
func PublicConnection(c Connection) Connection {
	out := c
	out.PasswordSet = c.Password != ""
	out.Password = ""
	return out
}

// NormalizeLLMProfiles migrates legacy single-LLM config into profiles and syncs active → LLM.
func NormalizeLLMProfiles(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	if cfg.LLMProfiles == nil {
		cfg.LLMProfiles = []LLMProfile{}
	}
	def := DefaultLLM()
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = def.BaseURL
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = def.Model
	}
	if cfg.LLM.MaxRows <= 0 {
		cfg.LLM.MaxRows = def.MaxRows
	}
	if cfg.LLM.MaxAgentSteps <= 0 {
		cfg.LLM.MaxAgentSteps = def.MaxAgentSteps
	}

	if len(cfg.LLMProfiles) == 0 {
		cfg.LLMProfiles = []LLMProfile{{
			ID:      DefaultLLMProfileID,
			Name:    DefaultLLMProfileName,
			BaseURL: cfg.LLM.BaseURL,
			APIKey:  cfg.LLM.APIKey,
			Model:   cfg.LLM.Model,
		}}
		cfg.ActiveLLMID = DefaultLLMProfileID
	}

	if cfg.ActiveLLMID == "" || !hasLLMProfile(cfg, cfg.ActiveLLMID) {
		cfg.ActiveLLMID = cfg.LLMProfiles[0].ID
	}
	SyncActiveLLM(cfg)
}

func hasLLMProfile(cfg *AppConfig, id string) bool {
	for _, p := range cfg.LLMProfiles {
		if p.ID == id {
			return true
		}
	}
	return false
}

// SyncActiveLLM copies the active profile endpoint fields onto cfg.LLM.
func SyncActiveLLM(cfg *AppConfig) {
	for _, p := range cfg.LLMProfiles {
		if p.ID == cfg.ActiveLLMID {
			cfg.LLM.BaseURL = p.BaseURL
			cfg.LLM.APIKey = p.APIKey
			cfg.LLM.Model = p.Model
			return
		}
	}
	if len(cfg.LLMProfiles) > 0 {
		cfg.ActiveLLMID = cfg.LLMProfiles[0].ID
		cfg.LLM.BaseURL = cfg.LLMProfiles[0].BaseURL
		cfg.LLM.APIKey = cfg.LLMProfiles[0].APIKey
		cfg.LLM.Model = cfg.LLMProfiles[0].Model
	}
}

// ProfileViews returns UI-safe profile rows.
func ProfileViews(cfg *AppConfig) []LLMProfileView {
	out := make([]LLMProfileView, 0, len(cfg.LLMProfiles))
	for _, p := range cfg.LLMProfiles {
		out = append(out, LLMProfileView{
			ID:        p.ID,
			Name:      p.Name,
			BaseURL:   p.BaseURL,
			Model:     p.Model,
			HasAPIKey: p.APIKey != "",
			Active:    p.ID == cfg.ActiveLLMID,
		})
	}
	return out
}
