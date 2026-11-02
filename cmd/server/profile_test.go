package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"AI-DB/internal/auth"
)

func authedProfileServer(t *testing.T) (*server, string) {
	t.Helper()
	users := usersWith(t, "alice", "secret")
	s := newLoginTestServer(t, users)
	// Issue a session cookie for alice.
	u, err := users.ByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	val, err := s.sessions.Issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return s, val
}

func TestGetMe(t *testing.T) {
	s, cookie := authedProfileServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	rec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleGetMe)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["username"] != "alice" {
		t.Fatalf("username=%v", body["username"])
	}
}

func TestUpdateMeDisplayName(t *testing.T) {
	s, cookie := authedProfileServer(t)
	payload := `{"displayName":"Alice Chen"}`
	req := httptest.NewRequest(http.MethodPut, "/api/me", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	rec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleUpdateMe)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	u, err := s.users.ByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.DisplayName != "Alice Chen" {
		t.Fatalf("displayName=%q", u.DisplayName)
	}
}

func TestChangePassword(t *testing.T) {
	s, cookie := authedProfileServer(t)
	payload := `{"currentPassword":"secret","newPassword":"newpass1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	rec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleChangePassword)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	u, err := s.users.ByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(u.PasswordHash, "newpass1") {
		t.Fatal("password not updated")
	}
	if u.TokenVersion < 1 {
		t.Fatalf("token_version=%d, want bumped", u.TokenVersion)
	}

	// Old cookie (ver=0) must fail Gate after the bump.
	oldMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	oldMe.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	oldRec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleGetMe)).ServeHTTP(oldRec, oldMe)
	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("old cookie status %d, want 401; body %s", oldRec.Code, oldRec.Body.String())
	}

	// Response must re-issue a cookie that still works.
	var fresh string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			fresh = c.Value
		}
	}
	if fresh == "" {
		t.Fatal("expected re-issued session cookie after password change")
	}
	newMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	newMe.AddCookie(&http.Cookie{Name: auth.CookieName, Value: fresh})
	newRec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleGetMe)).ServeHTTP(newRec, newMe)
	if newRec.Code != http.StatusOK {
		t.Fatalf("fresh cookie status %d, want 200; body %s", newRec.Code, newRec.Body.String())
	}
}
