// Command server runs AI-DB as a multi-user HTTP service. It shares the same
// internal/app service layer as the desktop build, but swaps the single-user
// JSON store for an owner-scoped MySQL store and gates every call behind a
// session cookie.
//
// Phase 0 wires one method end-to-end (ListConnections) to prove the transport
// abstraction, tenant partitioning and auth gate. Later phases add the rest.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"AI-DB/frontend"
	"AI-DB/internal/app"
	"AI-DB/internal/auth"
	"AI-DB/internal/db"
	"AI-DB/internal/egress"
	"AI-DB/internal/store"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type server struct {
	svc      *app.Service
	users    auth.UserRepository
	sessions *auth.SessionManager
	// dummyHash is a throwaway bcrypt hash compared against when the username is
	// unknown, so login spends the same bcrypt cost either way and response time
	// cannot be used to enumerate valid usernames.
	dummyHash string
	// loginLimit throttles per IP+username; loginIPLimit throttles per IP at a
	// higher threshold to catch spraying. audit records security events. All three
	// are server-only and nil-safe, so test servers may leave them unset.
	loginLimit   *loginLimiter
	loginIPLimit *loginLimiter
	audit        *auditLog
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run(ctx context.Context) error {
	dsn := getenv("AIDB_MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/aidb?parseTime=true&charset=utf8mb4")
	addr := getenv("AIDB_ADDR", ":8080")
	dataDir := getenv("AIDB_DATA_DIR", "./data")
	cookieSecure := getenv("AIDB_COOKIE_SECURE", "false") == "true"
	seedDemo := getenv("AIDB_SEED_DEMO", "false") == "true"

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	masterKey, err := store.LoadOrCreateKey(filepath.Join(dataDir, "master.key"))
	if err != nil {
		return fmt.Errorf("master key: %w", err)
	}
	sessionSecret, err := store.LoadOrCreateKey(filepath.Join(dataDir, "session.secret"))
	if err != nil {
		return fmt.Errorf("session secret: %w", err)
	}
	cipher, err := store.NewCipher(masterKey)
	if err != nil {
		return err
	}

	sqlDB, err := openDB(ctx, dsn)
	if err != nil {
		return fmt.Errorf("mysql: %w", err)
	}
	defer sqlDB.Close()

	st := store.NewMySQLStore(sqlDB, cipher)
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	users := auth.NewMySQLUsers(sqlDB)
	if err := seed(ctx, users, sqlDB, cipher, seedDemo, getenv("AIDB_SEED_DEMO_HOST", "db.internal.example")); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	// The first admin is minted only from env (no open registration); later
	// accounts are created by an admin via POST /api/admin/users.
	if err := bootstrapAdmin(ctx, users, getenv("AIDB_ADMIN_USERNAME", ""), getenv("AIDB_ADMIN_PASSWORD", "")); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	// Fail loudly if the server would come up with zero accounts. There is no open
	// registration and accounts are admin-only, so an empty users table means
	// nobody can ever log in — yet the process would otherwise start and print
	// "listening", a silent dead end that is painful to diagnose after the fact.
	if n, err := users.Count(ctx); err != nil {
		return fmt.Errorf("count users: %w", err)
	} else if n == 0 {
		return fmt.Errorf("no user accounts exist: set AIDB_ADMIN_USERNAME/AIDB_ADMIN_PASSWORD (or AIDB_SEED_DEMO=true) to bootstrap the first admin")
	}

	mgr := db.NewManager()
	// Server-only connection governance: global/per-user caps and idle reaping.
	// The desktop build never calls SetLimits and stays unlimited.
	limits := db.Limits{
		MaxGlobal:   atoiEnv("AIDB_MAX_CONNS_GLOBAL", 200),
		MaxPerOwner: atoiEnv("AIDB_MAX_CONNS_PER_USER", 8),
		IdleTTL:     durEnv("AIDB_CONN_IDLE_TTL", 30*time.Minute),
	}
	mgr.SetLimits(limits)
	defer mgr.Stop()

	// SSRF egress policy: default-deny loopback/link-local/metadata, allow internal
	// ranges; operators extend via AIDB_EGRESS_DENY_CIDR / AIDB_EGRESS_ALLOW_CIDR.
	policy, err := egress.New(
		splitList(getenv("AIDB_EGRESS_ALLOW_CIDR", "")),
		splitList(getenv("AIDB_EGRESS_DENY_CIDR", "")),
	)
	if err != nil {
		return err
	}
	queryTimeout := durEnv("AIDB_QUERY_TIMEOUT", 30*time.Second)

	svc := app.NewService(st, mgr)
	svc.SetMutationStore(st)
	svc.SetHardening(app.Hardening{
		Egress:              policy,
		QueryTimeout:        queryTimeout,
		RequireExplicitHost: true, // never silently default a host to 127.0.0.1 on the server
		MutationGate:        true, // audit queries; DBA-approve writes; agent read-only
	})
	log.Printf("hardening: query timeout %s, conns max %d/user %d global, idle reap %s",
		queryTimeout, limits.MaxPerOwner, limits.MaxGlobal, limits.IdleTTL)

	// Precompute a throwaway bcrypt hash so login always runs exactly one bcrypt
	// comparison, whether or not the username exists. Without it the unknown-user
	// path short-circuits before bcrypt and answers ~100ms faster, a measurable
	// timing oracle for username enumeration.
	dummyHash, err := auth.HashPassword(uuid.NewString())
	if err != nil {
		return fmt.Errorf("dummy hash: %w", err)
	}

	// Login throttling knobs. Two limiters: per IP+username (targeted brute force)
	// and per IP at 4x the threshold (credential spraying across many usernames).
	loginMaxFails := atoiEnv("AIDB_LOGIN_MAX_FAILS", 5)
	loginWindow := durEnv("AIDB_LOGIN_WINDOW", 10*time.Minute)
	loginBlock := durEnv("AIDB_LOGIN_BLOCK", 15*time.Minute)

	s := &server{
		svc:          svc,
		users:        users,
		sessions:     auth.NewSessionManager(sessionSecret, 12*time.Hour, cookieSecure),
		dummyHash:    dummyHash,
		loginLimit:   newLoginLimiter(loginMaxFails, loginWindow, loginBlock),
		loginIPLimit: newLoginLimiter(loginMaxFails*4, loginWindow, loginBlock),
		audit:        newAuditLog(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)
	// Authenticated RPC endpoints: all 23 bound service methods (see rpc.go),
	// each mirroring its desktop Wails binding's signature and error semantics.
	s.registerAPIRoutes(mux)
	// Streaming agent endpoint (server-only, NOT a Wails binding): the SSE sibling
	// of POST /api/AgentChat that pushes each agent step live. Kept out of
	// registerAPIRoutes so the bound 23-method surface (and frontend/wailsjs) is
	// untouched.
	mux.Handle("POST /api/AgentChatStream", s.gate(http.HandlerFunc(s.handleAgentChatStream)))
	// Self-service profile (any authenticated user).
	mux.Handle("GET /api/me", s.gate(http.HandlerFunc(s.handleGetMe)))
	mux.Handle("PUT /api/me", s.gate(http.HandlerFunc(s.handleUpdateMe)))
	mux.Handle("POST /api/me/password", s.gate(http.HandlerFunc(s.handleChangePassword)))
	// Admin-only account management (RequireAdmin runs inside the live account gate).
	mux.Handle("POST /api/admin/users", s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminCreateUser))))
	mux.Handle("GET /api/admin/users", s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminListUsers))))
	mux.Handle("PATCH /api/admin/users/{id}", s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminUpdateUser))))
	mux.Handle("POST /api/admin/users/{id}/password", s.gate(auth.RequireAdmin(http.HandlerFunc(s.handleAdminSetPassword))))

	// Same-origin static hosting of the built web UI (frontend/dist, embedded via
	// the AI-DB/frontend package). Registered as the least-specific "GET /" so
	// every /api/* pattern above wins; spaHandler serves real files, falls back to
	// index.html for client-side routes, and 404s unmatched /api paths. Serving
	// same-origin is what makes the SameSite=Strict session cookie work in a
	// browser with no CORS setup.
	distFS, err := frontend.Dist()
	if err != nil {
		return fmt.Errorf("frontend dist: %w", err)
	}
	mux.Handle("GET /", spaHandler(distFS))

	httpSrv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("AI-DB server listening on %s (cookie secure=%t)", addr, cookieSecure)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Println("shutting down…")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Drain in-flight HTTP requests before closing the DB drivers they use;
		// otherwise queries running during the drain window hit closed pools.
		err := httpSrv.Shutdown(shutCtx)
		mgr.CloseAll()
		return err
	}
}

