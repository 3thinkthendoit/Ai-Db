package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AI-DB/internal/auth"
)

// stubUsers is an in-memory UserRepository for the login handler tests. A non-nil
// err forces ByUsername to fail with that error (to exercise the 500 path).
type stubUsers struct {
	byName map[string]auth.User
	err    error
}

func (s *stubUsers) ByUsername(_ context.Context, username string) (*auth.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	if u, ok := s.byName[username]; ok {
		return &u, nil
	}
	return nil, auth.ErrNoUser
}

func (s *stubUsers) ByID(_ context.Context, id string) (*auth.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, u := range s.byName {
		if u.ID == id {
			cp := u
			return &cp, nil
		}
	}
	return nil, auth.ErrNoUser
}

func (s *stubUsers) ByIDAuth(ctx context.Context, id string) (*auth.AuthAccount, error) {
	u, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &auth.AuthAccount{
		ID: u.ID, Username: u.Username, Role: u.Role,
		Banned: u.Banned, TokenVersion: u.TokenVersion,
	}, nil
}

func (s *stubUsers) Create(_ context.Context, u auth.User) error {
	if s.byName == nil {
		s.byName = map[string]auth.User{}
	}
	s.byName[u.Username] = u
	return nil
}

func (s *stubUsers) UpdateProfile(_ context.Context, id, displayName, avatar string) error {
	for name, u := range s.byName {
		if u.ID == id {
			u.DisplayName = displayName
			u.Avatar = avatar
			s.byName[name] = u
			return nil
		}
	}
	return auth.ErrNoUser
}

func (s *stubUsers) UpdatePassword(_ context.Context, id, passwordHash string) (int, error) {
	for name, u := range s.byName {
		if u.ID == id {
			u.PasswordHash = passwordHash
			u.TokenVersion++
			s.byName[name] = u
			return u.TokenVersion, nil
		}
	}
	return 0, auth.ErrNoUser
}

func (s *stubUsers) UpdateAdmin(_ context.Context, id, role, displayName string, banned bool) error {
	for name, u := range s.byName {
		if u.ID == id {
			losingAdmin := u.Role == auth.RoleAdmin && !u.Banned && (role != auth.RoleAdmin || banned)
			if losingAdmin {
				n := 0
				for _, o := range s.byName {
					if o.Role == auth.RoleAdmin && !o.Banned {
						n++
					}
				}
				if n <= 1 {
					return auth.ErrLastAdmin
				}
			}
			u.Role = role
			u.DisplayName = displayName
			u.Banned = banned
			s.byName[name] = u
			return nil
		}
	}
	return auth.ErrNoUser
}

func (s *stubUsers) CountAdmins(context.Context) (int, error) {
	n := 0
	for _, u := range s.byName {
		if u.Role == auth.RoleAdmin && !u.Banned {
			n++
		}
	}
	return n, nil
}

func (s *stubUsers) Count(context.Context) (int, error) { return len(s.byName), nil }
func (s *stubUsers) List(context.Context) ([]auth.User, error) {
	out := make([]auth.User, 0, len(s.byName))
	for _, u := range s.byName {
		out = append(out, u)
	}
	return out, nil
}

var _ auth.UserRepository = (*stubUsers)(nil)

func newLoginTestServer(t *testing.T, users auth.UserRepository) *server {
	t.Helper()
	dummy, err := auth.HashPassword("dummy-hash-that-never-matches")
	if err != nil {
		t.Fatalf("dummy hash: %v", err)
	}
	return &server{
		users:     users,
		sessions:  auth.NewSessionManager(make([]byte, 32), time.Hour, false),
		dummyHash: dummy,
	}
}

func doLogin(t *testing.T, s *server, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	return rec
}

func usersWith(t *testing.T, name, password string) *stubUsers {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return &stubUsers{byName: map[string]auth.User{
		name: {ID: "u-" + name, Username: name, PasswordHash: hash, Role: auth.RoleUser},
	}}
}

// TestLoginUnknownUser401NoPanic is the timing-oracle regression guard: an
// unknown user must run the dummy bcrypt comparison (so it does not panic on a
// nil user) and answer 401 exactly like a wrong password.
func TestLoginUnknownUser401NoPanic(t *testing.T) {
	s := newLoginTestServer(t, &stubUsers{byName: map[string]auth.User{}})
	rec := doLogin(t, s, "ghost", "whatever")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: got %d, want 401", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if out["ok"] != false {
		t.Fatalf("expected ok=false, got %v", out["ok"])
	}
}

func TestLoginWrongPassword401(t *testing.T) {
	s := newLoginTestServer(t, usersWith(t, "alice", "right"))
	rec := doLogin(t, s, "alice", "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d, want 401", rec.Code)
	}
}

func TestLoginSuccess200SetsCookie(t *testing.T) {
	s := newLoginTestServer(t, usersWith(t, "alice", "right"))
	rec := doLogin(t, s, "alice", "right")
	if rec.Code != http.StatusOK {
		t.Fatalf("success: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected a session cookie on success")
	}
}

// TestLoginStoreError500 verifies a genuine repository failure (not ErrNoUser)
// surfaces as 500 rather than being masked as a 401 credential failure.
func TestLoginStoreError500(t *testing.T) {
	s := newLoginTestServer(t, &stubUsers{err: errors.New("db down")})
	rec := doLogin(t, s, "alice", "x")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: got %d, want 500", rec.Code)
	}
}

func TestLoginMalformedBody400(t *testing.T) {
	s := newLoginTestServer(t, &stubUsers{byName: map[string]auth.User{}})
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: got %d, want 400", rec.Code)
	}
}
