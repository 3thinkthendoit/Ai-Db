package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"AI-DB/internal/config"
	"AI-DB/internal/db"
	"AI-DB/internal/egress"
	"AI-DB/internal/store"
)

// corruptStore mimics a desktop FileStore whose config.json is corrupt or
// undecryptable: every Load surfaces an error, while Save succeeds (and would
// overwrite the bad file on disk).
type corruptStore struct {
	loadErr  error
	saved    *config.AppConfig
	saveCals int
}

func (c *corruptStore) Load(context.Context, string) (*config.AppConfig, error) {
	return nil, c.loadErr
}

func (c *corruptStore) Save(_ context.Context, _ string, cfg *config.AppConfig) error {
	c.saveCals++
	c.saved = cfg
	return nil
}

var _ store.ConfigStore = (*corruptStore)(nil)

// TestCorruptStoreWithoutPrimeSurfacesError documents the server-safe default:
// a load failure is NOT frozen into an empty config, so reads/writes surface it
// and a later request can retry the load rather than wipe real data.
func TestCorruptStoreWithoutPrimeSurfacesError(t *testing.T) {
	cs := &corruptStore{loadErr: errors.New("corrupt config")}
	svc := NewService(cs, db.NewManager())
	ctx := context.Background()
	p := LocalPrincipal()

	if _, err := svc.ListConnections(ctx, p); err == nil {
		t.Fatal("expected load error to surface without priming, got nil")
	}
	// A write must abort before reaching Save, so the corrupt file is untouched.
	if _, err := svc.UpsertConnection(ctx, p, config.Connection{Type: "mysql", Name: "x"}); err == nil {
		t.Fatal("expected write to abort on load error without priming, got nil")
	}
	if cs.saveCals != 0 {
		t.Fatalf("expected no Save without priming, got %d", cs.saveCals)
	}
}

// TestPrimeDefaultsRestoresDesktopSelfHeal is the desktop regression guard: after
// PrimeDefaults (called by app.go startup on a FileStore load failure) reads
// return defaults and the next write Saves over the bad file — matching the
// pre-refactor startup fallback that kept the store and overwrote corrupt config.
func TestPrimeDefaultsRestoresDesktopSelfHeal(t *testing.T) {
	cs := &corruptStore{loadErr: errors.New("corrupt config")}
	svc := NewService(cs, db.NewManager())
	ctx := context.Background()
	p := LocalPrincipal()

	svc.PrimeDefaults(p)

	conns, err := svc.ListConnections(ctx, p)
	if err != nil {
		t.Fatalf("reads must work after priming, got err: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("expected empty defaults after priming, got %d connections", len(conns))
	}

	saved, err := svc.UpsertConnection(ctx, p, config.Connection{Type: "mysql", Name: "x", Host: "127.0.0.1", Port: 3306})
	if err != nil {
		t.Fatalf("write must succeed after priming (self-heal), got err: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("expected an assigned id on the upserted connection")
	}
	if cs.saveCals != 1 {
		t.Fatalf("expected exactly one Save to overwrite the corrupt file, got %d", cs.saveCals)
	}
	if cs.saved == nil || len(cs.saved.Connections) != 1 {
		t.Fatal("expected the saved document to contain the new connection")
	}

	// PrimeDefaults is idempotent: it must not clobber an already-cached config.
	svc.PrimeDefaults(p)
	conns2, err := svc.ListConnections(ctx, p)
	if err != nil {
		t.Fatalf("reads must still work, got err: %v", err)
	}
	if len(conns2) != 1 {
		t.Fatalf("re-priming must not wipe the cached config, got %d connections", len(conns2))
	}
}

// mapStore is a working in-memory ConfigStore for the hardening tests (unlike
// corruptStore, its Load succeeds so writes reach their business logic).
type mapStore struct{ docs map[string]*config.AppConfig }

func (m *mapStore) Load(_ context.Context, uid string) (*config.AppConfig, error) {
	if c, ok := m.docs[uid]; ok {
		return c, nil
	}
	return &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}, nil
}

func (m *mapStore) Save(_ context.Context, uid string, cfg *config.AppConfig) error {
	if m.docs == nil {
		m.docs = map[string]*config.AppConfig{}
	}
	m.docs[uid] = cfg
	return nil
}

var _ store.ConfigStore = (*mapStore)(nil)

// TestRequireExplicitHostBlocksEmptyHost is the server posture: an empty host is
// rejected instead of silently defaulting to 127.0.0.1 ("the server itself").
func TestRequireExplicitHostBlocksEmptyHost(t *testing.T) {
	svc := NewService(&mapStore{}, db.NewManager())
	svc.SetHardening(Hardening{RequireExplicitHost: true})
	_, err := svc.UpsertConnection(context.Background(), LocalPrincipal(), config.Connection{Type: "mysql", Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("expected 'host is required', got %v", err)
	}
}

// TestDesktopDefaultsHostToLocalhost guards zero regression: with no hardening the
// empty host still defaults to 127.0.0.1 exactly as before Phase 4.
func TestDesktopDefaultsHostToLocalhost(t *testing.T) {
	svc := NewService(&mapStore{}, db.NewManager())
	saved, err := svc.UpsertConnection(context.Background(), LocalPrincipal(), config.Connection{Type: "mysql", Name: "x"})
	if err != nil {
		t.Fatalf("desktop upsert should succeed, got %v", err)
	}
	if saved.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %q", saved.Host)
	}
}

// TestEgressBlocksTestConnection proves the SSRF oracle is closed before any dial:
// a blocked host is rejected without contacting it (no DB is reachable in tests,
// so reaching the dial would surface a different, network-style error).
func TestEgressBlocksTestConnection(t *testing.T) {
	pol, err := egress.New(nil, nil)
	if err != nil {
		t.Fatalf("egress.New: %v", err)
	}
	svc := NewService(&mapStore{}, db.NewManager())
	svc.SetHardening(Hardening{Egress: pol})
	err = svc.TestConnection(context.Background(), LocalPrincipal(), config.Connection{Type: "mysql", Name: "probe", Host: "169.254.169.254", Port: 80})
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatalf("expected metadata host to be blocked, got %v", err)
	}
}

