# AI-DB

面向 **MySQL**、**PostgreSQL**、**Redis**、**MongoDB** 的桌面客户端，技术栈为 **Go + Wails + React**。

内置 Query 工作台，以及基于 OpenAI 兼容接口的多步 **Agent**（工具：查库表、执行语句、Explain 等）。

同一套内核还提供 **多用户服务端**（`cmd/server`）：以 HTTP 托管 Web 界面、Session Cookie 鉴权、MySQL 按用户存储——见[服务端模式](#服务端模式多用户)。

[English](README.md)

界面布局 / 主题 / 中英双语与 Ai-Shell 同一套模式（左侧栏、明暗色 token、zh/en）。

## 功能

- 连接配置：新建 / 编辑 / 测试 / 连接（MySQL、PostgreSQL、Redis、MongoDB）
- **工作台「当前库」**：在已打开连接上切换库（MySQL `USE`、PostgreSQL 重连目标库、Redis `Options.DB`、Mongo 当前库）
- SQL / 命令编辑器、结果表格、按需展开的对象 / Key 树
- Agent 对话与工具调用（绑定当前连接）；未选库时发送会先弹出选连接
- 侧栏 **Query / Agent** 共用连接上下文；首页 / 新建任务先清空预选连接，发送或进 Query 时再选库
- **复制到其它库** 等 playbook（SQL 引擎；PostgreSQL 跨库不安全时会限制）
- **偏好设置**：浅色 / 暗色、中文 / English、界面字体与字号
- LLM：多配置（`baseUrl`、API Key、模型）、最大行数、Agent 最大步数
- 安全：默认禁止写操作；在「偏好 → 安全与限额」中开启

## 环境要求

- Go 1.22+（模块可能声明更新的 toolchain，请使用较新的 Go）
- Node.js 18+
- [Wails v2](https://wails.io) CLI（`go install github.com/wailsapp/wails/v2/cmd/wails@latest`）

## 开发

```bash
wails dev
```

## 构建

```bash
wails build
```

产物在 `build/bin/`。

## 配置

保存在系统用户配置目录：

- Windows：`%AppData%\aidb\config.json`
- macOS：`~/Library/Application Support/aidb/config.json`
- Linux：`~/.config/aidb/config.json`

密码与 API Key 使用本地 AES-GCM 加密；配置目录下会生成随机 `master.key`（权限 `0600`）。连接列表不下发明文密码（编辑时密码留空表示保持原值）。

PostgreSQL 可配置 SSL（默认 `prefer`；服务器拒绝明文时用 `require`）。Redis 库号可留空（默认 `0`）。

## 服务端模式（多用户）

`cmd/server` 把 AI-DB 作为面向多用户的 HTTP 服务运行。它与桌面版**共用同一套
`internal/app` 内核**（每个请求都带按用户的 `Principal`），但把单用户 JSON 文件换成
**按 owner 分区的 MySQL 存储**，并将每个调用置于 **HttpOnly Session Cookie** 门禁之后。
每个用户只能看到自己的连接、LLM 配置与 Agent 会话。

服务端二进制**内嵌 `frontend/dist` 并在 `/` 同源托管**（未命中的前端路由回落
`index.html`；未匹配的 `/api/*` 返回 404）。同源托管正是 `SameSite=Strict` Cookie
能在普通浏览器里直接生效、无需 CORS 或反向代理的原因。

### 前置条件

- 一个可连通的 **MySQL** 实例，用于存放应用数据与账号（DSN 指定的库若不存在会
  自动创建）。
- Web 界面至少构建过一次：`dist/` 被 gitignore，而 `go:embed` 在编译期需要它
  （与桌面构建已有的前置条件一致）。

### 构建与运行

```bash
# 1. 构建 Web 界面（产出 frontend/dist，go:embed 需要）
cd frontend && npm install && npm run build && cd ..

# 2. 构建服务端二进制
go build -o build/bin/aidb-server ./cmd/server

# 3. 运行（bash）。置于 HTTPS 之后时设 AIDB_COOKIE_SECURE=true。
AIDB_MYSQL_DSN='user:pass@tcp(db-host:3306)/aidb?parseTime=true&charset=utf8mb4' \
AIDB_ADMIN_USERNAME=admin AIDB_ADMIN_PASSWORD='change-me' \
AIDB_COOKIE_SECURE=true \
./build/bin/aidb-server
```

PowerShell：用 `$env:AIDB_MYSQL_DSN='...'` 逐行设置变量，再运行
`.\build\bin\aidb-server.exe`。

### 本地开发（VS Code F5）

`.vscode/launch.json` 内置 `Server (cmd/server)` 启动配置：按 **F5** 即可以调试模式
构建并运行服务端，环境变量为本地开发值——`AIDB_MYSQL_DSN` 指向
`127.0.0.1:3306`，以及引导管理员（`admin` / `admin123456`，仅在用户表为空时创建，请自行修改）。
按本地 MySQL 实际情况编辑 `env` 块即可。前置条件：`frontend/dist` 需先存在一次
（`cd frontend && npm install && npm run build`），因为二进制内嵌它。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AIDB_MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/aidb?parseTime=true&charset=utf8mb4` | 后端 MySQL DSN（应用数据 + 账号）。 |
| `AIDB_ADDR` | `:8080` | 监听地址。 |
| `AIDB_DATA_DIR` | `./data` | 存放 `master.key`（凭据 AES-GCM）+ `session.secret`（HMAC Cookie）。须保密，切勿提交。 |
| `AIDB_COOKIE_SECURE` | `false` | 置于 HTTPS 之后时设 `true`，使 Session Cookie 带 `Secure`。 |
| `AIDB_ADMIN_USERNAME` / `AIDB_ADMIN_PASSWORD` | _(空)_ | 引导首个管理员（仅当两者都设且用户名不存在时）。幂等；绝不打印密码。 |
| `AIDB_SEED_DEMO` | `false` | 播种两个演示租户（`alice`/`bob`）用于隔离验证。 |
| `AIDB_SEED_DEMO_HOST` | `db.internal.example` | 演示连接的主机（刻意避开回环）。 |
| `AIDB_SEED_ALICE_PWD` / `AIDB_SEED_BOB_PWD` | _(随机)_ | 演示密码；未设则随机生成并打印一次。 |
| `AIDB_MAX_CONNS_GLOBAL` | `200` | 全体用户的目标库活动连接上限（`0` = 不限）。 |
| `AIDB_MAX_CONNS_PER_USER` | `8` | 每用户目标库连接上限（`0` = 不限）。 |
| `AIDB_CONN_IDLE_TTL` | `30m` | 空闲目标库连接超过此时长即回收（`0` = 不回收）。 |
| `AIDB_QUERY_TIMEOUT` | `30s` | 单次查询 / Agent 工具调用的超时（`0` = 不限）。 |
| `AIDB_EGRESS_ALLOW_CIDR` | _(空)_ | 允许网段白名单；一旦设置，**仅**这些网段（减去 deny）可达。面向公网部署时使用。 |
| `AIDB_EGRESS_DENY_CIDR` | _(空)_ | 在默认拒绝集之上追加屏蔽的网段。 |
| `AIDB_LOGIN_MAX_FAILS` | `5` | 单个（IP+用户名）桶触发冷却封锁前的失败登录次数。仅 IP 桶用此值的 4× 以遏制撞库喷洒。 |
| `AIDB_LOGIN_WINDOW` | `10m` | 累计失败登录次数的滑动窗口。 |
| `AIDB_LOGIN_BLOCK` | `15m` | 触发阈值后桶的封锁时长（以 `429` + `Retry-After` 响应）。 |

### 账号

**没有开放注册**。首个管理员只能在启动时经 `AIDB_ADMIN_USERNAME` +
`AIDB_ADMIN_PASSWORD` 创建。当**账号数为 0 时服务端拒绝启动**（否则无人能登录或建号）。
之后由管理员用 `POST /api/admin/users`（JSON `{username, password, role?}`）建号，
用 `GET /api/admin/users` 列出账号。

### 出口 / SSRF 策略

默认阻断对回环、链路本地与云元数据（`169.254.169.254`、`fd00:ec2::254`）的出口——
**同时覆盖**目标库连接与 LLM `baseUrl`——但**放行** RFC1918 私网，因为内网库与自建
LLM 端点是常态。面向公网部署时，用 `AIDB_EGRESS_ALLOW_CIDR` 精确指定可达网段
（白名单模式）。桌面版不受影响（其策略为 `nil` = 放行一切）。

### 安全加固（仅服务端）

以下能力在**桌面版全部为 no-op**——它们位于 `cmd/server`，Wails 路径永不构造。

- **登录限流。** 滑动窗口限流器在 `AIDB_LOGIN_WINDOW` 内累计 `AIDB_LOGIN_MAX_FAILS`
  次失败后，将桶封锁 `AIDB_LOGIN_BLOCK`。每次尝试跟踪两个桶：`IP+用户名`（定向爆破）
  与仅 `IP`（阈值为 4×，遏制撞库喷洒）。登录成功即清空两桶。客户端 IP 取自套接字对端——
  **不信任** `X-Forwarded-For`（极易伪造）。被封锁的尝试返回 `429` 与 `Retry-After` 提示。
- **审计日志。** 安全相关事件以单行结构化 JSON（前缀 `AUDIT`）输出到标准日志：`login`
  （success/failure/rate_limited）、`logout`、`admin_create_user`、`admin_list_users`，
  以及写操作 / 探测出口的 `rpc` 调用。记录**只含标识符与结果**（actor、role、IP、method、
  target、outcome），**绝不含 payload**——密码、API key 与查询数据永不落盘。被出口策略
  拦截的调用标记 `outcome=egress_denied`。
- **Agent 流式对话。** `POST /api/AgentChatStream` 与 `AgentChat` 跑同一套 agent 循环，
  但把每一步（thinking / tool / tool_result / message）作为一个 SSE 帧实时下发，最后以
  `done` 帧携带完整响应（或 `error` 帧）。粒度是**每个 agent 步骤**而非每个 token——LLM
  客户端读取的是完整响应。它仅服务端可用，且刻意**不做成 Wails 绑定方法**，故
  `frontend/wailsjs` 保持不变、桌面继续用一次性绑定。客户端断开会取消请求上下文并中止
  在飞的循环。Web UI 用 `fetch` + `ReadableStream` 消费；桌面下 `AgentChatStream` 透明回退到
  一次性调用并回放返回的步骤。
- **管理后台。** Web 版仅对 `role=admin` 的会话显示「用户管理」入口，打开一个基于
  `GET`/`POST /api/admin/users` 的页内面板。这些端点在服务端另由 `RequireAdmin` 兜底，
  故 UI 只是便利入口，并非安全边界。

## Agent 工具

| 引擎 | 工具 |
|------|------|
| MySQL / PostgreSQL | `list_schemas`、`list_tables`、`describe_table`、`execute_query`、`explain_plan` |
| PostgreSQL | `list_schemas` 返回**数据库**；`list_tables` / `describe_table` 的 `schema=` 是命名空间（默认 `public`）。可选 `database=` **临时**查看其它库，不改变工作台当前库。 |
| Redis | `redis_keys`、`redis_type`、`redis_get`、`execute_query` |
| MongoDB | `list_schemas`、`list_tables`、`describe_table`、`execute_query`（mongosh 风格；无 `explain_plan` / Redis 工具） |

## 安全说明

- 切换「当前库」会等待进行中的查询结束，并在替换连接池 / Redis client 期间阻止新的 Acquire。
- 对象树浏览非当前库时使用临时连接，不会悄悄改变 Execute 落库目标。
