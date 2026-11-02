package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"AI-DB/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgresDriver struct {
	db *sql.DB
	cfg config.Connection
	mu sync.Mutex
	// activeDB is the PostgreSQL database currently open on db (UI「当前库」).
	activeDB string
}

func openPostgres(ctx context.Context, c config.Connection) (Driver, error) {
	dbName := strings.TrimSpace(c.Database)
	if dbName == "" {
		dbName = "postgres"
	}
	dbx, err := openPostgresPool(c, dbName)
	if err != nil {
		return nil, err
	}
	d := &postgresDriver{db: dbx, cfg: c, activeDB: dbName}
	if err := d.Ping(ctx); err != nil {
		_ = dbx.Close()
		return nil, err
	}
	return d, nil
}

func openPostgresPool(c config.Connection, dbName string) (*sql.DB, error) {
	port := c.Port
	if port == 0 {
		port = 5432
	}
	q := url.Values{}
	q.Set("sslmode", postgresSSLMode(c.SSLMode))
	q.Set("connect_timeout", "8")
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Host, port),
		Path:     "/" + dbName,
		RawQuery: q.Encode(),
	}
	dbx, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	dbx.SetMaxOpenConns(8)
	dbx.SetMaxIdleConns(2)
	dbx.SetConnMaxLifetime(30 * time.Minute)
	return dbx, nil
}

// postgresSSLMode maps a connection profile value to a libpq sslmode.
// Empty / unknown → prefer (try TLS first; needed for cloud PG that reject "no encryption").
func postgresSSLMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "prefer"
	}
}

func (d *postgresDriver) Type() config.ConnType { return config.ConnPostgres }

func (d *postgresDriver) pool() *sql.DB {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db
}

func (d *postgresDriver) Ping(ctx context.Context) error { return pingSQL(ctx, d.pool()) }

func (d *postgresDriver) Close() error {
	d.mu.Lock()
	dbx := d.db
	d.db = nil
	d.mu.Unlock()
	if dbx == nil {
		return nil
	}
	return dbx.Close()
}

// SetActiveSchema switches the open pool to another PostgreSQL database (MySQL-style「当前库」).
func (d *postgresDriver) SetActiveSchema(schema string) error {
	name := strings.TrimSpace(schema)
	if name == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return d.switchDB(ctx, name)
}

