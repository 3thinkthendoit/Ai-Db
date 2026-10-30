package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type sqlQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func pingSQL(ctx context.Context, dbx *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return dbx.PingContext(ctx)
}

func execSQL(ctx context.Context, dbx *sql.DB, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	return execSQLQuerier(ctx, dbx, query, maxRows, allowWrites)
}

// execSQLWithSession runs optional sessionSQL then query on the same pooled connection
// so USE / SET search_path stick for that statement.
func execSQLWithSession(ctx context.Context, dbx *sql.DB, sessionSQL, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	sessionSQL = trimSQL(sessionSQL)
	if sessionSQL == "" {
		return execSQL(ctx, dbx, query, maxRows, allowWrites)
	}
	if err := EnsureReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := dbx.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, sessionSQL); err != nil {
		return nil, fmt.Errorf("session setup failed: %w", err)
	}
	out, err := execSQLQuerierTimed(ctx, conn, query, maxRows, allowWrites, start)
	return out, err
}

func execSQLQuerier(ctx context.Context, q sqlQuerier, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	if err := EnsureReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return execSQLQuerierTimed(ctx, q, query, maxRows, allowWrites, start)
}

func execSQLQuerierTimed(ctx context.Context, q sqlQuerier, query string, maxRows int, allowWrites bool, start time.Time) (*QueryResult, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		// Non-SELECT (when allowed) may need Exec.
		if allowWrites {
			res, e2 := q.ExecContext(ctx, query)
			if e2 != nil {
				return nil, fmt.Errorf("query failed: %v; exec failed: %w", err, e2)
			}
			aff, _ := res.RowsAffected()
			return &QueryResult{
				Columns:    []string{"rows_affected"},
				Rows:       [][]string{{fmt.Sprintf("%d", aff)}},
				RowCount:   1,
				DurationMs: timed(start),
				Message:    fmt.Sprintf("OK, %d rows affected", aff),
			}, nil
		}
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &QueryResult{Columns: cols, Rows: [][]string{}, DurationMs: 0}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if out.RowCount >= maxRows {
			out.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i := range cols {
			row[i] = stringify(raw[i])
		}
		out.Rows = append(out.Rows, row)
		out.RowCount++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.DurationMs = timed(start)
	return out, nil
}

func trimSQL(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	return s
}