// TestEgressBlocksConnect proves Connect vetoes a saved connection whose host is
// blocked, again before opening a driver.
func TestEgressBlocksConnect(t *testing.T) {
	pol, err := egress.New(nil, nil)
	if err != nil {
		t.Fatalf("egress.New: %v", err)
	}
	svc := NewService(&mapStore{}, db.NewManager())
	svc.SetHardening(Hardening{Egress: pol})
	ctx := context.Background()
	p := LocalPrincipal()
	saved, err := svc.UpsertConnection(ctx, p, config.Connection{Type: "mysql", Name: "meta", Host: "169.254.169.254", Port: 3306})
	if err != nil {
		t.Fatalf("saving a connection is not egress-checked, got %v", err)
	}
	err = svc.Connect(ctx, p, saved.ID)
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatalf("expected Connect to block the metadata host, got %v", err)
	}
}

// TestEgressBlocksLLMBaseURL proves the SSRF veto also covers the LLM egress path,
// not just DB dials. The LLM probe/agent echo the response (or its error) back, so
// an unrestricted BaseURL is an even better metadata oracle than TestConnection:
// saving such a profile is refused before it is persisted, and the probe reports
// the block instead of dialling.
func TestEgressBlocksLLMBaseURL(t *testing.T) {
	pol, err := egress.New(nil, nil)
	if err != nil {
		t.Fatalf("egress.New: %v", err)
	}
	svc := NewService(&mapStore{}, db.NewManager())
	svc.SetHardening(Hardening{Egress: pol})
	ctx := context.Background()
	p := LocalPrincipal()

	if _, err := svc.SaveLLMProfile(ctx, p, "", "meta", "http://169.254.169.254/latest", "m", "k", false); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatalf("expected SaveLLMProfile to block the metadata host, got %v", err)
	}

	ok, msg, _, _ := svc.TestLLMProfile(ctx, p, "", "http://169.254.169.254/latest", "m", "k")
	if ok || !strings.Contains(msg, "blocked by policy") {
		t.Fatalf("expected TestLLMProfile to block the metadata host, got ok=%v msg=%q", ok, msg)
	}
}

// TestDesktopAllowsArbitraryLLMBase guards zero regression: with no egress policy
// (the desktop posture) any LLM endpoint — including loopback for a local model —
// is accepted exactly as before Phase 4.
func TestDesktopAllowsArbitraryLLMBase(t *testing.T) {
	svc := NewService(&mapStore{}, db.NewManager())
	v, err := svc.SaveLLMProfile(context.Background(), LocalPrincipal(), "", "local", "http://127.0.0.1:11434/v1", "m", "k", false)
	if err != nil {
		t.Fatalf("desktop must allow a loopback LLM endpoint, got %v", err)
	}
	if v.ID == "" {
		t.Fatal("expected an assigned profile id")
	}
}
