package auth

import (
	"context"
	"net/http"

	"AI-DB/internal/app"
)

type ctxKey int

const principalKey ctxKey = iota

// Middleware enforces a valid session cookie and injects the caller's
// app.Principal into the request context. Requests without a valid session get
// 401 before reaching the handler.
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

// Role values stored on users.role and carried in app.Principal.Role.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// RequireAdmin rejects any principal that is not an admin with 403. It must wrap
// a handler that runs AFTER Middleware (which injects the principal), e.g.
//
//	sessions.Middleware(auth.RequireAdmin(http.HandlerFunc(handler)))
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
