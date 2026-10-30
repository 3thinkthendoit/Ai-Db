# AI-DB

Desktop GUI client for **MySQL**, **PostgreSQL**, and **Redis**, built with **Go + Wails + React**.

Includes a lightweight Query workbench and a multi-step **Agent** that uses an OpenAI-compatible LLM with tools (schema inspect, execute query, explain).

UI layout / theme / i18n follow the same patterns as Ai-Shell (left sidebar, light/dark tokens, zh/en).

## Features (MVP)

- Connection profiles (create / edit / test / connect)
- Query editor + result grid + object/key browser
- Agent chat with tool-calling (bound to the active connection)
- Sidebar **Query / Agent** switching (shared connection context)
- **Preferences**: light/dark theme, Chinese / English
- LLM settings: `baseUrl`, API key, model, max rows, max steps
- Safety: writes disabled by default; enable in LLM Settings

## Requirements

- Go 1.22+
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

## Agent tools

SQL connections: `list_schemas`, `list_tables`, `describe_table`, `execute_query`, `explain_plan`

Redis: `redis_keys`, `redis_type`, `redis_get`, `execute_query`
