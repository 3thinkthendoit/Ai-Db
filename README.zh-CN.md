# AI-DB

面向 **MySQL**、**PostgreSQL**、**Redis**、**MongoDB** 的桌面客户端，技术栈为 **Go + Wails + React**。

内置 Query 工作台，以及基于 OpenAI 兼容接口的多步 **Agent**（工具：查库表、执行语句、Explain 等）。

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
