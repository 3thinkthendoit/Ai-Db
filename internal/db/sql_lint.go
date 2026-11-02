package db

import (
	"fmt"
	"strings"

	"AI-DB/internal/config"
)

// LintFinding is one lightweight static check result (not goInception).
type LintFinding struct {
	Level   string `json:"level"` // info | warn | error
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LintSQL runs cheap heuristic checks for mutation tickets.
func LintSQL(connType config.ConnType, query string) []LintFinding {
	if connType == config.ConnRedis || connType == config.ConnMongo {
		return []LintFinding{{Level: "error", Code: "engine", Message: "writes on this engine are not ticketed"}}
	}
	var out []LintFinding
	normalized := normalizeSQL(query)
	if normalized == "" {
		return []LintFinding{{Level: "error", Code: "empty", Message: "empty SQL"}}
	}
	for _, stmt := range splitStatements(normalized) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		check := strings.TrimSpace(stripLeadingWithCTEs(stmt))
		if strings.HasPrefix(check, "EXPLAIN") {
			continue
		}
		first := firstKeyword(check)
		switch first {
		case "DROP":
			out = append(out, LintFinding{Level: "warn", Code: "drop", Message: "DROP is high-risk; confirm target object"})
		case "TRUNCATE":
			out = append(out, LintFinding{Level: "warn", Code: "truncate", Message: "TRUNCATE cannot be auto-rolled back"})
		case "DELETE":
			if !containsSQLKeyword(check, "WHERE") {
				out = append(out, LintFinding{Level: "error", Code: "delete_no_where", Message: "DELETE without WHERE is refused by lint"})
			}
		case "UPDATE":
			if !containsSQLKeyword(check, "WHERE") {
				out = append(out, LintFinding{Level: "error", Code: "update_no_where", Message: "UPDATE without WHERE is refused by lint"})
			}
		case "GRANT", "REVOKE":
			out = append(out, LintFinding{Level: "warn", Code: "priv", Message: "privilege change — verify least privilege"})
		}
		if containsSQLKeyword(check, "INTO") &&
			(containsSQLKeyword(check, "OUTFILE") || containsSQLKeyword(check, "DUMPFILE")) {
			out = append(out, LintFinding{Level: "error", Code: "outfile", Message: "INTO OUTFILE/DUMPFILE is blocked"})
		}
	}
	class := ClassifySQL(connType, query)
	if class == StmtDML {
		if _, err := ParseSimpleDML(query); err != nil {
			out = append(out, LintFinding{Level: "info", Code: "no_rollback", Message: fmt.Sprintf("not auto-rollbackable: %v", err)})
		} else {
			out = append(out, LintFinding{Level: "info", Code: "rollback_ok", Message: "simple DML — rollback snapshot available after execute"})
		}
	}
	if class == StmtDDL {
		out = append(out, LintFinding{Level: "info", Code: "ddl", Message: "DDL is approvable but not auto-rollbackable"})
	}
	return out
}

// LintBlocks reports whether any finding is error-level.
func LintBlocks(findings []LintFinding) bool {
	for _, f := range findings {
		if f.Level == "error" {
			return true
		}
	}
	return false
}

// FormatLintSummary is a short single-line summary for audit/UI.
func FormatLintSummary(findings []LintFinding) string {
	if len(findings) == 0 {
		return "ok"
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.Level+":"+f.Code)
	}
	s := strings.Join(parts, ", ")
	if len(s) > 480 {
		return s[:480]
	}
	return s
}
