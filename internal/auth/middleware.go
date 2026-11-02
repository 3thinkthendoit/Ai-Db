package auth

import (
	"context"
	"errors"
	"net/http"

	"AI-DB/internal/app"
)

type ctxKey int

const principalKey ctxKey = iota

// Middleware enforces a valid session cookie and injects the caller's
// app.Principal into the request context. Requests without a valid session get
// 401 before reaching the handler.
//
// Prefer Gate when a UserRepository is available: Middleware alone trusts the
// cookie's role/ban snapshot and will not revoke access after an admin bans or
// demotes the account.
func (m *SessionManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := m.FromRequest(r)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		p := app.Principal{UserID: sess.UserID, Username: sess.Username, Role: sess.Role}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// Gate verifies the session cookie, then reloads a lean auth row so ban, role,
// and token_version (password change) take effect immediately without loading
// avatar blobs. Banned/deleted/revoked accounts get a cleared cookie and 401/403.
func (m *SessionManager) Gate(users UserRepository, next http.Handler) http.Handler {
	return m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeUnauthorized(w)
			return
		}
		if users == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"auth error"}`))
			return
		}
		sess, err := m.FromRequest(r)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		u, err := users.ByIDAuth(r.Context(), p.UserID)
		if errors.Is(err, ErrNoUser) {
			http.SetCookie(w, m.ClearCookie())
			writeUnauthorized(w)
			return
		}
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"auth error"}`))
			return
		}
		if u.Banned {
			http.SetCookie(w, m.ClearCookie())
			writeBanned(w)
			return
		}
		if sess.TokenVersion != u.TokenVersion {
			http.SetCookie(w, m.ClearCookie())
			writeUnauthorized(w)
			return
		}
		live := app.Principal{UserID: u.ID, Username: u.Username, Role: u.Role}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, live)))
	}))
}

// PrincipalFromContext extracts the authenticated principal injected by
// Middleware. The boolean is false when the context carries no principal.
func PrincipalFromContext(ctx context.Context) (app.Principal, bool) {
	p, ok := ctx.Value(principalKey).(app.Principal)
	return p, ok
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeBanned(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"账号已被封禁"}`))
}

// Role values stored on users.role and carried in app.Principal.Role.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// RequireAdmin rejects any principal that is not an admin with 403. It must wrap
// a handler that runs AFTER Middleware/Gate (which injects the principal), e.g.
//
//	sessions.Gate(users, auth.RequireAdmin(http.HandlerFunc(handler)))
//
// This is the single authorisation choke point for admin-only endpoints (user
// directory, account creation). There is no open registration, so every account
// after the env-bootstrapped first admin is minted through a RequireAdmin route.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok || p.Role != RoleAdmin {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden: admin only"}`))
}
