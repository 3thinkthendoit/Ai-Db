package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/config"

	"github.com/go-sql-driver/mysql"
)

type mysqlDriver struct {
	db           *sql.DB
	cfg          config.Connection
	mu           sync.Mutex
	activeSchema string
}

func openMySQL(ctx context.Context, c config.Connection) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 3306
	}
	cfg := mysql.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", c.Host, port)
	cfg.DBName = c.Database
	cfg.Params = map[string]string{
		"charset": "utf8mb4",
		"timeout": "8s",
		"readTimeout": "60s",
		"writeTimeout": "60s",
	}
	cfg.ParseTime = true
	dbx, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	dbx.SetMaxOpenConns(8)
	dbx.SetMaxIdleConns(2)
	dbx.SetConnMaxLifetime(30 * time.Minute)
	d := &mysqlDriver{db: dbx, cfg: c}
	if err := d.Ping(ctx); err != nil {
		_ = dbx.Close()
		return nil, err
	}
	return d, nil
}

func (d *mysqlDriver) Type() config.ConnType { return config.ConnMySQL }

func (d *mysqlDriver) Ping(ctx context.Context) error { return pingSQL(ctx, d.db) }

func (d *mysqlDriver) Close() error { return d.db.Close() }

func (d *mysqlDriver) SetActiveSchema(schema string) {
	d.mu.Lock()
	d.activeSchema = strings.TrimSpace(schema)
	d.mu.Unlock()
}

func (d *mysqlDriver) sessionSQL() string {
	d.mu.Lock()
	s := d.activeSchema
	d.mu.Unlock()
	if s == "" {
		return ""
	}
	return "USE " + quoteMySQLIdent(s)
}

func (d *mysqlDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	return execSQLWithSession(ctx, d.db, d.sessionSQL(), query, maxRows, allowWrites)
}

func (d *mysqlDriver) ListSchemas(ctx context.Context) ([]string, error) {
	res, err := execSQL(ctx, d.db, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME", 1000, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) > 0 {
			out = append(out, r[0])
		}
	}
	return out, nil
}

func (d *mysqlDriver) ListTables(ctx context.Context, schema string) ([]string, error) {
	if schema == "" {
		schema = d.cfg.Database
	}
	q := fmt.Sprintf("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = '%s' ORDER BY TABLE_NAME",
		escapeSQLLiteral(schema))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) > 0 {
			out = append(out, r[0])
		}
	}
	return out, nil
}

func (d *mysqlDriver) DescribeTable(ctx context.Context, schema, table string) (*QueryResult, error) {
	if schema == "" {
		schema = d.cfg.Database
	}
	q := fmt.Sprintf(
		"SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA "+
			"FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s' ORDER BY ORDINAL_POSITION",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	return execSQL(ctx, d.db, q, 2000, false)
}

func (d *mysqlDriver) Explain(ctx context.Context, query string) (*QueryResult, error) {
	if err := EnsureExplainReadOnly(query); err != nil {
		return nil, err
	}
	return execSQLWithSession(ctx, d.db, d.sessionSQL(), "EXPLAIN "+query, 500, false)
}

func quoteMySQLIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (d *mysqlDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	schemas, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]ObjectNode, 0, len(schemas))
	for _, s := range schemas {
		nodes = append(nodes, ObjectNode{Name: s, Kind: "schema", Lazy: true})
	}
	return nodes, nil
}

func (d *mysqlDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	schema := path[0]
	if len(path) == 1 {
		return mysqlSchemaFolders(), nil
	}
	cat := path[1]
	switch cat {
	case CatTables, CatViews:
		if len(path) == 2 {
			kind := "table"
			tableType := "BASE TABLE"
			if cat == CatViews {
				kind = "view"
				tableType = "VIEW"
			}
			names, err := d.listTablesOfType(ctx, schema, tableType)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, kind, true), nil
		}
		if len(path) == 3 {
			return tableDetailFolders(), nil
		}
		if len(path) == 4 {
			table := path[2]
			switch path[3] {
			case CatColumns:
				return d.listColumnNodes(ctx, schema, table)
			case CatIndexes:
				return d.listIndexNodes(ctx, schema, table)
			}
		}
	case CatProcedures:
		if len(path) == 2 {
			names, err := d.listRoutines(ctx, schema, "PROCEDURE")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "procedure", false), nil
		}
	case CatFunctions:
		if len(path) == 2 {
			names, err := d.listRoutines(ctx, schema, "FUNCTION")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "function", false), nil
		}
	case CatTriggers:
		if len(path) == 2 {
			names, err := d.listTriggers(ctx, schema)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "trigger", false), nil
		}
	case CatEvents:
		if len(path) == 2 {
			names, err := d.listEvents(ctx, schema)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "event", false), nil
		}
	}
	return nil, badObjectPath(path)
}

func (d *mysqlDriver) listTablesOfType(ctx context.Context, schema, tableType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = '%s' AND TABLE_TYPE = '%s' ORDER BY TABLE_NAME",
		escapeSQLLiteral(schema), escapeSQLLiteral(tableType))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *mysqlDriver) listRoutines(ctx context.Context, schema, routineType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT ROUTINE_NAME FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = '%s' AND ROUTINE_TYPE = '%s' ORDER BY ROUTINE_NAME",
		escapeSQLLiteral(schema), escapeSQLLiteral(routineType))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *mysqlDriver) listTriggers(ctx context.Context, schema string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = '%s' ORDER BY TRIGGER_NAME",
		escapeSQLLiteral(schema))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *mysqlDriver) listEvents(ctx context.Context, schema string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT EVENT_NAME FROM information_schema.EVENTS WHERE EVENT_SCHEMA = '%s' ORDER BY EVENT_NAME",
		escapeSQLLiteral(schema))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *mysqlDriver) listColumnNodes(ctx context.Context, schema, table string) ([]ObjectNode, error) {
	q := fmt.Sprintf(
		"SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_KEY FROM information_schema.COLUMNS "+
			"WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s' ORDER BY ORDINAL_POSITION",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	res, err := execSQL(ctx, d.db, q, 2000, false)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectNode, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) == 0 {
			continue
		}
		label := r[0]
		if len(r) > 1 && r[1] != "" {
			label = r[0] + " · " + r[1]
		}
		n := ObjectNode{Name: label, Kind: "column"}
		if len(r) > 2 && strings.EqualFold(r[2], "PRI") {
			n.Badge = "PK"
		}
		out = append(out, n)
	}
	return out, nil
}

func (d *mysqlDriver) listIndexNodes(ctx context.Context, schema, table string) ([]ObjectNode, error) {
	q := fmt.Sprintf(
		"SELECT INDEX_NAME, NON_UNIQUE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) AS cols "+
			"FROM information_schema.STATISTICS WHERE TABLE_SCHEMA='%s' AND TABLE_NAME='%s' "+
			"GROUP BY INDEX_NAME, NON_UNIQUE ORDER BY INDEX_NAME",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	res, err := execSQL(ctx, d.db, q, 2000, false)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectNode, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) == 0 {
			continue
		}
		label := r[0]
		if len(r) > 2 && r[2] != "" {
			label = r[0] + " (" + r[2] + ")"
		}
		n := ObjectNode{Name: label, Kind: "index"}
		if strings.EqualFold(r[0], "PRIMARY") {
			n.Badge = "PK"
		} else if len(r) > 1 && (r[1] == "0" || strings.EqualFold(r[1], "false")) {
			n.Badge = "UNI"
		}
		out = append(out, n)
	}
	return out, nil
}

func escapeSQLLiteral(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}
