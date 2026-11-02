package db

import (
	"testing"

	"AI-DB/internal/config"
)

func TestLintSQL_DeleteNoWhere(t *testing.T) {
	f := LintSQL(config.ConnMySQL, "DELETE FROM t")
	if !LintBlocks(f) {
		t.Fatalf("expected block, got %#v", f)
	}
}

func TestLintSQL_UpdateWithWhere(t *testing.T) {
	f := LintSQL(config.ConnMySQL, "UPDATE t SET a=1 WHERE id=1")
	if LintBlocks(f) {
		t.Fatalf("should not block: %#v", f)
	}
}

func TestLintSQL_DropWarn(t *testing.T) {
	f := LintSQL(config.ConnMySQL, "DROP TABLE t")
	found := false
	for _, x := range f {
		if x.Code == "drop" && x.Level == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected drop warn: %#v", f)
	}
	if LintBlocks(f) {
		t.Fatalf("drop should warn not error")
	}
}
