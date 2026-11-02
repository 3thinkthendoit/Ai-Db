package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"AI-DB/internal/config"

	"github.com/go-sql-driver/mysql"
)

// schemaSQL is the server DDL, applied idempotently by Migrate on startup.
//
//go:embed schema.sql
var schemaSQL string

// MySQLStore is the multi-user ConfigStore backed by MySQL. Every statement is
// scoped by owner_id so tenants are isolated at the storage layer (defence in
// depth with the app.Service's per-user in-memory partitioning).
//
// The whole AppConfig document is persisted across three tables: connections,
// llm_profiles (API keys encrypted at rest) and user_settings (active profile id
// + safety knobs). Load reassembles them; Save rewrites all three inside one
// transaction so a partial failure never leaves a half-updated document.
type MySQLStore struct {
	db     *sql.DB
	cipher *Cipher
}

// NewMySQLStore wraps an open pool with the secret cipher used for at-rest
// encryption of connection passwords and LLM API keys.
func NewMySQLStore(db *sql.DB, cipher *Cipher) *MySQLStore {
	return &MySQLStore{db: db, cipher: cipher}
}

// DB exposes the underlying pool for auth/session wiring and bootstrap seeding.
func (s *MySQLStore) DB() *sql.DB { return s.db }

// Migrate applies schema.sql (CREATE TABLE IF NOT EXISTS …) so a fresh server
// bootstraps its own tables. Statements are split on ";" after dropping
// full-line "--" comments. Then additive ALTERs backfill columns on older DBs
// that already have users without display_name/avatar.
func (s *MySQLStore) Migrate(ctx context.Context) error {
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := s.ensureUserProfileColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureMutationActiveSchema(ctx); err != nil {
		return err
	}
	return nil
}

// ensureMutationActiveSchema backfills active_schema on older mutation_requests tables.
func (s *MySQLStore) ensureMutationActiveSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"ALTER TABLE mutation_requests ADD COLUMN active_schema VARCHAR(128) NOT NULL DEFAULT ''")
	if err != nil {
		if isDuplicateColumn(err) {
			return nil
		}
		// Table may not exist yet on brand-new migrate order; ignore missing table.
		if strings.Contains(err.Error(), "doesn't exist") {
			return nil
		}
		return fmt.Errorf("migrate mutation active_schema: %w", err)
	}
	return nil
}

// ensureUserProfileColumns adds display_name / avatar when missing (idempotent).
// CREATE TABLE IF NOT EXISTS does not alter existing tables, so upgrades need this.
func (s *MySQLStore) ensureUserProfileColumns(ctx context.Context) error {
	alters := []string{
		"ALTER TABLE users ADD COLUMN display_name VARCHAR(64) NOT NULL DEFAULT ''",
		"ALTER TABLE users ADD COLUMN avatar MEDIUMTEXT NULL",
		"ALTER TABLE users ADD COLUMN banned TINYINT(1) NOT NULL DEFAULT 0",
		"ALTER TABLE users ADD COLUMN token_version INT NOT NULL DEFAULT 0",
	}
	for _, stmt := range alters {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("migrate profile columns: %w", err)
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1060 // ER_DUP_FIELDNAME
	}
	// Fallback for drivers that wrap the error string only.
	return strings.Contains(err.Error(), "Duplicate column name")
}

// Load assembles the user's config document from the owner-scoped rows across
// connections, llm_profiles and user_settings. Secrets are decrypted into the
// in-memory document (matching the desktop FileStore); the app.Service re-masks
// them via config.PublicConnection / config.ProfileViews before they reach a
// client. A brand-new user (no rows) yields normalised defaults.
func (s *MySQLStore) Load(ctx context.Context, userID string) (*config.AppConfig, error) {
	conns, err := s.loadConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.loadLLMProfiles(ctx, userID)
	if err != nil {
		return nil, err
	}
	cfg := &config.AppConfig{
		Connections: conns,
		LLMProfiles: profiles,
		LLM:         config.DefaultLLM(),
	}
	// Overlay persisted per-user settings when a row exists; otherwise the
	// DefaultLLM knobs and NormalizeLLMProfiles' active-id fallback stand.
	st, ok, err := s.loadSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	if ok {
		cfg.ActiveLLMID = st.activeLLMID
		cfg.LLM.AllowWrites = st.allowWrites
		cfg.LLM.MaxRows = st.maxRows
		cfg.LLM.MaxAgentSteps = st.maxAgentSteps
	}
	config.NormalizeLLMProfiles(cfg)
	return cfg, nil
}

func (s *MySQLStore) loadConnections(ctx context.Context, userID string) ([]config.Connection, error) {
	const q = "SELECT id, name, `type`, host, port, `user`, password, `database`, auth_source, ssl_mode " +
		"FROM connections WHERE owner_id = ? ORDER BY created_at, id"
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conns := make([]config.Connection, 0)
	for rows.Next() {
		var (
			c           config.Connection
			connType    string
			encPassword string
		)
		if err := rows.Scan(&c.ID, &c.Name, &connType, &c.Host, &c.Port, &c.User,
			&encPassword, &c.Database, &c.AuthSource, &c.SSLMode); err != nil {
			return nil, err
		}
		c.Type = config.ConnType(connType)
		pwd, err := s.cipher.Decrypt(encPassword)
		if err != nil {
			return nil, fmt.Errorf("connection %s: %w", c.ID, err)
		}
		c.Password = pwd
		conns = append(conns, c)
	}
	return conns, rows.Err()
}

