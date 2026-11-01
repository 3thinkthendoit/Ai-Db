package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// CookieName is the session cookie key.
const CookieName = "aidb_session"

// Session is the authenticated identity carried by the cookie. Phase 0 uses a
// stateless HMAC-signed cookie (no sessions table); Phase 2 can add server-side
// sessions for revocation without changing this shape.
type Session struct {
	UserID    string    `json:"uid"`
	Username  string    `json:"user"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"exp"`
}

// SessionManager issues and verifies signed session cookies.
type SessionManager struct {
	secret []byte
	ttl    time.Duration
	secure bool // Secure flag; set true behind HTTPS in production
}

// NewSessionManager builds a manager. secret must be a high-entropy key (>=32B).
func NewSessionManager(secret []byte, ttl time.Duration, secure bool) *SessionManager {
	return &SessionManager{secret: secret, ttl: ttl, secure: secure}
}

// TTL reports the configured session lifetime (used to align cookie expiry).
func (m *SessionManager) TTL() time.Duration { return m.ttl }

// Issue signs a fresh session for the user and returns the cookie value.
func (m *SessionManager) Issue(userID, username, role string) (string, error) {
	s := Session{
		UserID:    userID,
		Username:  username,
		Role:      role,
		ExpiresAt: time.Now().Add(m.ttl).UTC(),
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := base64.RawURLEncoding.EncodeToString(m.sign([]byte(body)))
	return body + "." + sig, nil
}

// Verify validates the signature and expiry of a cookie value.
func (m *SessionManager) Verify(value string) (*Session, error) {
	body, sig, ok := strings.Cut(value, ".")
	if !ok {
		return nil, errors.New("malformed session")
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, errors.New("malformed session signature")
	}
	if !hmac.Equal(m.sign([]byte(body)), got) {
		return nil, errors.New("bad session signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("malformed session payload")
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if time.Now().UTC().After(s.ExpiresAt) {
		return nil, errors.New("session expired")
	}
	return &s, nil
}

// FromRequest reads and verifies the session cookie on a request.
func (m *SessionManager) FromRequest(r *http.Request) (*Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil, err
	}
	return m.Verify(c.Value)
}

// Cookie builds the HttpOnly session cookie for a signed value.
func (m *SessionManager) Cookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(m.ttl).UTC(),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// ClearCookie returns an expired cookie that instructs the browser to drop the
// session (stateless logout).
func (m *SessionManager) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (m *SessionManager) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(data)
	return mac.Sum(nil)
}
