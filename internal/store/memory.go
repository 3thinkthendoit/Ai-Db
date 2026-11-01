package store

import (
	"context"
	"sync"

	"AI-DB/internal/config"
)

// MemoryStore is an in-memory ConfigStore keyed by userID. It backs unit tests
// and any ephemeral scenario where durable persistence is unnecessary.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]*config.AppConfig
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]*config.AppConfig)}
}

// Load returns the user's config, or normalised defaults when nothing is stored
// yet. The returned document is a deep copy so callers cannot mutate the
// stored state without going through Save.
func (m *MemoryStore) Load(_ context.Context, userID string) (*config.AppConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, ok := m.data[userID]
	if !ok {
		cfg = &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}
		config.NormalizeLLMProfiles(cfg)
		return cfg, nil
	}
	return deepCopy(cfg), nil
}

// Save stores a deep copy of the user's config.
func (m *MemoryStore) Save(_ context.Context, userID string, cfg *config.AppConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[userID] = deepCopy(cfg)
	return nil
}

// deepCopy clones an AppConfig, giving the Connections and LLMProfiles slices
// their own backing arrays. Both element types are pure value structs (only
// string/int/bool fields), so copy() yields a full independent copy.
func deepCopy(cfg *config.AppConfig) *config.AppConfig {
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
