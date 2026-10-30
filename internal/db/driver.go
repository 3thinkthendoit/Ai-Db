package db

import (
	"context"
	"fmt"
	"time"

	"AI-DB/internal/config"
)

// QueryResult is a tabular result returned to the UI / agent.
type QueryResult struct {
	Columns    []string   `json:"columns"`
	Rows       [][]string `json:"rows"`
	RowCount   int        `json:"rowCount"`
	DurationMs int64      `json:"durationMs"`
	Message    string     `json:"message"`
	Truncated  bool       `json:"truncated"`
}

// ObjectNode is a lightweight browser node (schema/table/key/folder).
// Lazy=true means children are loaded on demand via ExpandObject.
type ObjectNode struct {
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	Lazy     bool         `json:"lazy,omitempty"`
	Children []ObjectNode `json:"children,omitempty"`
}

// Driver is the unified database access interface.
type Driver interface {
	Ping(ctx context.Context) error
	Close() error
	Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error)
	ListObjects(ctx context.Context) ([]ObjectNode, error)
	// ExpandObject loads children for a tree path, e.g.
	// [schema] → category folders; [schema,"tables"] → tables;
	// [schema,"tables",table] → columns/indexes folders;
	// [schema,"tables",table,"indexes"] → index leaves.
	ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error)
	ListSchemas(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context, schema string) ([]string, error)
	DescribeTable(ctx context.Context, schema, table string) (*QueryResult, error)
	Explain(ctx context.Context, query string) (*QueryResult, error)
	Type() config.ConnType
}

// Open creates a driver for the given connection profile.
func Open(ctx context.Context, c config.Connection) (Driver, error) {
	switch c.Type {
	case config.ConnMySQL:
		return openMySQL(ctx, c)
	case config.ConnPostgres:
		return openPostgres(ctx, c)
	case config.ConnRedis:
		return openRedis(ctx, c)
	default:
		return nil, fmt.Errorf("unsupported connection type: %s", c.Type)
	}
}

func timed(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

func stringify(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}
