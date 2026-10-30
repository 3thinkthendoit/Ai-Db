package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"AI-DB/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgresDriver struct {
	db  *sql.DB
	cfg config.Connection
}

func openPostgres(ctx context.Context, c config.Connection) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 5432
	}
	dbName := c.Database
	if dbName == "" {
		dbName = "postgres"
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, port),
		Path:   "/" + dbName,
		RawQuery: "sslmode=disable&connect_timeout=8",
	}
	dsn := u.String()
	dbx, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	dbx.SetMaxOpenConns(8)
	dbx.SetMaxIdleConns(2)
	dbx.SetConnMaxLifetime(30 * time.Minute)
	d := &postgresDriver{db: dbx, cfg: c}
	if err := d.Ping(ctx); err != nil {
		_ = dbx.Close()
		return nil, err
	}
	return d, nil
}

func (d *postgresDriver) Type() config.ConnType { return config.ConnPostgres }

func (d *postgresDriver) Ping(ctx context.Context) error { return pingSQL(ctx, d.db) }

func (d *postgresDriver) Close() error { return d.db.Close() }

func (d *postgresDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	return execSQL(ctx, d.db, query, maxRows, allowWrites)
}

func (d *postgresDriver) ListSchemas(ctx context.Context) ([]string, error) {
	res, err := execSQL(ctx, d.db,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog','information_schema','pg_toast') ORDER BY schema_name",
		1000, false)
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

func (d *postgresDriver) ListTables(ctx context.Context, schema string) ([]string, error) {
	if schema == "" {
		schema = "public"
	}
	q := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = 'BASE TABLE' ORDER BY table_name",
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

func (d *postgresDriver) DescribeTable(ctx context.Context, schema, table string) (*QueryResult, error) {
	if schema == "" {
		schema = "public"
	}
	q := fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, column_default "+
			"FROM information_schema.columns WHERE table_schema='%s' AND table_name='%s' ORDER BY ordinal_position",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	return execSQL(ctx, d.db, q, 2000, false)
}

func (d *postgresDriver) Explain(ctx context.Context, query string) (*QueryResult, error) {
	if err := EnsureExplainReadOnly(query); err != nil {
		return nil, err
	}
	return execSQL(ctx, d.db, "EXPLAIN "+query, 500, false)
}

func (d *postgresDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	schemas, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]ObjectNode, 0, len(schemas))
	for _, s := range schemas {
		tables, err := d.ListTables(ctx, s)
		if err != nil {
			return nil, err
		}
		children := make([]ObjectNode, 0, len(tables))
		for _, t := range tables {
			children = append(children, ObjectNode{Name: t, Kind: "table"})
		}
		nodes = append(nodes, ObjectNode{Name: s, Kind: "schema", Children: children})
	}
	return nodes, nil
}
