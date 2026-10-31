# AI-DB

Desktop GUI client for **MySQL**, **PostgreSQL**, **Redis**, and **MongoDB**, built with **Go + Wails + React**.

Includes a Query workbench and a multi-step **Agent** that uses an OpenAI-compatible LLM with tools (schema inspect, execute query, explain).

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
