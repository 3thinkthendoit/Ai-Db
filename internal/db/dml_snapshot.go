package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxSnapshotRows is the hard cap for before-image capture. Beyond this the
// mutation is refused so the rollback promise stays honest.
const MaxSnapshotRows = 1000

// RowSnapshot is the durable before-image (+ metadata) used to build compensating SQL.
type RowSnapshot struct {
	Kind       string     `json:"kind"` // insert | update | delete
	Table      string     `json:"table"`
	QuoteStyle string     `json:"quoteStyle"` // mysql | postgres
	Columns    []string   `json:"columns,omitempty"`
	PKColumns  []string   `json:"pkColumns,omitempty"`
	Rows       [][]string `json:"rows,omitempty"`       // before-image (update/delete) or inserted values (insert)
	WhereSQL   string     `json:"whereSQL,omitempty"`   // original WHERE for update/delete (diagnostic)
}

// ParsedDML is a narrowly supported single-table DML statement.
type ParsedDML struct {
	Kind     string // insert | update | delete
	Table    string
	WhereSQL string // including leading WHERE, or empty
	Raw      string
}

// ParseSimpleDML accepts a single INSERT/UPDATE/DELETE on one table.
// INSERT…SELECT, multi-statement, and JOIN forms are rejected.
// Case of identifiers is preserved (unlike classify normalizeSQL).
func ParseSimpleDML(query string) (*ParsedDML, error) {
	cleaned := strings.Join(strings.Fields(stripSQLComments(strings.TrimSpace(query))), " ")
	stmts := splitStatements(cleaned)
	var one string
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s != "" {
			if one != "" {
				return nil, fmt.Errorf("only a single statement is supported for rollbackable DML")
			}
			one = s
		}
	}
	if one == "" {
		return nil, fmt.Errorf("empty statement")
	}
	one = strings.TrimSpace(stripLeadingWithCTEs(one))
	first := firstKeyword(strings.ToUpper(one))
	switch first {
	case "DELETE":
		return parseDelete(one)
	case "UPDATE":
		return parseUpdate(one)
	case "INSERT", "REPLACE":
		return parseInsert(one)
	default:
		return nil, fmt.Errorf("unsupported DML kind %s", first)
	}
}

func kw(s string) string { return strings.ToUpper(firstKeyword(s)) }

func parseDelete(one string) (*ParsedDML, error) {
	// DELETE FROM t WHERE ...
	rest := strings.TrimSpace(one[len(firstKeyword(one)):])
	if kw(rest) != "FROM" {
		return nil, fmt.Errorf("DELETE must be DELETE FROM table")
	}
	rest = strings.TrimSpace(rest[len(firstKeyword(rest)):])
	table, after, err := readSQLIdent(rest)
	if err != nil {
		return nil, err
	}
	after = strings.TrimSpace(after)
	where := ""
	if after != "" {
		if kw(after) != "WHERE" {
			return nil, fmt.Errorf("DELETE with JOIN/USING/ORDER is not rollbackable")
		}
		where = after
	}
	return &ParsedDML{Kind: "delete", Table: table, WhereSQL: where, Raw: one}, nil
}

func parseUpdate(one string) (*ParsedDML, error) {
	// UPDATE t SET ... WHERE ...
	rest := strings.TrimSpace(one[len(firstKeyword(one)):])
	table, after, err := readSQLIdent(rest)
	if err != nil {
		return nil, err
	}
	after = strings.TrimSpace(after)
	if kw(after) != "SET" {
		return nil, fmt.Errorf("UPDATE must have SET")
	}
	setBody := strings.TrimSpace(after[len(firstKeyword(after)):])
	whereIdx := findTopLevelKeyword(setBody, "WHERE")
	where := ""
	if whereIdx >= 0 {
		where = strings.TrimSpace(setBody[whereIdx:])
	}
	return &ParsedDML{Kind: "update", Table: table, WhereSQL: where, Raw: one}, nil
}