// maxAuthBodyBytes caps the login / admin-create request bodies. These carry only
// a username, password and optional role, so 64 KiB is generous; the limit stops
// a client from streaming an unbounded body into the shared server process.
const maxAuthBodyBytes = 64 << 10 // 64 KiB

// handleLogin verifies credentials and sets the session cookie. It always
// returns HTTP-level {ok,error} JSON so the frontend login() helper can read it
// without branching on status.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}

	// Throttle repeated failures before spending any bcrypt work. Two buckets:
	// IP+username (targeted guessing) and IP alone at a higher threshold
	// (spraying many usernames). Either tripping answers 429 + Retry-After.
	ip := clientIP(r)
	userKey := ip + "|" + strings.ToLower(strings.TrimSpace(body.Username))
	ipKey := "ip|" + ip
	if wait, blocked := s.loginLimit.blocked(userKey); blocked {
		s.audit.loginBlocked(r, body.Username, wait)
		writeRateLimited(w, wait)
		return
	}
	if wait, blocked := s.loginIPLimit.blocked(ipKey); blocked {
		s.audit.loginBlocked(r, body.Username, wait)
		writeRateLimited(w, wait)
		return
	}

	u, err := s.users.ByUsername(r.Context(), body.Username)
	if err != nil && !errors.Is(err, auth.ErrNoUser) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "auth error"})
		return
	}
	// Always run one bcrypt comparison: against the real hash when the user
	// exists, the throwaway hash otherwise. This equalises response time so it
	// cannot reveal whether the username is valid (no timing oracle). The dummy
	// hash never matches, so u==nil still yields 401.
	hash := s.dummyHash
	if u != nil {
		hash = u.PasswordHash
	}
	if !auth.VerifyPassword(hash, body.Password) || u == nil {
		// Count the failure against both buckets, then answer identically for an
		// unknown user and a wrong password (no username oracle).
		s.loginLimit.recordFailure(userKey)
		s.loginIPLimit.recordFailure(ipKey)
		s.audit.loginFailure(r, body.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "用户名或密码错误"})
		return
	}
	if u.Banned {
		s.audit.loginFailure(r, body.Username)
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "账号已被封禁"})
		return
	}
	value, err := s.sessions.Issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "session error"})
		return
	}
	// A good login clears the failure state so earlier typos never lock a real
	// user out, and records the success for the audit trail.
	s.loginLimit.recordSuccess(userKey)
	s.loginIPLimit.recordSuccess(ipKey)
	s.audit.loginSuccess(r, u.Username, u.Role)
	http.SetCookie(w, s.sessions.Cookie(value))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": u.Username, "role": u.Role})
}

