package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func streamMux(s *server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/AgentChatStream", s.sessions.Middleware(http.HandlerFunc(s.handleAgentChatStream)))
	return mux
}

// TestAgentChatStreamRequiresSession proves the streaming endpoint sits behind the
// same session gate as the rest of /api (401, not a stream, when unauthenticated).
func TestAgentChatStreamRequiresSession(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/AgentChatStream", strings.NewReader(`{"args":[{}]}`))
	rec := httptest.NewRecorder()
	streamMux(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no-session status = %d, want 401", rec.Code)
	}
}

// TestAgentChatStreamBadRequest verifies a malformed positional arg is rejected
// with 400 before any SSE headers are committed.
func TestAgentChatStreamBadRequest(t *testing.T) {
	s, sessions := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/AgentChatStream", strings.NewReader(`{"args":[123]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	streamMux(s).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad args status = %d, want 400", rec.Code)
	}
}

// TestAgentChatStreamEmitsErrorFrame exercises the full streaming path up to the
// agent loop: an empty message is rejected inside the service, so the handler —
// having already committed 200 text/event-stream — surfaces it as a terminal SSE
// "error" frame. No LLM or DB is needed because the empty-message guard runs
// before the agent is touched.
func TestAgentChatStreamEmitsErrorFrame(t *testing.T) {
	s, sessions := newTestServer(t)
	s.audit = newAuditLog()
	req := httptest.NewRequest(http.MethodPost, "/api/AgentChatStream",
		strings.NewReader(`{"args":[{"connectionId":"c1","message":""}]}`))
	req.AddCookie(sessionCookie(t, sessions, "u1", "alice", "user"))
	rec := httptest.NewRecorder()
	streamMux(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SSE commits before the loop)", rec.Code)
	}
	if !isEventStream(rec.Header()) {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("no SSE data frame emitted: %q", body)
	}
	if !strings.Contains(body, `"kind":"error"`) || !strings.Contains(body, "message is empty") {
		t.Errorf("expected a terminal error frame, got %q", body)
	}
}
