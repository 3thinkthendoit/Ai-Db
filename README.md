# AI-DB

Desktop GUI client for **MySQL**, **PostgreSQL**, **Redis**, and **MongoDB**, built with **Go + Wails + React**.

Includes a Query workbench and a multi-step **Agent** that uses an OpenAI-compatible LLM with tools (schema inspect, execute query, explain).

The same kernel also ships as a **multi-user server** (`cmd/server`) that hosts the web UI over HTTP with session-cookie auth and MySQL-backed per-user storage — see [Server mode](#server-mode-multi-user).

[中文说明](README.zh-CN.md)

UI layout / theme / i18n follow the same patterns as Ai-Shell (left sidebar, light/dark tokens, zh/en).

## Features

- Connection profiles (create / edit / test / connect) for MySQL, PostgreSQL, Redis, MongoDB
- **Workbench「当前库」**: switch catalog / logical DB on the live connection (MySQL `USE`, PostgreSQL database reconnect, Redis `Options.DB`, Mongo active DB)
- Query editor + result grid + lazy object / key browser
- Agent chat with tool-calling (bound to the active connection); pick a connection when sending if none is selected
- Sidebar **Query / Agent** surfaces with shared connection context; home / new task clears the pre-selected connection until you pick one
- **Copy to other database** playbooks (SQL engines; PG limited where cross-DB SQL is unsafe)
- **Preferences**: light/dark theme, Chinese / English, UI font family & size
- LLM settings: profiles (`baseUrl`, API key, model), max rows, max agent steps
- Safety: writes disabled by default; enable under Preferences → Safety & limits

## Requirements

- Go 1.22+ (module targets a newer toolchain; use a current Go release)
- Node.js 18+
- [Wails v2](https://wails.io) CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

## Develop

```bash
wails dev
```

## Build

```bash
wails build
```

Binary output is under `build/bin/`.

## Config

Saved under the OS user config directory:

- Windows: `%AppData%\aidb\config.json`
- macOS: `~/Library/Application Support/aidb/config.json`
- Linux: `~/.config/aidb/config.json`

Passwords and API keys are stored with local AES-GCM. A random `master.key` is created under the config directory (`0600`). Connection passwords are not sent to the UI list (edit form keeps existing password when left blank).

PostgreSQL SSL mode is configurable on the connection (`prefer` by default; use `require` when the server rejects non-TLS). Redis database index may be left blank (defaults to `0`).

## Server mode (multi-user)

`cmd/server` runs AI-DB as an HTTP service for many users. It shares the exact
same `internal/app` service kernel as the desktop build (every request carries a
per-user `Principal`), but swaps the single-user JSON file for an **owner-scoped
MySQL store** and gates every call behind an **HttpOnly session cookie**. Each
user only ever sees their own connections, LLM profiles and agent sessions.

The binary **embeds `frontend/dist` and serves it same-origin at `/`** (SPA
fallback to `index.html`; unmatched `/api/*` returns 404). Same-origin hosting is
what makes the `SameSite=Strict` cookie work in a plain browser with no CORS or
reverse proxy.

### Prerequisites

- A reachable **MySQL** instance for app data + accounts (the database is
  auto-created if the DSN names one that does not exist yet).
- The web UI built at least once, because `dist/` is gitignored and the embed
  needs it at compile time (same prerequisite the desktop build already has).

### Build & run

```bash
# 1. Build the web UI (produces frontend/dist, required by go:embed)
cd frontend && npm install && npm run build && cd ..

# 2. Build the server binary
go build -o build/bin/aidb-server ./cmd/server

# 3. Run it (bash). Set AIDB_COOKIE_SECURE=true behind HTTPS.
AIDB_MYSQL_DSN='user:pass@tcp(db-host:3306)/aidb?parseTime=true&charset=utf8mb4' \
AIDB_ADMIN_USERNAME=admin AIDB_ADMIN_PASSWORD='change-me' \
AIDB_COOKIE_SECURE=true \
./build/bin/aidb-server
```

PowerShell: set vars with `$env:AIDB_MYSQL_DSN='...'` (one per line) then run
`.\build\bin\aidb-server.exe`.

### Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `AIDB_MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/aidb?parseTime=true&charset=utf8mb4` | Backing MySQL DSN (app data + accounts). |
| `AIDB_ADDR` | `:8080` | Listen address. |
| `AIDB_DATA_DIR` | `./data` | Holds `master.key` (credential AES-GCM) + `session.secret` (HMAC cookies). Keep private; never commit. |
| `AIDB_COOKIE_SECURE` | `false` | Set `true` behind HTTPS so the session cookie is `Secure`. |
| `AIDB_ADMIN_USERNAME` / `AIDB_ADMIN_PASSWORD` | _(empty)_ | Bootstrap the first admin (only when both set and the username is absent). Idempotent; never logged. |
| `AIDB_SEED_DEMO` | `false` | Seed two demo tenants (`alice`/`bob`) for isolation testing. |
| `AIDB_SEED_DEMO_HOST` | `db.internal.example` | Host for demo connections (kept off-loopback). |
| `AIDB_SEED_ALICE_PWD` / `AIDB_SEED_BOB_PWD` | _(random)_ | Demo passwords; generated and logged once if unset. |
| `AIDB_MAX_CONNS_GLOBAL` | `200` | Cap on live target-DB connections across all users (`0` = unlimited). |
| `AIDB_MAX_CONNS_PER_USER` | `8` | Per-user target-DB connection cap (`0` = unlimited). |
| `AIDB_CONN_IDLE_TTL` | `30m` | Idle target-DB connections are closed after this (`0` = never). |
| `AIDB_QUERY_TIMEOUT` | `30s` | Deadline per query / agent tool call (`0` = no timeout). |
| `AIDB_EGRESS_ALLOW_CIDR` | _(empty)_ | Allowlist CIDRs; when set, **only** these (minus deny) are reachable. Use for internet-facing deploys. |
| `AIDB_EGRESS_DENY_CIDR` | _(empty)_ | Extra CIDRs to block on top of the default deny set. |
| `AIDB_LOGIN_MAX_FAILS` | `5` | Failed logins per (IP+username) before a cooldown block. The IP-only bucket uses 4× this to curb credential spraying. |
| `AIDB_LOGIN_WINDOW` | `10m` | Sliding window that accumulates failed-login counts. |
| `AIDB_LOGIN_BLOCK` | `15m` | How long a bucket stays blocked once the threshold is hit (answered with `429` + `Retry-After`). |

### Accounts

There is **no open registration**. The first admin is minted only via
`AIDB_ADMIN_USERNAME` + `AIDB_ADMIN_PASSWORD` at startup. The server **refuses to
start when zero accounts exist** (otherwise nobody could ever log in or create a
user). Admins then create further accounts with `POST /api/admin/users`
(JSON `{username, password, role?}`) and list them with `GET /api/admin/users`.

### Egress / SSRF policy

By default the server blocks outbound to loopback, link-local and cloud metadata
(`169.254.169.254`, `fd00:ec2::254`) — on **both** target-DB connects **and** LLM
`baseUrl` — but **allows** RFC1918 private ranges, since internal DBs and
self-hosted LLM endpoints are the normal case. For an internet-facing deployment,
set `AIDB_EGRESS_ALLOW_CIDR` to the exact ranges that should be reachable
(allowlist mode). The desktop build is unaffected (its policy is `nil` = allow).

### Security hardening (server only)

All of the following are **no-ops on desktop** — they live in `cmd/server` and are
never constructed in the Wails path.

- **Login rate limiting.** A sliding-window limiter blocks a bucket after
  `AIDB_LOGIN_MAX_FAILS` failures within `AIDB_LOGIN_WINDOW` for
  `AIDB_LOGIN_BLOCK`. Two buckets are tracked per attempt: `IP+username` (targeted
  brute force) and `IP` alone at 4× the threshold (credential spraying). A
  successful login clears both. The client IP is taken from the socket peer —
  `X-Forwarded-For` is **not** trusted (it is trivially spoofable). Blocked
  attempts get `429` with a `Retry-After` hint.
- **Audit log.** Security-relevant events are emitted as single-line structured
  JSON prefixed `AUDIT` on the standard logger: `login`
  (success/failure/rate_limited), `logout`, `admin_create_user`,
  `admin_list_users`, and mutating / egress-probing `rpc` calls. Records carry
  only identifiers and outcomes (actor, role, IP, method, target, outcome) —
  **never** payloads, so passwords, API keys and query data are never written.
  Egress-blocked calls are tagged `outcome=egress_denied`.
- **Streaming agent chat.** `POST /api/AgentChatStream` runs the same agent loop
  as `AgentChat` but streams each step (thinking / tool / tool_result / message)
  as a Server-Sent-Events frame, ending with a `done` frame holding the full
  response (or an `error` frame). Granularity is per **agent step**, not per
  token — the LLM client reads a complete response. It is server-only and
  deliberately **not** a Wails bound method, so `frontend/wailsjs` is unchanged
  and desktop keeps using the one-shot binding. Client disconnects cancel the
  request context and abort the in-flight loop. The web UI consumes it via
  `fetch` + `ReadableStream`; on desktop `AgentChatStream` transparently falls
  back to the one-shot call and replays the returned steps.
- **Admin console.** The web build shows a “用户管理” control only for sessions
  with `role=admin`, opening an in-page panel over `GET`/`POST /api/admin/users`.
  The endpoints are additionally gated server-side by `RequireAdmin`, so the UI
  is a convenience, not the security boundary.

## Agent tools

| Engine | Tools |
|--------|--------|
| MySQL / PostgreSQL | `list_schemas`, `list_tables`, `describe_table`, `execute_query`, `explain_plan` |
| PostgreSQL | `list_schemas` = databases; `list_tables` / `describe_table` use `schema=` as namespace (`public` default). Optional `database=` inspects another catalog **temporarily** (does not change the workbench DB). |
| Redis | `redis_keys`, `redis_type`, `redis_get`, `execute_query` |
| MongoDB | `list_schemas`, `list_tables`, `describe_table`, `execute_query` (mongosh-style helpers; no `explain_plan` / Redis tools) |

## Safety notes

- Catalog / Redis DB switches wait for in-flight queries and block new acquires while the pool or client is replaced.
- Object-tree browse of a non-active database uses a temporary connection and does not silently change where Execute runs.
