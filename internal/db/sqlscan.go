package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func pingSQL(ctx context.Context, dbx *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return dbx.PingContext(ctx)
}

func execSQL(ctx context.Context, dbx *sql.DB, query string, maxRows int, allowWrites bool) (*QueryResult, error) {
	if err := EnsureReadOnly(query, allowWrites); err != nil {
		return nil, err
	}
	if maxRows <= 0 {
		maxRows = 500
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	rows, err := dbx.QueryContext(ctx, query)
	if err != nil {
		// Non-SELECT (when allowed) may need Exec.
		if allowWrites {
			res, e2 := dbx.ExecContext(ctx, query)
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