// writeRateLimited answers 429 with a Retry-After hint so a throttled client
// knows the block is temporary and how long to wait.
func writeRateLimited(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "登录尝试过多，请稍后再试"})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Best-effort actor attribution: the cookie may already be gone/expired, in
	// which case logout is still honoured but not attributable.
	if sess, err := s.sessions.FromRequest(r); err == nil {
		s.audit.logout(r, sess.Username)
	}
	http.SetCookie(w, s.sessions.ClearCookie())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessions.FromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	u, err := s.users.ByIDAuth(r.Context(), sess.UserID)
	if err != nil || u.Banned || sess.TokenVersion != u.TokenVersion {
		// Drop a stale cookie when the account vanished, was banned, or password changed.
		http.SetCookie(w, s.sessions.ClearCookie())
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": u.Username, "role": u.Role})
}

// gate authenticates via cookie then reloads live ban/role/token_version.
func (s *server) gate(next http.Handler) http.Handler {
	return s.sessions.Gate(s.users, next)
}

// handleAdminCreateUser mints a new account (admin-only, the sole creation path
// once the first admin exists — there is no open registration). Body:
// {username,password,role?}; role defaults to "user" and must be "admin"/"user".
func (s *server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	// The RequireAdmin middleware already injected the caller's principal.
	actor := ""
	if p, ok := auth.PrincipalFromContext(r.Context()); ok {
		actor = p.Username
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "username and password are required"})
		return
	}
	if msg := validatePassword(body.Password); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
		return
	}
	role := body.Role
	if role == "" {
		role = auth.RoleUser
	}
	if !auth.ValidAccountRole(role) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "role must be admin, dba, or user"})
		return
	}
	if _, err := s.users.ByUsername(r.Context(), body.Username); err == nil {
		s.audit.adminCreateUser(r, actor, body.Username, role, "error", "username already exists")
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "username already exists"})
		return
	} else if !errors.Is(err, auth.ErrNoUser) {
		log.Printf("admin create user: lookup %q failed: %v", body.Username, err)
		s.audit.adminCreateUser(r, actor, body.Username, role, "error", "lookup failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "查询用户失败"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "hash error"})
		return
	}
	u := auth.User{ID: uuid.NewString(), Username: body.Username, PasswordHash: hash, Role: role}
	if err := s.users.Create(r.Context(), u); err != nil {
		log.Printf("admin create user: create %q failed: %v", u.Username, err)
		s.audit.adminCreateUser(r, actor, u.Username, u.Role, "error", "create failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "创建用户失败"})
		return
	}
	// The structured audit line supersedes the old plain log line.
	s.audit.adminCreateUser(r, actor, u.Username, u.Role, "success", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": u.ID, "username": u.Username, "role": u.Role})
}

// handleAdminListUsers returns the account directory (admin-only), projected to a
// safe view that never includes password hashes.
func (s *server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	actor := ""
	if p, ok := auth.PrincipalFromContext(r.Context()); ok {
		actor = p.Username
	}
	all, err := s.users.List(r.Context())
	if err != nil {
		log.Printf("admin list users failed: %v", err)
		s.audit.adminListUsers(r, actor, 0, "error")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "获取用户列表失败"})
		return
	}
	type userView struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		Role        string `json:"role"`
		DisplayName string `json:"displayName"`
		Banned      bool   `json:"banned"`
	}
	out := make([]userView, 0, len(all))
	for _, u := range all {
		out = append(out, userView{
			ID: u.ID, Username: u.Username, Role: u.Role,
			DisplayName: u.DisplayName, Banned: u.Banned,
		})
	}
	s.audit.adminListUsers(r, actor, len(all), "success")
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// openDB connects to MySQL, creating the target database if it does not exist
// so a fresh `go run ./cmd/server` works with only server credentials.
func openDB(ctx context.Context, dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	dbName := cfg.DBName
	if dbName == "" {
		dbName = "aidb"
	}

	bootstrap := cfg.Clone()
	bootstrap.DBName = ""
	tmp, err := sql.Open("mysql", bootstrap.FormatDSN())
	if err != nil {
		return nil, err
	}
	defer tmp.Close()
	// Escape backticks so a dbName containing one cannot break out of the
	// identifier quoting (defense-in-depth; dbName is operator-supplied via DSN).
	escaped := strings.ReplaceAll(dbName, "`", "``")
	create := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4", escaped)
	if _, err := tmp.ExecContext(ctx, create); err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}

	cfg.DBName = dbName
	cfg.ParseTime = true
	sqlDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return sqlDB, nil
}

