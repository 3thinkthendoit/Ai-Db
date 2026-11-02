package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"AI-DB/internal/auth"
)

// maxProfileBodyBytes caps profile/password bodies. Avatar data-URLs dominate;
// 400 KiB leaves headroom under the 256 KiB decoded payload limit.
const maxProfileBodyBytes = 400 << 10

// maxAvatarChars caps stored avatar data-URL length (~190 KiB image as base64).
const maxAvatarChars = 280_000

const minPasswordRunes = 6

// maxPasswordBytes matches bcrypt's input limit; longer secrets are silently
// truncated by the library, so we refuse them instead.
const maxPasswordBytes = 72

func validatePassword(pw string) string {
	if utf8.RuneCountInString(pw) < minPasswordRunes {
		return "密码至少 6 位"
	}
	if len(pw) > maxPasswordBytes {
		return "密码过长（最多 72 字节）"
	}
	return ""
}

func validateEmail(email string) string {
	if email == "" {
		return ""
	}
	if utf8.RuneCountInString(email) > 128 {
		return "邮箱过长（最多 128 字）"
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.Contains(email[at+1:], "@") {
		return "邮箱格式不正确"
	}
	return ""
}

func validatePhone(phone string) string {
	if phone == "" {
		return ""
	}
	if utf8.RuneCountInString(phone) > 32 {
		return "手机号过长（最多 32 字）"
	}
	for _, r := range phone {
		switch {
		case r >= '0' && r <= '9':
		case r == '+' || r == '-' || r == ' ' || r == '(' || r == ')':
		default:
			return "手机号格式不正确"
		}
	}
	return ""
}

func (s *server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	u, err := s.users.ByID(r.Context(), p.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrNoUser) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		log.Printf("get me: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "加载个人信息失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":    u.Username,
		"role":        u.Role,
		"displayName": u.DisplayName,
		"avatar":      u.Avatar,
	})
}

func (s *server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var body struct {
		DisplayName *string `json:"displayName"`
		Avatar      *string `json:"avatar"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}
	u, err := s.users.ByID(r.Context(), p.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrNoUser) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "加载个人信息失败"})
		return
	}
	displayName := u.DisplayName
	avatar := u.Avatar
	if body.DisplayName != nil {
		displayName = strings.TrimSpace(*body.DisplayName)
		if utf8.RuneCountInString(displayName) > 64 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "名称过长（最多 64 字）"})
			return
		}
	}
	if body.Avatar != nil {
		avatar = strings.TrimSpace(*body.Avatar)
		if avatar != "" {
			if len(avatar) > maxAvatarChars {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "头像过大"})
				return
			}
			if !validAvatarDataURL(avatar) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "头像格式无效（仅支持 PNG / JPEG）"})
				return
			}
		}
	}
	if err := s.users.UpdateProfile(r.Context(), p.UserID, displayName, avatar); err != nil {
		log.Printf("update me: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"username":    u.Username,
		"role":        u.Role,
		"displayName": displayName,
		"avatar":      avatar,
	})
}

func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}
	if body.CurrentPassword == "" || body.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请填写当前密码和新密码"})
		return
	}
	if msg := validatePassword(body.NewPassword); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
		return
	}
	u, err := s.users.ByID(r.Context(), p.UserID)
	if err != nil {
		if errors.Is(err, auth.ErrNoUser) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "加载账号失败"})
		return
	}
	if !auth.VerifyPassword(u.PasswordHash, body.CurrentPassword) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "当前密码不正确"})
		return
	}
	hash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "hash error"})
		return
	}
	ver, err := s.users.UpdatePassword(r.Context(), p.UserID, hash)
	if err != nil {
		log.Printf("change password: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "修改密码失败"})
		return
	}
	// Bumped token_version invalidates other devices; re-issue so this browser stays signed in.
	value, err := s.sessions.Issue(p.UserID, u.Username, u.Role, ver)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "session error"})
		return
	}
	http.SetCookie(w, s.sessions.Cookie(value))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validAvatarDataURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "data:image/png;base64,") ||
		strings.HasPrefix(lower, "data:image/jpeg;base64,") ||
		strings.HasPrefix(lower, "data:image/jpg;base64,")
}
