package agent

import (
	"context"
	"encoding/json"
	"fmt"

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
		fn("execute_query", "Execute a SQL statement or Redis command on the current connection. Respect read-only mode.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "SQL or Redis command"},
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
	return append(common,
		fn("list_schemas", "List database schemas.", map[string]any{"type": "object", "properties": map[string]any{}}),
		fn("list_tables", "List tables in a schema.", map[string]any{
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
	drv, release, err := t.mgr.Acquire(t.connID)
	if err != nil {
		return "", err
	}
	defer release()

	var args map[string]any
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	if args == nil {
		args = map[string]any{}
	}

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
		tables, err := drv.ListTables(ctx, schema)
		if err != nil {
			return "", err
		}
		return mustJSON(tables), nil
	case "describe_table":
		schema, _ := args["schema"].(string)
		table, _ := args["table"].(string)
		res, err := drv.DescribeTable(ctx, schema, table)
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