func parseInsert(one string) (*ParsedDML, error) {
	upper := strings.ToUpper(one)
	if strings.Contains(upper, " SELECT ") || strings.HasSuffix(upper, " SELECT") {
		return nil, fmt.Errorf("INSERT…SELECT is not rollbackable")
	}
	rest := strings.TrimSpace(one[len(firstKeyword(one)):])
	if kw(rest) == "INTO" {
		rest = strings.TrimSpace(rest[len(firstKeyword(rest)):])
	}
	table, after, err := readSQLIdent(rest)
	if err != nil {
		return nil, err
	}
	_ = after
	return &ParsedDML{Kind: "insert", Table: table, Raw: one}, nil
}

func readSQLIdent(s string) (ident, rest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("missing table name")
	}
	if s[0] == '`' || s[0] == '"' {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", "", fmt.Errorf("unclosed identifier quote")
		}
		ident = s[1 : 1+end]
		rest = strings.TrimSpace(s[2+end:])
		// schema.table
		if strings.HasPrefix(rest, ".") {
			part2, rest2, err := readSQLIdent(rest[1:])
			if err != nil {
				return "", "", err
			}
			return ident + "." + part2, rest2, nil
		}
		return ident, rest, nil
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return "", "", fmt.Errorf("invalid table name")
	}
	ident = s[:i]
	rest = strings.TrimSpace(s[i:])
	if strings.HasPrefix(rest, ".") {
		part2, rest2, err := readSQLIdent(rest[1:])
		if err != nil {
			return "", "", err
		}
		return ident + "." + part2, rest2, nil
	}
	return ident, rest, nil
}

func findTopLevelKeyword(s, kw string) int {
	kw = strings.ToUpper(kw)
	depth := 0
	inSQ, inDQ, inBT := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSQ {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSQ = false
			}
			continue
		}
		if inDQ {
			if c == '"' {
				inDQ = false
			}
			continue
		}
		if inBT {
			if c == '`' {
				inBT = false
			}
			continue
		}
		switch c {
		case '\'':
			inSQ = true
		case '"':
			inDQ = true
		case '`':
			inBT = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				if matchKeywordAt(s, i, kw) {
					return i
				}
			}
		}
	}
	return -1
}

func matchKeywordAt(s string, i int, kw string) bool {
	if i+len(kw) > len(s) {
		return false
	}
	if !strings.EqualFold(s[i:i+len(kw)], kw) {
		return false
	}
	if i > 0 {
		prev := s[i-1]
		if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
			return false
		}
	}
	end := i + len(kw)
	if end < len(s) {
		next := s[end]
		if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_' {
			return false
		}
	}
	return true
}

