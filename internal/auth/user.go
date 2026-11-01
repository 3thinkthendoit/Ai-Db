package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNoUser is returned when a username does not exist. Callers should treat it
// identically to a wrong password so login does not leak which usernames exist.
var ErrNoUser = errors.New("user not found")

// User is an authenticated account.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
}

// UserRepository loads and creates accounts. Backed by MySQL on the server;
// the desktop build has no user store.
type UserRepository interface {
	ByUsername(ctx context.Context, username string) (*User, error)
	Create(ctx context.Context, u User) error
	Count(ctx context.Context) (int, error)
	List(ctx context.Context) ([]User, error)
}

// MySQLUsers is the MySQL-backed UserRepository.
type MySQLUsers struct{ db *sql.DB }

// NewMySQLUsers wraps an open pool.
func NewMySQLUsers(db *sql.DB) *MySQLUsers { return &MySQLUsers{db: db} }

// ByUsername loads a user by their unique username.
func (r *MySQLUsers) ByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := r.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, role FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoUser
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create inserts a new user.
func (r *MySQLUsers) Create(ctx context.Context, u User) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (?,?,?,?,?,?)",
		u.ID, u.Username, u.PasswordHash, u.Role, now, now)
	return err
}

// Count returns the total number of users (used to decide whether to seed).
func (r *MySQLUsers) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// List returns every account ordered by creation. It is used by the admin-only
// user directory; the PasswordHash field is populated but callers MUST NOT
// serialise it to a client (see the admin list handler, which projects to a
// safe view).
func (r *MySQLUsers) List(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, username, password_hash, role FROM users ORDER BY created_at, username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
