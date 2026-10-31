package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AI-DB/internal/config"
	"AI-DB/internal/db"
)

// ToolRunner executes agent tools against an open connection.
type ToolRunner struct {
	mgr        *db.Manager
	connID     string
	connType   config.ConnType
	allowWrite bool
	maxRows    int
}

// NewToolRunner creates a tool runner bound to a connection.
func NewToolRunner(mgr *db.Manager, connID string, connType config.ConnType, allowWrite bool, maxRows int) *ToolRunner {
	if maxRows <= 0 {
		maxRows = 500
	}
	return &ToolRunner{mgr: mgr, connID: connID, connType: connType, allowWrite: allowWrite, maxRows: maxRows}
}

// Specs returns tool definitions for the current connection type.
func (t *ToolRunner) Specs() []ToolSpec {
	common := []ToolSpec{
		fn("execute_query", t.executeQueryToolDesc(), map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": t.executeQueryArgDesc()},
			},
			"required": []string{"query"},
		}),
	}
	if t.connType == config.ConnRedis {
		return append(common,
			fn("redis_keys", "Scan Redis keys by pattern (limited).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer"},
				},
			}),
			fn("redis_type", "Get Redis key type.", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
				"required": []string{"key"},
			}),
			fn("redis_get", "GET a Redis string key.", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
				"required": []string{"key"},
			}),
		)
	}
	if t.connType == config.ConnMongo {
		// Document console: catalog helpers only — no SQL EXPLAIN.
		return append(common,
			fn("list_schemas", "List MongoDB databases.", map[string]any{"type": "object", "properties": map[string]any{}}),
			fn("list_tables", "List collections in a database (schema = database name).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"schema": map[string]any{"type": "string", "description": "Database name"},
				},
			}),
			fn("describe_table", "List indexes for a collection (schema = database, table = collection).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"schema": map[string]any{"type": "string", "description": "Database name"},
					"table":  map[string]any{"type": "string", "description": "Collection name"},
				},
				"required": []string{"table"},
			}),
		)
	}
	if t.connType == config.ConnPostgres {
		return append(common,
			fn("list_schemas", "List PostgreSQL databases (catalogs). These are NOT schema namespaces.", map[string]any{"type": "object", "properties": map[string]any{}}),
			fn("list_tables", "List tables in a schema namespace. schema defaults to public. Pass database to inspect another PostgreSQL catalog temporarily (does not change the workbench DB).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"schema":   map[string]any{"type": "string", "description": "Schema namespace (usually public)"},
					"database": map[string]any{"type": "string", "description": "Optional other catalog to inspect (temporary; does not change workbench DB)"},
				},
			}),
			fn("describe_table", "Describe columns of a table in a schema namespace (default public).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"schema":   map[string]any{"type": "string", "description": "Schema namespace (usually public)"},
					"table":    map[string]any{"type": "string"},
					"database": map[string]any{"type": "string", "description": "Optional other catalog to inspect (temporary; does not change workbench DB)"},
				},
				"required": []string{"table"},
			}),
			fn("explain_plan", "Run EXPLAIN on a SQL query (ANALYZE is blocked in read-only mode).", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			}),
		)
	}
	return append(common,
		fn("list_schemas", "List MySQL databases (schemas).", map[string]any{"type": "object", "properties": map[string]any{}}),
		fn("list_tables", "List tables in a database/schema.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"schema": map[string]any{"type": "string"},
			},
		}),
		fn("describe_table", "Describe columns of a table.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"schema": map[string]any{"type": "string"},
				"table":  map[string]any{"type": "string"},
			},
			"required": []string{"table"},
		}),
		fn("explain_plan", "Run EXPLAIN on a SQL query (ANALYZE is blocked in read-only mode).", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		}),
	)
}

func (t *ToolRunner) executeQueryToolDesc() string {
	switch t.connType {
	case config.ConnRedis:
		return "Execute a Redis command on the current connection. Respect read-only mode."
	case config.ConnMongo:
		return "Execute a MongoDB command (mongosh-lite, e.g. db.coll.find({})) on the current connection. Respect read-only mode."
	default:
		return "Execute a SQL statement on the current connection. Respect read-only mode."
	}
}

func (t *ToolRunner) executeQueryArgDesc() string {
	switch t.connType {
	case config.ConnRedis:
		return "Redis command"
	case config.ConnMongo:
		return "MongoDB command"
	default:
		return "SQL statement"
	}
}

func fn(name, desc string, params map[string]any) ToolSpec {
	return ToolSpec{
		Type: "function",
		Function: ToolFunctionSchema{
			Name:        name,
			Description: desc,
			Parameters:  params,
		},
	}
}