func (s *MySQLStore) loadLLMProfiles(ctx context.Context, userID string) ([]config.LLMProfile, error) {
	const q = "SELECT id, name, base_url, model, api_key " +
		"FROM llm_profiles WHERE owner_id = ? ORDER BY created_at, id"
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := make([]config.LLMProfile, 0)
	for rows.Next() {
		var (
			p      config.LLMProfile
			encKey string
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &encKey); err != nil {
			return nil, err
		}
		key, err := s.cipher.Decrypt(encKey)
		if err != nil {
			return nil, fmt.Errorf("llm profile %s: %w", p.ID, err)
		}
		p.APIKey = key
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// userSettingsRow mirrors the user_settings table (active id + safety knobs).
type userSettingsRow struct {
	activeLLMID   string
	allowWrites   bool
	maxRows       int
	maxAgentSteps int
}

func (s *MySQLStore) loadSettings(ctx context.Context, userID string) (userSettingsRow, bool, error) {
	const q = "SELECT active_llm_id, allow_writes, max_rows, max_agent_steps FROM user_settings WHERE owner_id = ?"
	var st userSettingsRow
	err := s.db.QueryRowContext(ctx, q, userID).
		Scan(&st.activeLLMID, &st.allowWrites, &st.maxRows, &st.maxAgentSteps)
	if errors.Is(err, sql.ErrNoRows) {
		return userSettingsRow{}, false, nil
	}
	if err != nil {
		return userSettingsRow{}, false, err
	}
	return st, true, nil
}

// Save rewrites the user's whole document to match cfg. All three tables are
// rewritten inside one transaction so a failure rolls back to the prior state.
// Each connections/llm_profiles row carries a per-index microsecond created_at so
// the next Load's ORDER BY created_at reproduces the saved slice order. Secrets
// are encrypted at rest.
func (s *MySQLStore) Save(ctx context.Context, userID string, cfg *config.AppConfig) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	now := time.Now().UTC()
	if err := s.saveConnections(ctx, tx, userID, cfg.Connections, now); err != nil {
		return err
	}
	if err := s.saveLLMProfiles(ctx, tx, userID, cfg.LLMProfiles, now); err != nil {
		return err
	}
	if err := s.saveSettings(ctx, tx, userID, cfg); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLStore) saveConnections(ctx context.Context, tx *sql.Tx, userID string, conns []config.Connection, now time.Time) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM connections WHERE owner_id = ?", userID); err != nil {
		return err
	}
	const ins = "INSERT INTO connections (id, owner_id, name, `type`, host, port, `user`, password, `database`, auth_source, ssl_mode, created_at, updated_at) " +
		"VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)"
	for i, c := range conns {
		encPwd, err := s.cipher.Encrypt(c.Password)
		if err != nil {
			return err
		}
		ts := now.Add(time.Duration(i) * time.Microsecond)
		if _, err := tx.ExecContext(ctx, ins, c.ID, userID, c.Name, string(c.Type), c.Host, c.Port,
			c.User, encPwd, c.Database, c.AuthSource, c.SSLMode, ts, ts); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) saveLLMProfiles(ctx context.Context, tx *sql.Tx, userID string, profiles []config.LLMProfile, now time.Time) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM llm_profiles WHERE owner_id = ?", userID); err != nil {
		return err
	}
	const ins = "INSERT INTO llm_profiles (id, owner_id, name, base_url, model, api_key, created_at, updated_at) " +
		"VALUES (?,?,?,?,?,?,?,?)"
	for i, p := range profiles {
		encKey, err := s.cipher.Encrypt(p.APIKey)
		if err != nil {
			return err
		}
		ts := now.Add(time.Duration(i) * time.Microsecond)
		if _, err := tx.ExecContext(ctx, ins, p.ID, userID, p.Name, p.BaseURL, p.Model, encKey, ts, ts); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) saveSettings(ctx context.Context, tx *sql.Tx, userID string, cfg *config.AppConfig) error {
	const q = "INSERT INTO user_settings (owner_id, active_llm_id, allow_writes, max_rows, max_agent_steps) " +
		"VALUES (?,?,?,?,?) " +
		"ON DUPLICATE KEY UPDATE active_llm_id=VALUES(active_llm_id), allow_writes=VALUES(allow_writes), " +
		"max_rows=VALUES(max_rows), max_agent_steps=VALUES(max_agent_steps)"
	_, err := tx.ExecContext(ctx, q, userID, cfg.ActiveLLMID, cfg.LLM.AllowWrites, cfg.LLM.MaxRows, cfg.LLM.MaxAgentSteps)
	return err
}

// splitStatements strips full-line SQL comments and splits the script on ";".
func splitStatements(script string) []string {
	lines := strings.Split(script, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "--") {
			continue
		}
		kept = append(kept, ln)
	}
	var out []string
	for _, stmt := range strings.Split(strings.Join(kept, "\n"), ";") {
		if trimmed := strings.TrimSpace(stmt); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
