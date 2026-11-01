package store

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"AI-DB/internal/config"

	_ "github.com/go-sql-driver/mysql"
)

// These integration tests exercise MySQLStore against a real (throwaway) MySQL
// database. They are gated on AIDB_TEST_MYSQL_DSN so the default `go test ./...`
// compiles them but skips at runtime — keeping CI green without a database. To
// run them for real:
//
//	AIDB_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/aidb_itest?parseTime=true' go test ./internal/store/
//
// Every row is keyed to a fixed test user id and removed on cleanup; FK CASCADE
// clears the child tables (connections / llm_profiles / user_settings).

const itestUserID = "itest-roundtrip-user"

func TestMySQLStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("AIDB_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AIDB_TEST_MYSQL_DSN not set; skipping MySQL integration test")
	}
	ctx := context.Background()

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sqlDB.Close()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Skipf("MySQL not reachable, skipping: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st := NewMySQLStore(sqlDB, cipher)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cleanup := func() {
		_, _ = sqlDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = ?", itestUserID)
	}
	cleanup() // clean slate before, in case a prior run aborted
	defer cleanup()

	now := time.Now().UTC()
	if _, err := sqlDB.ExecContext(ctx,
		"INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (?,?,?,?,?,?)",
		itestUserID, itestUserID, "x", "user", now, now); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	want := &config.AppConfig{
		Connections: []config.Connection{
			{ID: "c1", Name: "db-one", Type: config.ConnMySQL, Host: "10.0.0.1", Port: 3306, User: "u1", Password: "s3cr3t", Database: "app"},
			{ID: "c2", Name: "db-two", Type: config.ConnPostgres, Host: "10.0.0.2", Port: 5432, User: "u2", Password: "p@ssw0rd", Database: "analytics", SSLMode: "require"},
		},
		LLMProfiles: []config.LLMProfile{
			{ID: "p1", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o", APIKey: "sk-secret-1"},
			{ID: "p2", Name: "Local", BaseURL: "http://localhost:11434/v1", Model: "llama3", APIKey: ""},
		},
		ActiveLLMID: "p2",
		LLM:         config.LLMConfig{AllowWrites: true, MaxRows: 250, MaxAgentSteps: 7},
	}
	config.NormalizeLLMProfiles(want) // mirrors the active profile endpoint onto want.LLM

	if err := st.Save(ctx, itestUserID, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.Load(ctx, itestUserID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// connections round-trip: order preserved, passwords decrypted back.
	if len(got.Connections) != len(want.Connections) {
		t.Fatalf("connections len = %d, want %d", len(got.Connections), len(want.Connections))
	}
	for i := range want.Connections {
		g, w := got.Connections[i], want.Connections[i]
		if g.ID != w.ID || g.Name != w.Name || g.Type != w.Type || g.Host != w.Host ||
			g.Port != w.Port || g.User != w.User || g.Database != w.Database || g.SSLMode != w.SSLMode {
			t.Errorf("conn[%d] mismatch: got %+v want %+v", i, g, w)
		}
		if g.Password != w.Password {
			t.Errorf("conn[%d] password not round-tripped: got %q want %q", i, g.Password, w.Password)
		}
	}

	// llm_profiles round-trip: order preserved, api_key decrypted back.
	if len(got.LLMProfiles) != len(want.LLMProfiles) {
		t.Fatalf("profiles len = %d, want %d", len(got.LLMProfiles), len(want.LLMProfiles))
	}
	for i := range want.LLMProfiles {
		g, w := got.LLMProfiles[i], want.LLMProfiles[i]
		if g.ID != w.ID || g.Name != w.Name || g.BaseURL != w.BaseURL || g.Model != w.Model {
			t.Errorf("profile[%d] mismatch: got %+v want %+v", i, g, w)
		}
		if g.APIKey != w.APIKey {
			t.Errorf("profile[%d] api_key not round-tripped: got %q want %q", i, g.APIKey, w.APIKey)
		}
	}

	// user_settings round-trip: active id + safety knobs.
	if got.ActiveLLMID != want.ActiveLLMID {
		t.Errorf("ActiveLLMID = %q, want %q", got.ActiveLLMID, want.ActiveLLMID)
	}
	if got.LLM.AllowWrites != want.LLM.AllowWrites || got.LLM.MaxRows != want.LLM.MaxRows ||
		got.LLM.MaxAgentSteps != want.LLM.MaxAgentSteps {
		t.Errorf("safety knobs mismatch: got %+v want %+v", got.LLM, want.LLM)
	}
	// Endpoint fields are derived from the active profile (not stored on settings).
	if got.LLM.BaseURL != want.LLM.BaseURL || got.LLM.Model != want.LLM.Model || got.LLM.APIKey != want.LLM.APIKey {
		t.Errorf("active endpoint mismatch: got %q/%q/%q want %q/%q/%q",
			got.LLM.BaseURL, got.LLM.Model, got.LLM.APIKey, want.LLM.BaseURL, want.LLM.Model, want.LLM.APIKey)
	}

	// Secrets are encrypted at rest: read the raw columns back.
	var rawKey string
	if err := sqlDB.QueryRowContext(ctx, "SELECT api_key FROM llm_profiles WHERE id = ?", "p1").Scan(&rawKey); err != nil {
		t.Fatalf("read raw api_key: %v", err)
	}
	if !strings.HasPrefix(rawKey, "enc:") {
		t.Errorf("api_key not encrypted at rest: %q", rawKey)
	}
	var rawPwd string
	if err := sqlDB.QueryRowContext(ctx, "SELECT password FROM connections WHERE id = ?", "c1").Scan(&rawPwd); err != nil {
		t.Fatalf("read raw password: %v", err)
	}
	if !strings.HasPrefix(rawPwd, "enc:") {
		t.Errorf("connection password not encrypted at rest: %q", rawPwd)
	}

	// Tenant isolation: an unknown owner sees none of itestUserID's rows. (Load
	// synthesises a default LLM profile for an empty user, so assert absence of
	// OUR ids rather than an empty slice.)
	other, err := st.Load(ctx, "itest-nonexistent-owner")
	if err != nil {
		t.Fatalf("load other: %v", err)
	}
	if len(other.Connections) != 0 {
		t.Errorf("tenant isolation broken: other owner got %d connections", len(other.Connections))
	}
	for _, p := range other.LLMProfiles {
		if p.ID == "p1" || p.ID == "p2" {
			t.Errorf("tenant isolation broken: other owner sees profile %q", p.ID)
		}
	}
}
