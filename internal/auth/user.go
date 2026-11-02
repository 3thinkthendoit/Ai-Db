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

// ErrLastAdmin is returned when an update would leave zero active admins.
var ErrLastAdmin = errors.New("cannot demote or ban the last admin")

// User is an authenticated account.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	DisplayName  string
	Avatar       string // data-URL or empty; never returned from List admin views unless needed
	Email        string
	Phone        string
	Banned       bool
	TokenVersion int // bumped on password change; must match session cookie
	CreatedAt    time.Time
	LastLoginAt  time.Time // zero when the user has never logged in
}

// AuthAccount is the lean row Gate/session checks need (no avatar / password hash).
type AuthAccount struct {
	ID           string
	Username     string
	Role         string
	Banned       bool
	TokenVersion int
}

// UserRepository loads and creates accounts. Backed by MySQL on the server;
// the desktop build has no user store.
type UserRepository interface {
	ByUsername(ctx context.Context, username string) (*User, error)
	ByID(ctx context.Context, id string) (*User, error)
	ByIDAuth(ctx context.Context, id string) (*AuthAccount, error)
	Create(ctx context.Context, u User) error
	UpdateProfile(ctx context.Context, id, displayName, avatar string) error
	UpdatePassword(ctx context.Context, id, passwordHash string) (tokenVersion int, err error)
	UpdateAdmin(ctx context.Context, id, role, displayName, email, phone string, banned bool) error
	TouchLastLogin(ctx context.Context, id string) error
	CountAdmins(ctx context.Context) (int, error)
	Count(ctx context.Context) (int, error)
	List(ctx context.Context) ([]User, error)
}

// MySQLUsers is the MySQL-backed UserRepository.
type MySQLUsers struct{ db *sql.DB }

// NewMySQLUsers wraps an open pool.
func NewMySQLUsers(db *sql.DB) *MySQLUsers { return &MySQLUsers{db: db} }

func scanUser(row interface {
	Scan(dest ...any) error
}) (*User, error) {
	var u User
	var avatar sql.NullString
	var banned int
	var lastLogin sql.NullTime
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &avatar, &u.Email, &u.Phone, &banned, &u.TokenVersion, &u.CreatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoUser
	}
	if err != nil {
		return nil, err
	}
	if avatar.Valid {
		u.Avatar = avatar.String
	}
	u.Banned = banned != 0
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	return &u, nil
}

func scanAuthAccount(row interface {
	Scan(dest ...any) error
}) (*AuthAccount, error) {
	var a AuthAccount
	var banned int
	err := row.Scan(&a.ID, &a.Username, &a.Role, &banned, &a.TokenVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoUser
	}
	if err != nil {
		return nil, err
	}
	a.Banned = banned != 0
	return &a, nil
}

const userSelectCols = "id, username, password_hash, role, display_name, avatar, email, phone, banned, token_version, created_at, last_login_at"
const authSelectCols = "id, username, role, banned, token_version"

// ByUsername loads a user by their unique username.
func (r *MySQLUsers) ByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		"SELECT "+userSelectCols+" FROM users WHERE username = ?", username))
}

// ByID loads a user by primary key (includes avatar; for profile paths).
func (r *MySQLUsers) ByID(ctx context.Context, id string) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		"SELECT "+userSelectCols+" FROM users WHERE id = ?", id))
}

// ByIDAuth loads the lean auth row for Gate / session checks (no avatar).
func (r *MySQLUsers) ByIDAuth(ctx context.Context, id string) (*AuthAccount, error) {
	return scanAuthAccount(r.db.QueryRowContext(ctx,
		"SELECT "+authSelectCols+" FROM users WHERE id = ?", id))
}

// Create inserts a new user.
func (r *MySQLUsers) Create(ctx context.Context, u User) error {
	now := time.Now().UTC()
	banned := 0
	if u.Banned {
		banned = 1
	}
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO users (id, username, password_hash, role, display_name, avatar, email, phone, banned, token_version, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, nullIfEmpty(u.Avatar), u.Email, u.Phone, banned, u.TokenVersion, now, now)
	return err
}

// UpdateProfile sets display name and avatar for the given user id.
// MySQL reports RowsAffected=0 when values are unchanged, so a zero count is
// treated as success when the row still exists.
func (r *MySQLUsers) UpdateProfile(ctx context.Context, id, displayName, avatar string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE users SET display_name = ?, avatar = ?, updated_at = ? WHERE id = ?",
		displayName, nullIfEmpty(avatar), time.Now().UTC(), id)
	if err != nil {
		return err
	}
	return r.ensureExists(ctx, id)
}

// UpdatePassword replaces the bcrypt hash and bumps token_version so existing
// session cookies stop passing Gate.
func (r *MySQLUsers) UpdatePassword(ctx context.Context, id, passwordHash string) (int, error) {
	_, err := r.db.ExecContext(ctx,
		"UPDATE users SET password_hash = ?, token_version = token_version + 1, updated_at = ? WHERE id = ?",
		passwordHash, time.Now().UTC(), id)
	if err != nil {
		return 0, err
	}
	if err := r.ensureExists(ctx, id); err != nil {
		return 0, err
	}
	var ver int
	err = r.db.QueryRowContext(ctx, "SELECT token_version FROM users WHERE id = ?", id).Scan(&ver)
	return ver, err
}

// UpdateAdmin sets role, display name, contact fields and ban flag. When the
// change would remove the last active admin, it returns ErrLastAdmin. The check
// runs under a transaction with FOR UPDATE so concurrent demotions cannot race
// to zero admins.
func (r *MySQLUsers) UpdateAdmin(ctx context.Context, id, role, displayName, email, phone string, banned bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var curRole string
	var curBanned int
	err = tx.QueryRowContext(ctx,
		"SELECT role, banned FROM users WHERE id = ? FOR UPDATE", id).Scan(&curRole, &curBanned)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoUser
	}
	if err != nil {
		return err
	}

	losingAdmin := curRole == RoleAdmin && curBanned == 0 && (role != RoleAdmin || banned)
	if losingAdmin {
		rows, err := tx.QueryContext(ctx,
			"SELECT id FROM users WHERE role = ? AND banned = 0 FOR UPDATE", RoleAdmin)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			var ignore string
			if err := rows.Scan(&ignore); err != nil {
				rows.Close()
				return err
			}
			n++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}

	flag := 0
	if banned {
		flag = 1
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE users SET role = ?, display_name = ?, email = ?, phone = ?, banned = ?, updated_at = ? WHERE id = ?",
		role, displayName, email, phone, flag, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Unchanged values still count as success when the row exists.
		var one int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id = ?", id).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoUser
			}
			return err
		}
	}
	return tx.Commit()
}

func (r *MySQLUsers) ensureExists(ctx context.Context, id string) error {
	var one int
	err := r.db.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id = ?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoUser
	}
	return err
}

// TouchLastLogin stamps last_login_at to now (best-effort after successful login).
func (r *MySQLUsers) TouchLastLogin(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?",
		time.Now().UTC(), time.Now().UTC(), id)
	return err
}

// CountAdmins returns non-banned admin accounts (used to prevent locking out).
func (r *MySQLUsers) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE role = ? AND banned = 0", RoleAdmin).Scan(&n)
	return n, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
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
		"SELECT "+userSelectCols+" FROM users ORDER BY created_at, username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}