// seed creates two isolated demo tenants so the cross-tenant isolation assertion
// can be run immediately. It is OPT-IN via AIDB_SEED_DEMO=true (default off) so a
// server pointed at an empty database — including a misconfigured production or CI
// instance — never silently gains known-credential accounts. Passwords come from
// AIDB_SEED_ALICE_PWD / AIDB_SEED_BOB_PWD, or are randomly generated when unset.
// An operator-supplied password is never echoed; only a generated one is logged
// once (tagged DEMO-ONLY) since it would otherwise be unrecoverable. seed is a
// no-op once any user exists.
func seed(ctx context.Context, users auth.UserRepository, sqlDB *sql.DB, cipher *store.Cipher, enabled bool, demoHost string) error {
	if !enabled {
		return nil
	}
	n, err := users.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	demo := []struct {
		username string
		pwdEnv   string
		connName string
		connType string
		host     string
		port     int
	}{
		{"alice", "AIDB_SEED_ALICE_PWD", "A-db", "mysql", demoHost, 3306},
		{"bob", "AIDB_SEED_BOB_PWD", "B-db", "postgres", demoHost, 5432},
	}
	for _, d := range demo {
		// An operator-supplied password is used verbatim and never echoed; only a
		// randomly generated one is logged (once) because it is otherwise lost.
		password := os.Getenv(d.pwdEnv)
		generated := password == ""
		if generated {
			password = uuid.NewString()
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		uid := uuid.NewString()
		if err := users.Create(ctx, auth.User{ID: uid, Username: d.username, PasswordHash: hash, Role: "user"}); err != nil {
			return err
		}
		encPwd, err := cipher.Encrypt("demo-secret")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		_, err = sqlDB.ExecContext(ctx,
			"INSERT INTO connections (id, owner_id, name, `type`, host, port, `user`, password, `database`, auth_source, ssl_mode, created_at, updated_at) "+
				"VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
			uuid.NewString(), uid, d.connName, d.connType, d.host, d.port, "demo_user", encPwd, "", "", "", now, now)
		if err != nil {
			return err
		}
		if generated {
			log.Printf("[DEMO-ONLY] seeded tenant %q with a RANDOM password %q and connection %q — record it now; disable via AIDB_SEED_DEMO=false", d.username, password, d.connName)
		} else {
			log.Printf("[DEMO-ONLY] seeded tenant %q (password from %s env) with connection %q — disable via AIDB_SEED_DEMO=false", d.username, d.pwdEnv, d.connName)
		}
	}
	log.Printf("[DEMO-ONLY] demo connections point at host %q (AIDB_SEED_DEMO_HOST); they exist to demonstrate tenant isolation via ListConnections — set AIDB_SEED_DEMO_HOST to a reachable internal DB host to actually connect (the default egress policy blocks loopback)", demoHost)
	return nil
}

// bootstrapAdmin creates the first administrator when AIDB_ADMIN_USERNAME and
// AIDB_ADMIN_PASSWORD are set and that username does not yet exist. This is the
// ONLY way to mint an admin (there is no open registration); later accounts are
// created by an admin via POST /api/admin/users. It is idempotent — re-running
// with an existing username is a no-op, so restarts never duplicate or reset it,
// and the password is never logged.
func bootstrapAdmin(ctx context.Context, users auth.UserRepository, username, password string) error {
	if username == "" || password == "" {
		return nil // not configured; skip silently
	}
	if _, err := users.ByUsername(ctx, username); err == nil {
		log.Printf("admin bootstrap: account %q already exists, skipping", username)
		return nil // idempotent no-op
	} else if !errors.Is(err, auth.ErrNoUser) {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := users.Create(ctx, auth.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		Role:         auth.RoleAdmin,
	}); err != nil {
		return err
	}
	log.Printf("admin bootstrap: created first admin %q (further accounts via POST /api/admin/users)", username)
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// atoiEnv reads a non-negative int env var, logging and falling back to def on a
// malformed value so a typo cannot silently disable a limit or crash startup.
func atoiEnv(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("ignoring invalid %s=%q, using default %d", key, v, def)
		return def
	}
	return n
}

// durEnv reads a non-negative duration env var (e.g. "30s", "30m"), logging and
// falling back to def on a malformed value.
func durEnv(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		log.Printf("ignoring invalid %s=%q, using default %s", key, v, def)
		return def
	}
	return d
}

// splitList splits a comma-separated env value into trimmed, non-empty items.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// spaHandler serves the embedded frontend same-origin. A request for an existing
// file (index.html, assets/…) is served directly; any other non-/api GET falls
// back to index.html so the single-page app owns client-side navigation. Paths
// under /api/ that reached here matched no route, so they 404 rather than return
// the HTML shell — a mistyped endpoint must not be masked by a 200 index page.
func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		// fs.FS rejects invalid paths (leading "..", etc.), so a traversal attempt
		// simply misses here and falls through to the index.html fallback.
		if f, err := dist.Open(name); err == nil {
			_ = f.Close()
			files.ServeHTTP(w, r)
			return
		}
		index, err := dist.Open("index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		defer index.Close()
		data, err := io.ReadAll(index)
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}
