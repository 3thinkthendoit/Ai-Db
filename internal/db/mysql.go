package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"AI-DB/internal/config"

	"github.com/go-sql-driver/mysql"
)

type mysqlDriver struct {
	db  *sql.DB
	cfg config.Connection
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

func (d *mysqlDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	return execSQL(ctx, d.db, query, maxRows, allowWrites)
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
	return execSQL(ctx, d.db, "EXPLAIN "+query, 500, false)
}

func (d *mysqlDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	schemas, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	// Prefer current database if set.
	if d.cfg.Database != "" {
		tables, err := d.ListTables(ctx, d.cfg.Database)
		if err != nil {
			return nil, err
		}
		children := make([]ObjectNode, 0, len(tables))
		for _, t := range tables {
			children = append(children, ObjectNode{Name: t, Kind: "table"})
		}
		return []ObjectNode{{Name: d.cfg.Database, Kind: "schema", Children: children}}, nil
	}
	nodes := make([]ObjectNode, 0, len(schemas))
	for _, s := range schemas {
		nodes = append(nodes, ObjectNode{Name: s, Kind: "schema"})
	}
	return nodes, nil
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
