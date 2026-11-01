package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AI-DB/internal/app"
	"AI-DB/internal/auth"
	"AI-DB/internal/config"
	"AI-DB/internal/db"
)

// memStore is a minimal in-memory ConfigStore so the wiring tests can exercise
// the full HTTP → auth → rpc → service → store path without MySQL.
type memStore struct{ docs map[string]*config.AppConfig }

func (m *memStore) Load(_ context.Context, userID string) (*config.AppConfig, error) {
	if cfg, ok := m.docs[userID]; ok {
		return cfg, nil
	}
	return &config.AppConfig{Connections: []config.Connection{}, LLM: config.DefaultLLM()}, nil
}

func (m *memStore) Save(_ context.Context, userID string, cfg *config.AppConfig) error {
	if m.docs == nil {
		m.docs = make(map[string]*config.AppConfig)
	}
	m.docs[userID] = cfg
	return nil
}

func newTestServer(_ *testing.T) (*server, *auth.SessionManager) {
	sessions := auth.NewSessionManager(make([]byte, 32), time.Hour, false)
	s := &server{
		svc:      app.NewService(&memStore{}, db.NewManager()),
		sessions: sessions,
	}
	return s, sessions
}

func sessionCookie(t *testing.T, sessions *auth.SessionManager, userID, username, role string) *http.Cookie {
	t.Helper()
	value, err := sessions.Issue(userID, username, role)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return sessions.Cookie(value)
}

// apiMethods is the full bound surface (mirrors frontend/wailsjs/go/main/App.d.ts).
var apiMethods = []string{
	"ListConnections", "UpsertConnection", "DeleteConnection", "TestConnection",
	"Connect", "Disconnect", "ListObjects", "ExpandObject", "ExecuteQuery",
	"SetActiveSchema", "IsMutatingQuery", "WritesAllowed", "GetLLMConfig",
	"ListLLMProfiles", "SaveLLMProfile", "DeleteLLMProfile", "ActivateLLMProfile",
	"SaveLLMSafety", "TestLLMProfile", "SaveLLMConfig", "AgentChat",
}