// quoteIdent quotes an identifier for MySQL or Postgres.
func quoteIdent(style, name string) string {
	parts := strings.Split(name, ".")
	out := make([]string, len(parts))
	for i, p := range parts {
		if style == "postgres" {
			out[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		} else {
			out[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
		}
	}
	return strings.Join(out, ".")
}

// nullSentinel marks a SQL NULL in snapshot cell strings so the text "NULL"
// stays a quoted literal on rollback.
const nullSentinel = "\x00NULL\x00"

func quoteLit(v string) string {
	if v == nullSentinel {
		return "NULL"
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func snapshotCell(v any) string {
	if v == nil {
		return nullSentinel
	}
	return stringify(v)
}

// loadPrimaryKeyColumns queries information_schema for PK columns of table.
func loadPrimaryKeyColumns(ctx context.Context, q sqlQuerier, style, table string) ([]string, error) {
	schema, name := splitSchemaTable(table, style)
	var rows *sql.Rows
	var err error
	if style == "postgres" {
		rows, err = q.QueryContext(ctx, `
SELECT a.attname
FROM pg_index i
JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
JOIN pg_class c ON c.oid = i.indrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE i.indisprimary AND n.nspname = $1 AND c.relname = $2
ORDER BY array_position(i.indkey, a.attnum)`, schema, name)
	} else if schema != "" {
		rows, err = q.QueryContext(ctx, `
SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY'
ORDER BY ORDINAL_POSITION`, schema, name)
	} else {
		rows, err = q.QueryContext(ctx, `
SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY'
ORDER BY ORDINAL_POSITION`, name)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func splitSchemaTable(table, style string) (schema, name string) {
	parts := strings.Split(table, ".")
	if len(parts) == 1 {
		if style == "postgres" {
			return "public", parts[0]
		}
		return "", parts[0]
	}
	return parts[0], parts[len(parts)-1]
}

func fetchRows(ctx context.Context, q sqlQuerier, selectSQL string, maxRows int) (cols []string, data [][]string, err error) {
	rows, err := q.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err = rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if len(data) >= maxRows {
			return cols, data, fmt.Errorf("affected rows exceed rollback cap (%d)", maxRows)
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make([]string, len(cols))
		for i := range cols {
			row[i] = snapshotCell(raw[i])
		}
		data = append(data, row)
	}
	return cols, data, rows.Err()
}

// ExecuteDMLWithSnapshot runs a rollbackable DML inside a transaction: snapshot
// then mutate. style is "mysql" or "postgres".
func ExecuteDMLWithSnapshot(ctx context.Context, dbx *sql.DB, sessionSQL, query, style string) (*QueryResult, *RowSnapshot, error) {
	parsed, err := ParseSimpleDML(query)
	if err != nil {
		return nil, nil, err
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := dbx.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	if trimSQL(sessionSQL) != "" {
		if _, err := conn.ExecContext(ctx, sessionSQL); err != nil {
			return nil, nil, fmt.Errorf("session setup failed: %w", err)
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	pkCols, err := loadPrimaryKeyColumns(ctx, tx, style, parsed.Table)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup primary key: %w", err)
	}
	if len(pkCols) == 0 {
		return nil, nil, fmt.Errorf("table %s has no primary key; refused so rollback stays possible", parsed.Table)
	}

	qt := quoteIdent(style, parsed.Table)
	snap := &RowSnapshot{Kind: parsed.Kind, Table: parsed.Table, QuoteStyle: style, PKColumns: pkCols, WhereSQL: parsed.WhereSQL}

	switch parsed.Kind {
	case "update", "delete":
		sel := "SELECT * FROM " + qt
		if parsed.WhereSQL != "" {
			sel += " " + parsed.WhereSQL
		}
		cols, data, err := fetchRows(ctx, tx, sel, MaxSnapshotRows)
		if err != nil {
			return nil, nil, err
		}
		if len(data) == 0 && parsed.Kind == "delete" {
			// nothing to do; still allow execute
		}
		if len(data) > MaxSnapshotRows {
			return nil, nil, fmt.Errorf("affected rows exceed rollback cap (%d)", MaxSnapshotRows)
		}
		snap.Columns = cols
		snap.Rows = data
	}

	var aff int64
	if parsed.Kind == "insert" && style == "postgres" {
		ins := strings.TrimRight(strings.TrimSpace(query), ";")
		if !strings.Contains(strings.ToUpper(ins), " RETURNING ") {
			ins = ins + " RETURNING *"
		}
		cols, data, err := fetchRows(ctx, tx, ins, MaxSnapshotRows)
		if err != nil {
			return nil, nil, err
		}
		if len(data) == 0 {
			return nil, nil, fmt.Errorf("INSERT returned no rows to snapshot")
		}
		snap.Columns = cols
		snap.Rows = data
		aff = int64(len(data))
	} else {
		res, err := tx.ExecContext(ctx, query)
		if err != nil {
			return nil, nil, err
		}
		aff, _ = res.RowsAffected()

		if parsed.Kind == "insert" {
			var lid int64
			if err := tx.QueryRowContext(ctx, "SELECT LAST_INSERT_ID()").Scan(&lid); err != nil {
				return nil, nil, fmt.Errorf("capture insert id: %w", err)
			}
			if lid == 0 {
				return nil, nil, fmt.Errorf("cannot capture inserted primary key (LAST_INSERT_ID=0); refused")
			}
			if aff > 1 {
				return nil, nil, fmt.Errorf("multi-row INSERT rollback requires aff=1 with auto-inc; got %d", aff)
			}
			pk := quoteIdent(style, pkCols[0])
			sel := fmt.Sprintf("SELECT * FROM %s WHERE %s = %d", qt, pk, lid)
			cols, data, err := fetchRows(ctx, tx, sel, MaxSnapshotRows)
			if err != nil {
				return nil, nil, err
			}
			snap.Columns = cols
			snap.Rows = data
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &QueryResult{
		Columns:    []string{"rows_affected"},
		Rows:       [][]string{{fmt.Sprintf("%d", aff)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    fmt.Sprintf("OK, %d rows affected (snapshot saved)", aff),
	}, snap, nil
}

// BuildRollbackSQL generates compensating statements from a snapshot.
func BuildRollbackSQL(snap *RowSnapshot) ([]string, error) {
	if snap == nil {
		return nil, fmt.Errorf("nil snapshot")
	}
	qt := quoteIdent(snap.QuoteStyle, snap.Table)
	switch snap.Kind {
	case "delete":
		var out []string
		for _, row := range snap.Rows {
			cols := make([]string, len(snap.Columns))
			vals := make([]string, len(snap.Columns))
			for i, c := range snap.Columns {
				cols[i] = quoteIdent(snap.QuoteStyle, c)
				vals[i] = quoteLit(row[i])
			}
			out = append(out, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				qt, strings.Join(cols, ", "), strings.Join(vals, ", ")))
		}
		return out, nil
	case "insert":
		var out []string
		pkIdx := map[string]int{}
		for i, c := range snap.Columns {
			pkIdx[c] = i
		}
		for _, row := range snap.Rows {
			var preds []string
			for _, pk := range snap.PKColumns {
				i, ok := pkIdx[pk]
				if !ok {
					return nil, fmt.Errorf("pk column %s missing from snapshot", pk)
				}
				preds = append(preds, quoteIdent(snap.QuoteStyle, pk)+" = "+quoteLit(row[i]))
			}
			out = append(out, fmt.Sprintf("DELETE FROM %s WHERE %s", qt, strings.Join(preds, " AND ")))
		}
		return out, nil
	case "update":
		var out []string
		pkIdx := map[string]int{}
		for i, c := range snap.Columns {
			pkIdx[c] = i
		}
		for _, row := range snap.Rows {
			var sets []string
			for i, c := range snap.Columns {
				sets = append(sets, quoteIdent(snap.QuoteStyle, c)+" = "+quoteLit(row[i]))
			}
			var preds []string
			for _, pk := range snap.PKColumns {
				i, ok := pkIdx[pk]
				if !ok {
					return nil, fmt.Errorf("pk column %s missing from snapshot", pk)
				}
				preds = append(preds, quoteIdent(snap.QuoteStyle, pk)+" = "+quoteLit(row[i]))
			}
			out = append(out, fmt.Sprintf("UPDATE %s SET %s WHERE %s",
				qt, strings.Join(sets, ", "), strings.Join(preds, " AND ")))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown snapshot kind %s", snap.Kind)
	}
}

// ExecuteRollbackSQL runs compensating statements in one transaction.
func ExecuteRollbackSQL(ctx context.Context, dbx *sql.DB, sessionSQL string, stmts []string) (*QueryResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	conn, err := dbx.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if trimSQL(sessionSQL) != "" {
		if _, err := conn.ExecContext(ctx, sessionSQL); err != nil {
			return nil, fmt.Errorf("session setup failed: %w", err)
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	for _, st := range stmts {
		res, err := tx.ExecContext(ctx, st)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &QueryResult{
		Columns:    []string{"rows_affected"},
		Rows:       [][]string{{fmt.Sprintf("%d", total)}},
		RowCount:   1,
		DurationMs: timed(start),
		Message:    fmt.Sprintf("rolled back, %d rows affected", total),
	}, nil
}

// MarshalSnapshot encodes a snapshot for storage.
func MarshalSnapshot(snap *RowSnapshot) (string, error) {
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalSnapshot decodes a stored snapshot.
func UnmarshalSnapshot(s string) (*RowSnapshot, error) {
	var snap RowSnapshot
	if err := json.Unmarshal([]byte(s), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
