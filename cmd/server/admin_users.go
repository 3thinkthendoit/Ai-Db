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

// handleAdminUpdateUser patches role / displayName / email / phone / banned.
// Guards: cannot ban or demote yourself when that would leave zero active admins.
func (s *server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing user id"})
		return
	}
	var body struct {
		Role        *string `json:"role"`
		DisplayName *string `json:"displayName"`
		Email       *string `json:"email"`
		Phone       *string `json:"phone"`
		Banned      *bool   `json:"banned"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}

	u, err := s.users.ByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, auth.ErrNoUser) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "用户不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "查询用户失败"})
		return
	}

	role := u.Role
	displayName := u.DisplayName
	email := u.Email
	phone := u.Phone
	banned := u.Banned
	// DBA may manage user/dba accounts but must not touch admin accounts.
	if actor.Role == auth.RoleDBA && u.Role == auth.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "dba cannot modify admin accounts"})
		return
	}
	if body.Role != nil {
		role = strings.TrimSpace(*body.Role)
		if !auth.ValidAccountRole(role) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "role must be admin, dba, or user"})
			return
		}
		// DBA must not promote anyone (including self) to admin.
		if actor.Role == auth.RoleDBA && role == auth.RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "dba cannot assign admin role"})
			return
		}
	}
	if body.DisplayName != nil {
		displayName = strings.TrimSpace(*body.DisplayName)
		if utf8.RuneCountInString(displayName) > 64 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "名称过长（最多 64 字）"})
			return
		}
	}
	if body.Email != nil {
		email = strings.TrimSpace(*body.Email)
		if msg := validateEmail(email); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
			return
		}
	}
	if body.Phone != nil {
		phone = strings.TrimSpace(*body.Phone)
		if msg := validatePhone(phone); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
			return
		}
	}
	if body.Banned != nil {
		banned = *body.Banned
	}

	if id == actor.UserID && banned {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "不能封禁自己"})
		return
	}

	if err := s.users.UpdateAdmin(r.Context(), id, role, displayName, email, phone, banned); err != nil {
		if errors.Is(err, auth.ErrLastAdmin) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "不能封禁或降级唯一管理员"})
			return
		}
		log.Printf("admin update user %s: %v", id, err)
		s.audit.adminUpdateUser(r, actor.Username, u.Username, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存失败"})
		return
	}
	s.audit.adminUpdateUser(r, actor.Username, u.Username, "success", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"id":          u.ID,
		"username":    u.Username,
		"role":        role,
		"displayName": displayName,
		"email":       email,
		"phone":       phone,
		"banned":      banned,
	})
}

// handleAdminSetPassword sets a new password for a user (admin reset; no current password).
func (s *server) handleAdminSetPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing user id"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}
	if msg := validatePassword(body.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
		return
	}
	u, err := s.users.ByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, auth.ErrNoUser) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "用户不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "查询用户失败"})
		return
	}
	if actor.Role == auth.RoleDBA && u.Role == auth.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "dba cannot reset admin passwords"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "hash error"})
		return
	}
	if _, err := s.users.UpdatePassword(r.Context(), id, hash); err != nil {
		log.Printf("admin set password %s: %v", id, err)
		s.audit.adminSetPassword(r, actor.Username, u.Username, "error")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "修改密码失败"})
		return
	}
	// token_version bump invalidates the target's existing cookies on next Gate.
	s.audit.adminSetPassword(r, actor.Username, u.Username, "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