// Run executes a named tool with JSON arguments.
func (t *ToolRunner) Run(ctx context.Context, name, argsJSON string) (string, error) {
	var args map[string]any
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	if args == nil {
		args = map[string]any{}
	}

	drv, release, err := t.mgr.Acquire(t.connID)
	if err != nil {
		return "", err
	}
	defer release()

	switch name {
	case "execute_query":
		q, _ := args["query"].(string)
		res, err := drv.Execute(ctx, q, t.maxRows, t.allowWrite)
		if err != nil {
			return "", err
		}
		return mustJSON(res), nil
	case "list_schemas":
		schemas, err := drv.ListSchemas(ctx)
		if err != nil {
			return "", err
		}
		return mustJSON(schemas), nil
	case "list_tables":
		schema, _ := args["schema"].(string)
		if strings.TrimSpace(schema) == "" && t.connType == config.ConnPostgres {
			schema = "public"
		}
		dbName, _ := args["database"].(string)
		tables, err := listTablesMaybeCatalog(ctx, drv, strings.TrimSpace(dbName), schema)
		if err != nil {
			return "", err
		}
		return mustJSON(tables), nil
	case "describe_table":
		schema, _ := args["schema"].(string)
		if strings.TrimSpace(schema) == "" && t.connType == config.ConnPostgres {
			schema = "public"
		}
		table, _ := args["table"].(string)
		dbName, _ := args["database"].(string)
		res, err := describeTableMaybeCatalog(ctx, drv, strings.TrimSpace(dbName), schema, table)
		if err != nil {
			return "", err
		}
		return mustJSON(res), nil
	case "explain_plan":
		q, _ := args["query"].(string)
		res, err := drv.Explain(ctx, q)
		if err != nil {
			return "", err
		}
		return mustJSON(res), nil
	case "redis_keys":
		pattern, _ := args["pattern"].(string)
		limit := int64(100)
		if v, ok := args["limit"].(float64); ok {
			limit = int64(v)
		}
		keys, err := redisKeys(ctx, drv, pattern, limit)
		if err != nil {
			return "", err
		}
		return mustJSON(keys), nil
	case "redis_type":
		key, _ := args["key"].(string)
		typ, err := redisType(ctx, drv, key)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]string{"key": key, "type": typ}), nil
	case "redis_get":
		key, _ := args["key"].(string)
		val, err := redisGet(ctx, drv, key)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]string{"key": key, "value": val}), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func listTablesMaybeCatalog(ctx context.Context, drv db.Driver, database, schema string) ([]string, error) {
	if database != "" {
		type catalogLister interface {
			ListTablesInDatabase(ctx context.Context, database, schema string) ([]string, error)
		}
		if c, ok := drv.(catalogLister); ok {
			return c.ListTablesInDatabase(ctx, database, schema)
		}
	}
	return drv.ListTables(ctx, schema)
}

func describeTableMaybeCatalog(ctx context.Context, drv db.Driver, database, schema, table string) (*db.QueryResult, error) {
	if database != "" {
		type catalogDescriber interface {
			DescribeTableInDatabase(ctx context.Context, database, schema, table string) (*db.QueryResult, error)
		}
		if c, ok := drv.(catalogDescriber); ok {
			return c.DescribeTableInDatabase(ctx, database, schema, table)
		}
	}
	return drv.DescribeTable(ctx, schema, table)
}

func redisKeys(ctx context.Context, drv db.Driver, pattern string, limit int64) ([]string, error) {
	type keyScanner interface {
		RedisKeys(ctx context.Context, pattern string, limit int64) ([]string, error)
	}
	if k, ok := drv.(keyScanner); ok {
		return k.RedisKeys(ctx, pattern, limit)
	}
	return drv.ListTables(ctx, "")
}

func redisType(ctx context.Context, drv db.Driver, key string) (string, error) {
	type typer interface {
		RedisType(ctx context.Context, key string) (string, error)
	}
	if t, ok := drv.(typer); ok {
		return t.RedisType(ctx, key)
	}
	res, err := drv.DescribeTable(ctx, "", key)
	if err != nil {
		return "", err
	}
	if res.RowCount > 0 && len(res.Rows[0]) > 1 {
		return res.Rows[0][1], nil
	}
	return "", fmt.Errorf("type unavailable")
}

func redisGet(ctx context.Context, drv db.Driver, key string) (string, error) {
	type getter interface {
		RedisGet(ctx context.Context, key string) (string, error)
	}
	if g, ok := drv.(getter); ok {
		return g.RedisGet(ctx, key)
	}
	res, err := drv.Execute(ctx, "GET "+key, 1, false)
	if err != nil {
		return "", err
	}
	if res.RowCount > 0 && len(res.Rows[0]) > 0 {
		return res.Rows[0][0], nil
	}
	return "", nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}
