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
		nodes = append(nodes, ObjectNode{Name: s, Kind: "schema", Lazy: true})
	}
	return nodes, nil
}

func (d *postgresDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	schema := path[0]
	if len(path) == 1 {
		return postgresSchemaFolders(), nil
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
	case CatFunctions:
		if len(path) == 2 {
			names, err := d.listRoutines(ctx, schema, "FUNCTION")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "function", false), nil
		}
	case CatProcedures:
		if len(path) == 2 {
			names, err := d.listRoutines(ctx, schema, "PROCEDURE")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "procedure", false), nil
		}
	case CatSequences:
		if len(path) == 2 {
			names, err := d.listSequences(ctx, schema)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "sequence", false), nil
		}
	}
	return nil, badObjectPath(path)
}

func (d *postgresDriver) listTablesOfType(ctx context.Context, schema, tableType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = '%s' ORDER BY table_name",
		escapeSQLLiteral(schema), escapeSQLLiteral(tableType))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *postgresDriver) listRoutines(ctx context.Context, schema, routineType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT routine_name FROM information_schema.routines WHERE specific_schema = '%s' AND routine_type = '%s' ORDER BY routine_name",
		escapeSQLLiteral(schema), escapeSQLLiteral(routineType))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *postgresDriver) listSequences(ctx context.Context, schema string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = '%s' ORDER BY sequence_name",
		escapeSQLLiteral(schema))
	res, err := execSQL(ctx, d.db, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *postgresDriver) listColumnNodes(ctx context.Context, schema, table string) ([]ObjectNode, error) {
	q := fmt.Sprintf(
		"SELECT column_name, data_type FROM information_schema.columns "+
			"WHERE table_schema='%s' AND table_name='%s' ORDER BY ordinal_position",
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
		out = append(out, ObjectNode{Name: label, Kind: "column"})
	}
	return out, nil
}

func (d *postgresDriver) listIndexNodes(ctx context.Context, schema, table string) ([]ObjectNode, error) {
	q := fmt.Sprintf(
		"SELECT indexname FROM pg_indexes WHERE schemaname='%s' AND tablename='%s' ORDER BY indexname",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	res, err := execSQL(ctx, d.db, q, 2000, false)
	if err != nil {
		return nil, err
	}
	return namesToNodes(rowsFirstCol(res), "index", false), nil
}
