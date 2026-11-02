package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"AI-DB/internal/auth"
)

func adminFixture(t *testing.T) (*server, *stubUsers, string, string) {
	t.Helper()
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	users := &stubUsers{byName: map[string]auth.User{
		"admin": {ID: "u-admin", Username: "admin", PasswordHash: hash, Role: auth.RoleAdmin},
		"bob":   {ID: "u-bob", Username: "bob", PasswordHash: hash, Role: auth.RoleUser},
	}}
	s := newLoginTestServer(t, users)
	adminCookie, err := s.sessions.Issue("u-admin", "admin", auth.RoleAdmin, 0)
	if err != nil {
		t.Fatalf("issue admin: %v", err)
	}
	bobCookie, err := s.sessions.Issue("u-bob", "bob", auth.RoleUser, 0)
	if err != nil {
		t.Fatalf("issue bob: %v", err)
	}
	return s, users, adminCookie, bobCookie
}

func TestAdminBanBlocksLiveSession(t *testing.T) {
	s, users, adminCookie, bobCookie := adminFixture(t)

	// Ban bob via admin API.
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/u-bob", bytes.NewBufferString(`{"banned":true}`))
	req.SetPathValue("id", "u-bob")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminCookie})
	rec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminUpdateUser))).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ban status %d body %s", rec.Code, rec.Body.String())
	}
	u, _ := users.ByID(t.Context(), "u-bob")
	if !u.Banned {
		t.Fatal("bob should be banned in store")
	}

	// Bob's existing cookie must be rejected by Gate.
	me := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	me.AddCookie(&http.Cookie{Name: auth.CookieName, Value: bobCookie})
	meRec := httptest.NewRecorder()
	s.gate(http.HandlerFunc(s.handleGetMe)).ServeHTTP(meRec, me)
	if meRec.Code != http.StatusForbidden {
		t.Fatalf("banned session status %d, want 403; body %s", meRec.Code, meRec.Body.String())
	}
}

func TestAdminCannotBanLastAdmin(t *testing.T) {
	s, _, adminCookie, _ := adminFixture(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/u-admin", bytes.NewBufferString(`{"banned":true}`))
	req.SetPathValue("id", "u-admin")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminCookie})
	rec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminUpdateUser))).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDemotionTakesEffectOnNextRequest(t *testing.T) {
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	users := &stubUsers{byName: map[string]auth.User{
		"admin": {ID: "u-admin", Username: "admin", PasswordHash: hash, Role: auth.RoleAdmin},
		"aide":  {ID: "u-aide", Username: "aide", PasswordHash: hash, Role: auth.RoleAdmin},
	}}
	s := newLoginTestServer(t, users)
	adminCookie, _ := s.sessions.Issue("u-admin", "admin", auth.RoleAdmin, 0)
	aideCookie, _ := s.sessions.Issue("u-aide", "aide", auth.RoleAdmin, 0)

	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/u-aide", bytes.NewBufferString(`{"role":"user"}`))
	req.SetPathValue("id", "u-aide")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminCookie})
	rec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminUpdateUser))).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote status %d body %s", rec.Code, rec.Body.String())
	}

	// Cookie still says admin, but Gate reloads role → RequireAdmin must 403.
	list := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	list.AddCookie(&http.Cookie{Name: auth.CookieName, Value: aideCookie})
	listRec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminListUsers))).ServeHTTP(listRec, list)
	if listRec.Code != http.StatusForbidden {
		t.Fatalf("demoted admin list status %d, want 403", listRec.Code)
	}
}

func TestAdminSetPassword(t *testing.T) {
	s, users, adminCookie, _ := adminFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/u-bob/password",
		bytes.NewBufferString(`{"password":"newpass1"}`))
	req.SetPathValue("id", "u-bob")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminCookie})
	rec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminSetPassword))).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	u, err := users.ByID(t.Context(), "u-bob")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(u.PasswordHash, "newpass1") {
		t.Fatal("password not updated")
	}
}

func TestAdminSetPasswordTooLong(t *testing.T) {
	s, _, adminCookie, _ := adminFixture(t)
	long := string(bytes.Repeat([]byte("a"), 73))
	body, _ := json.Marshal(map[string]string{"password": long})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/u-bob/password", bytes.NewReader(body))
	req.SetPathValue("id", "u-bob")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: adminCookie})
	rec := httptest.NewRecorder()
	s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminSetPassword))).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body %s", rec.Code, rec.Body.String())
	}
}
