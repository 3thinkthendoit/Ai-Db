package db

import "testing"

func TestEnsureReadOnly(t *testing.T) {
	cases := []struct {
		q       string
		allow   bool
		wantErr bool
	}{
		{"SELECT 1", false, false},
		{"DELETE FROM t", false, true},
		{"DELETE FROM t", true, false},
		{"WITH x AS (SELECT 1) DELETE FROM t", false, true},
		{"WITH x AS (SELECT 1) UPDATE t SET a=1", false, true},
		{"WITH x AS (SELECT 1) INSERT INTO t VALUES (1)", false, true},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false, false},
		{"EXPLAIN ANALYZE DELETE FROM t", false, true},
		{"EXPLAIN SELECT 1", false, false},
		{"SELECT * FROM t INTO OUTFILE '/tmp/x'", false, true},
		{"SELECT 'DELETE FROM t' AS x", false, false},
		{"-- DELETE\nSELECT 1", false, false},
		{"SELECT 1; DELETE FROM t", false, true},
	}
	for _, tc := range cases {
		err := EnsureReadOnly(tc.q, tc.allow)
		if tc.wantErr && err == nil {
			t.Fatalf("expected error for %q", tc.q)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.q, err)
		}
	}
}

func TestEnsureExplainReadOnly(t *testing.T) {
	if err := EnsureExplainReadOnly("SELECT 1"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := EnsureExplainReadOnly("ANALYZE DELETE FROM t"); err == nil {
		t.Fatal("analyze delete should block")
	}
	if err := EnsureExplainReadOnly("DELETE FROM t"); err == nil {
		t.Fatal("delete should block")
	}
	if err := EnsureExplainReadOnly("WITH x AS (SELECT 1) DELETE FROM t"); err == nil {
		t.Fatal("cte delete should block")
	}
}

func TestEnsureRedisReadOnly(t *testing.T) {
	if err := EnsureRedisReadOnly("GET foo", false); err != nil {
		t.Fatalf("GET should pass: %v", err)
	}
	if err := EnsureRedisReadOnly("SET foo bar", false); err == nil {
		t.Fatal("SET should block")
	}
}