// TestRegisterAPIRoutes verifies all 21 endpoints register on the ServeMux
// without a pattern conflict (Go 1.22 mux panics at registration on conflict)
// and that the session gate rejects unauthenticated calls with 401 — proving the
// routes exist (a 404 would mean the pattern never registered).
func TestRegisterAPIRoutes(t *testing.T) {
	s, _ := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux) // must not panic

	if len(apiMethods) != 21 {
		t.Fatalf("expected 21 bound methods, got %d", len(apiMethods))
	}
	for _, m := range apiMethods {
		req := httptest.NewRequest(http.MethodPost, "/api/"+m, strings.NewReader(`{"args":[]}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: no-session status = %d, want 401", m, rec.Code)
		}
	}
}

// TestRPCAuthenticatedNoArg exercises the full handleRPC path with a valid
// session using a no-arg, no-error binding (WritesAllowed → false for a fresh
// user), confirming principal injection and the standard 200 envelope.
func TestRPCAuthenticatedNoArg(t *testing.T) {
	s, sessions := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/WritesAllowed", strings.NewReader(`{"args":[]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "false" {
		t.Errorf("WritesAllowed body = %q, want false", body)
	}
}

// TestRPCUpsertAndListRoundTrip drives an arg-carrying write through HTTP, then
// reads it back: verifies positional arg decoding, the service write path, the
// public-connection projection (id set, password cleared) and per-tenant persist.
func TestRPCUpsertAndListRoundTrip(t *testing.T) {
	s, sessions := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)
	cookie := sessionCookie(t, sessions, "u1", "alice", "user")

	upsert := `{"args":[{"name":"db1","type":"mysql","host":"127.0.0.1","port":3306,"user":"u","password":"secret","database":"d"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/UpsertConnection", strings.NewReader(upsert))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var created config.Connection
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode upsert resp: %v (%s)", err, rec.Body.String())
	}
	if created.ID == "" {
		t.Errorf("created connection has empty id: %+v", created)
	}
	if created.Password != "" {
		t.Errorf("created connection leaked password: %+v", created)
	}

	listReq := httptest.NewRequest(http.MethodPost, "/api/ListConnections", strings.NewReader(`{"args":[]}`))
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	var list []config.Connection
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list resp: %v (%s)", err, listRec.Body.String())
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v, want one connection with id %q", list, created.ID)
	}
	if list[0].Password != "" {
		t.Errorf("listed connection leaked password: %+v", list[0])
	}
}

// TestRPCBadRequest verifies a malformed positional arg yields a 400 (client
// error) rather than a 500, via the badRequestError path in handleRPC.
func TestRPCBadRequest(t *testing.T) {
	s, sessions := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	// DeleteConnection expects args[0] to be a string; send a number.
	req := httptest.NewRequest(http.MethodPost, "/api/DeleteConnection", strings.NewReader(`{"args":[123]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// failStore surfaces an infrastructure-style error (carrying a fake internal DB
// address) from every Load/Save, to prove the RPC layer sanitizes persistence
// faults instead of echoing them to an authenticated tenant.
type failStore struct{}

const fakeInfraDetail = "dial tcp 10.0.0.5:3306: connect: connection refused"

func (failStore) Load(context.Context, string) (*config.AppConfig, error) {
	return nil, errors.New(fakeInfraDetail)
}

func (failStore) Save(context.Context, string, *config.AppConfig) error {
	return errors.New(fakeInfraDetail)
}

// TestRPCSanitizesInternalStoreError proves a config-store (infrastructure) fault
// is logged server-side and returned to the client as a generic message, so it
// cannot leak the operator's internal DB address to a tenant.
func TestRPCSanitizesInternalStoreError(t *testing.T) {
	sessions := auth.NewSessionManager(make([]byte, 32), time.Hour, false)
	s := &server{svc: app.NewService(failStore{}, db.NewManager()), sessions: sessions}
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	// SaveLLMSafety hits ensureCfgLocked (Load) before any validation, so the
	// failStore fault propagates as an internal error.
	req := httptest.NewRequest(http.MethodPost, "/api/SaveLLMSafety", strings.NewReader(`{"args":[false,10,10]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "connection refused") {
		t.Errorf("internal store error leaked infra detail to client: %s", body)
	}
	if !strings.Contains(body, "内部服务错误") {
		t.Errorf("expected sanitized message, got %s", body)
	}
}

// TestRPCBusinessErrorPassesThrough guards the mirror contract: a validation error
// (not an infra fault) must reach the client verbatim so the frontend can surface
// it exactly like a desktop Wails bridge reject.
func TestRPCBusinessErrorPassesThrough(t *testing.T) {
	s, sessions := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	// maxRows=0 fails SaveLLMSafety's own validation with a plain (non-internal) error.
	req := httptest.NewRequest(http.MethodPost, "/api/SaveLLMSafety", strings.NewReader(`{"args":[false,0,0]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "maxRows must be") {
		t.Errorf("business error must pass through verbatim, got %s", rec.Body.String())
	}
}

// TestRPCRejectsOversizedBody verifies the shared body cap: a request larger than
// maxRPCBodyBytes is refused at decode time with a 400, so one tenant cannot
// exhaust the shared server heap with a multi-GB {args:[...]} envelope. The cap
// lives in decodeArgs, which every one of the 21 endpoints funnels through.
func TestRPCRejectsOversizedBody(t *testing.T) {
	s, sessions := newTestServer(t)
	mux := http.NewServeMux()
	s.registerAPIRoutes(mux)

	big := `{"args":["` + strings.Repeat("a", maxRPCBodyBytes+1) + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/WritesAllowed", strings.NewReader(big))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized body status = %d, want 400", rec.Code)
	}
}