// switchDB replaces the live pool. Callers (Manager) must ensure no in-flight queries.
func (d *postgresDriver) switchDB(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	d.mu.Lock()
	if d.activeDB == name && d.db != nil {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	dbx, err := openPostgresPool(d.cfg, name)
	if err != nil {
		return err
	}
	if err := pingSQL(ctx, dbx); err != nil {
		_ = dbx.Close()
		return fmt.Errorf("switch database %q: %w", name, err)
	}

	d.mu.Lock()
	old := d.db
	d.db = dbx
	d.activeDB = name
	d.cfg.Database = name
	d.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// withDB runs fn against the live pool when name matches activeDB; otherwise a temporary pool
// (object-tree browse must not silently change where Execute runs).
func (d *postgresDriver) withDB(ctx context.Context, name string, fn func(*sql.DB) error) error {
	name = strings.TrimSpace(name)
	d.mu.Lock()
	active := d.activeDB
	live := d.db
	d.mu.Unlock()
	if name == "" || name == active {
		if live == nil {
			return fmt.Errorf("postgres pool not open")
		}
		return fn(live)
	}
	tmp, err := openPostgresPool(d.cfg, name)
	if err != nil {
		return err
	}
	defer tmp.Close()
	if err := pingSQL(ctx, tmp); err != nil {
		return fmt.Errorf("open database %q: %w", name, err)
	}
	return fn(tmp)
}

func (d *postgresDriver) Execute(ctx context.Context, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	return execSQL(ctx, d.pool(), query, maxRows, allowWrites)
}

// ExecuteDMLSnapshot runs rollbackable DML with a before-image (Postgres).
func (d *postgresDriver) ExecuteDMLSnapshot(ctx context.Context, query string) (*QueryResult, *RowSnapshot, error) {
	return ExecuteDMLWithSnapshot(ctx, d.pool(), "", query, "postgres")
}

// ExecuteRollbackSnapshot applies compensating SQL from a snapshot (Postgres).
func (d *postgresDriver) ExecuteRollbackSnapshot(ctx context.Context, snap *RowSnapshot) (*QueryResult, error) {
	stmts, err := BuildRollbackSQL(snap)
	if err != nil {
		return nil, err
	}
	return ExecuteRollbackSQL(ctx, d.pool(), "", stmts)
}

// ListSchemas lists PostgreSQL databases (UI / agent 「库」), matching MySQL semantics.
func (d *postgresDriver) ListSchemas(ctx context.Context) ([]string, error) {
	res, err := execSQL(ctx, d.pool(),
		"SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname",
		1000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func listNamespaceSchemasOn(ctx context.Context, dbx *sql.DB) ([]string, error) {
	res, err := execSQL(ctx, dbx,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog','information_schema','pg_toast') ORDER BY schema_name",
		1000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func (d *postgresDriver) ListTables(ctx context.Context, schema string) ([]string, error) {
	return d.ListTablesInDatabase(ctx, "", schema)
}

func (d *postgresDriver) DescribeTable(ctx context.Context, schema, table string) (*QueryResult, error) {
	return d.DescribeTableInDatabase(ctx, "", schema, table)
}

// ListTablesInDatabase lists base tables in schema. Empty database → live pool;
// otherwise a temporary pool (Agent database= must not permanently switch catalog).
func (d *postgresDriver) ListTablesInDatabase(ctx context.Context, database, schema string) ([]string, error) {
	if schema == "" {
		schema = "public"
	}
	q := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = 'BASE TABLE' ORDER BY table_name",
		escapeSQLLiteral(schema))
	var out []string
	err := d.withDB(ctx, strings.TrimSpace(database), func(dbx *sql.DB) error {
		res, err := execSQL(ctx, dbx, q, 5000, false)
		if err != nil {
			return err
		}
		out = rowsFirstCol(res)
		return nil
	})
	return out, err
}

// DescribeTableInDatabase describes a table. Empty database → live pool.
func (d *postgresDriver) DescribeTableInDatabase(ctx context.Context, database, schema, table string) (*QueryResult, error) {
	if schema == "" {
		schema = "public"
	}
	q := fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, column_default "+
			"FROM information_schema.columns WHERE table_schema='%s' AND table_name='%s' ORDER BY ordinal_position",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	var out *QueryResult
	err := d.withDB(ctx, strings.TrimSpace(database), func(dbx *sql.DB) error {
		res, err := execSQL(ctx, dbx, q, 2000, false)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

func (d *postgresDriver) Explain(ctx context.Context, query string) (*QueryResult, error) {
	if err := EnsureExplainReadOnly(query); err != nil {
		return nil, err
	}
	return execSQL(ctx, d.pool(), "EXPLAIN "+query, 500, false)
}

func quotePGIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (d *postgresDriver) ListObjects(ctx context.Context) ([]ObjectNode, error) {
	dbs, err := d.ListSchemas(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]ObjectNode, 0, len(dbs))
	for _, name := range dbs {
		nodes = append(nodes, ObjectNode{Name: name, Kind: "database", Lazy: true})
	}
	return nodes, nil
}

// ExpandObject paths: [database] → schemas; [database, schema] → folders;
// [database, schema, tables|…] → objects (MySQL-like catalog at the top).
// Browsing a non-active database uses a temporary pool and does not change Execute target.
func (d *postgresDriver) ExpandObject(ctx context.Context, path []string) ([]ObjectNode, error) {
	if len(path) == 0 {
		return d.ListObjects(ctx)
	}
	// Schema folder list is static — no connection required.
	if len(path) == 2 {
		return postgresSchemaFolders(), nil
	}
	var out []ObjectNode
	err := d.withDB(ctx, path[0], func(dbx *sql.DB) error {
		nodes, e := expandPostgresOn(ctx, dbx, path)
		out = nodes
		return e
	})
	return out, err
}

func expandPostgresOn(ctx context.Context, dbx *sql.DB, path []string) ([]ObjectNode, error) {
	if len(path) == 1 {
		schemas, err := listNamespaceSchemasOn(ctx, dbx)
		if err != nil {
			return nil, err
		}
		return namesToNodes(schemas, "schema", true), nil
	}
	if len(path) == 2 {
		return postgresSchemaFolders(), nil
	}
	schema := path[1]
	cat := path[2]
	switch cat {
	case CatTables, CatViews:
		if len(path) == 3 {
			kind := "table"
			tableType := "BASE TABLE"
			if cat == CatViews {
				kind = "view"
				tableType = "VIEW"
			}
			names, err := listTablesOfTypeOn(ctx, dbx, schema, tableType)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, kind, true), nil
		}
		if len(path) == 4 {
			return tableDetailFolders(), nil
		}
		if len(path) == 5 {
			table := path[3]
			switch path[4] {
			case CatColumns:
				return listColumnNodesOn(ctx, dbx, schema, table)
			case CatIndexes:
				return listIndexNodesOn(ctx, dbx, schema, table)
			}
		}
	case CatFunctions:
		if len(path) == 3 {
			names, err := listRoutinesOn(ctx, dbx, schema, "FUNCTION")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "function", false), nil
		}
	case CatProcedures:
		if len(path) == 3 {
			names, err := listRoutinesOn(ctx, dbx, schema, "PROCEDURE")
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "procedure", false), nil
		}
	case CatSequences:
		if len(path) == 3 {
			names, err := listSequencesOn(ctx, dbx, schema)
			if err != nil {
				return nil, err
			}
			return namesToNodes(names, "sequence", false), nil
		}
	}
	return nil, badObjectPath(path)
}

func listTablesOfTypeOn(ctx context.Context, dbx *sql.DB, schema, tableType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = '%s' ORDER BY table_name",
		escapeSQLLiteral(schema), escapeSQLLiteral(tableType))
	res, err := execSQL(ctx, dbx, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func listRoutinesOn(ctx context.Context, dbx *sql.DB, schema, routineType string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT routine_name FROM information_schema.routines WHERE specific_schema = '%s' AND routine_type = '%s' ORDER BY routine_name",
		escapeSQLLiteral(schema), escapeSQLLiteral(routineType))
	res, err := execSQL(ctx, dbx, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func listSequencesOn(ctx context.Context, dbx *sql.DB, schema string) ([]string, error) {
	q := fmt.Sprintf(
		"SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = '%s' ORDER BY sequence_name",
		escapeSQLLiteral(schema))
	res, err := execSQL(ctx, dbx, q, 5000, false)
	if err != nil {
		return nil, err
	}
	return rowsFirstCol(res), nil
}

func listColumnNodesOn(ctx context.Context, dbx *sql.DB, schema, table string) ([]ObjectNode, error) {
	// EXISTS avoids duplicate rows when a column participates in multiple constraints.
	q := fmt.Sprintf(
		"SELECT c.column_name, c.data_type, "+
			"CASE WHEN EXISTS ("+
			"  SELECT 1 FROM information_schema.table_constraints tc "+
			"  JOIN information_schema.key_column_usage kcu "+
			"    ON kcu.constraint_schema = tc.constraint_schema "+
			"   AND kcu.constraint_name = tc.constraint_name "+
			"   AND kcu.table_schema = tc.table_schema "+
			"   AND kcu.table_name = tc.table_name "+
			"  WHERE tc.constraint_type = 'PRIMARY KEY' "+
			"    AND tc.table_schema = c.table_schema AND tc.table_name = c.table_name "+
			"    AND kcu.column_name = c.column_name"+
			") THEN 'PRI' ELSE '' END AS column_key "+
			"FROM information_schema.columns c "+
			"WHERE c.table_schema='%s' AND c.table_name='%s' "+
			"ORDER BY c.ordinal_position",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	res, err := execSQL(ctx, dbx, q, 2000, false)
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

func listIndexNodesOn(ctx context.Context, dbx *sql.DB, schema, table string) ([]ObjectNode, error) {
	// Match MySQL richness: name (cols) + PK/UNI badge.
	// Extra indexdef column covers expression indexes (no plain attname cols).
	q := fmt.Sprintf(
		"SELECT ci.relname AS index_name, "+
			"CASE WHEN i.indisprimary THEN 'PK' WHEN i.indisunique THEN 'UNI' ELSE '' END AS badge, "+
			"coalesce(("+
			"  SELECT string_agg(a.attname, ', ' ORDER BY k.ord) "+
			"  FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) "+
			"  JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum "+
			"  WHERE k.attnum > 0"+
			"), '') AS cols, "+
			"pg_get_indexdef(i.indexrelid) AS indexdef "+
			"FROM pg_index i "+
			"JOIN pg_class ct ON ct.oid = i.indrelid "+
			"JOIN pg_namespace n ON n.oid = ct.relnamespace "+
			"JOIN pg_class ci ON ci.oid = i.indexrelid "+
			"WHERE n.nspname = '%s' AND ct.relname = '%s' "+
			"ORDER BY ci.relname",
		escapeSQLLiteral(schema), escapeSQLLiteral(table))
	res, err := execSQL(ctx, dbx, q, 2000, false)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectNode, 0, res.RowCount)
	for _, r := range res.Rows {
		if len(r) == 0 {
			continue
		}
		label := r[0]
		cols := ""
		if len(r) > 2 {
			cols = r[2]
		}
		if cols == "" && len(r) > 3 {
			cols = shortenIndexDef(r[3])
		}
		if cols != "" {
			label = r[0] + " (" + cols + ")"
		}
		n := ObjectNode{Name: label, Kind: "index"}
		if len(r) > 1 && r[1] != "" {
			n.Badge = r[1]
		}
		out = append(out, n)
	}
	return out, nil
}

// shortenIndexDef extracts the key/expression list from CREATE INDEX … (…).
func shortenIndexDef(def string) string {
	def = strings.TrimSpace(def)
	if def == "" {
		return ""
	}
	upper := strings.ToUpper(def)
	startSearch := 0
	if on := strings.Index(upper, " ON "); on >= 0 {
		startSearch = on
	}
	openRel := strings.Index(def[startSearch:], "(")
	if openRel < 0 {
		if len(def) > 80 {
			return def[:77] + "…"
		}
		return def
	}
	open := startSearch + openRel
	depth := 0
	for j := open; j < len(def); j++ {
		switch def[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner := strings.TrimSpace(def[open+1 : j])
				if len(inner) > 80 {
					return inner[:77] + "…"
				}
				return inner
			}
		}
	}
	if len(def) > 80 {
		return def[:77] + "…"
	}
	return def
}
