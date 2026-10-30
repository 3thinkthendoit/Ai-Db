package db

import (
	"fmt"
	"strings"
	"unicode"
)

var writeKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE",
	"DROP", "CREATE", "ALTER", "TRUNCATE", "GRANT", "REVOKE",
	"CALL", "EXECUTE", "LOAD", "COPY", "ATTACH", "DETACH",
	"RENAME",
}

// EnsureReadOnly rejects write / DDL / ANALYZE-explain statements when writes are disabled.
func EnsureReadOnly(query string, allowWrites bool) error {
	if allowWrites {
		return nil
	}
	normalized := normalizeSQL(query)
	if normalized == "" {
		return fmt.Errorf("empty query")
	}
	if err := rejectExplainAnalyze(normalized); err != nil {
		return err
	}
	if containsSQLKeyword(normalized, "INTO") &&
		(containsSQLKeyword(normalized, "OUTFILE") || containsSQLKeyword(normalized, "DUMPFILE")) {
		return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): SELECT INTO OUTFILE/DUMPFILE")
	}

	for _, stmt := range splitStatements(normalized) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		check := stmt
		if strings.HasPrefix(check, "EXPLAIN") {
			rest := strings.TrimSpace(check[len("EXPLAIN"):])
			if strings.HasPrefix(rest, "ANALYZE") {
				return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): EXPLAIN ANALYZE")
			}
			check = rest
		}
		check = strings.TrimSpace(stripLeadingWithCTEs(check))
		if check == "" {
			continue
		}
		first := firstKeyword(check)
		for _, b := range writeKeywords {
			if first == b || first == "COMMENT" {
				return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): starts with %s", first)
			}
		}
		for _, b := range writeKeywords {
			if containsSQLKeyword(check, b) {
				return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): contains %s", b)
			}
		}
	}
	return nil
}

// EnsureExplainReadOnly validates an EXPLAIN target under read-only mode.
func EnsureExplainReadOnly(query string) error {
	normalized := normalizeSQL(query)
	if normalized == "" {
		return fmt.Errorf("empty query")
	}
	if firstKeyword(normalized) == "ANALYZE" || strings.HasPrefix(normalized, "ANALYZE ") {
		return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): EXPLAIN ANALYZE")
	}
	if err := rejectExplainAnalyze(normalized); err != nil {
		return err
	}
	return EnsureReadOnly(query, false)
}

func rejectExplainAnalyze(normalized string) error {
	if strings.Contains(normalized, "EXPLAIN ANALYZE") {
		return fmt.Errorf("write/DDL statement blocked (enable Allow Writes in settings): EXPLAIN ANALYZE")
	}
	return nil
}

// EnsureRedisReadOnly blocks mutating Redis commands when writes are disabled.
func EnsureRedisReadOnly(cmd string, allowWrites bool) error {
	if allowWrites {
		return nil
	}
	parts := splitRedisArgs(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	op := strings.ToUpper(parts[0])
	readOps := map[string]bool{
		"GET": true, "MGET": true, "STRLEN": true, "GETRANGE": true,
		"EXISTS": true, "TYPE": true, "TTL": true, "PTTL": true, "KEYS": true, "SCAN": true,
		"HGET": true, "HMGET": true, "HGETALL": true, "HKEYS": true, "HVALS": true, "HLEN": true,
		"LRANGE": true, "LLEN": true, "LINDEX": true,
		"SMEMBERS": true, "SCARD": true, "SISMEMBER": true, "SRANDMEMBER": true,
		"ZRANGE": true, "ZREVRANGE": true, "ZCARD": true, "ZSCORE": true, "ZRANK": true,
		"INFO": true, "DBSIZE": true, "PING": true, "ECHO": true, "TIME": true,
		"XRANGE": true, "XLEN": true, "XINFO": true,
		"MEMORY": true, "OBJECT": true, "DUMP": true,
	}
	if !readOps[op] {
		return fmt.Errorf("redis write/admin command blocked (enable Allow Writes): %s", op)
	}
	return nil
}

func normalizeSQL(query string) string {
	q := stripSQLComments(strings.TrimSpace(query))
	q = stripSQLStrings(q)
	q = strings.Join(strings.Fields(q), " ")
	return strings.ToUpper(q)
}

func stripSQLComments(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	for i := 0; i < len(q); {
		if i+1 < len(q) && q[i] == '-' && q[i+1] == '-' {
			for i < len(q) && q[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(q) && q[i] == '/' && q[i+1] == '*' {
			i += 2
			for i+1 < len(q) && !(q[i] == '*' && q[i+1] == '/') {
				i++
			}
			if i+1 < len(q) {
				i += 2
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(q[i])
		i++
	}
	return b.String()
}

func stripSQLStrings(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	for i := 0; i < len(q); {
		ch := q[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			i++
			for i < len(q) {
				if q[i] == '\\' && i+1 < len(q) {
					i += 2
					continue
				}
				if q[i] == quote {
					if quote == '\'' && i+1 < len(q) && q[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}

func splitStatements(s string) []string {
	return strings.Split(s, ";")
}

func firstKeyword(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return ""
	}
	i := 0
	for i < len(stmt) {
		r := rune(stmt[i])
		if unicode.IsLetter(r) || r == '_' {
			i++
			continue
		}
		break
	}
	return stmt[:i]
}

func containsSQLKeyword(stmt, kw string) bool {
	parts := strings.FieldsFunc(stmt, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '_'
	})
	for _, p := range parts {
		if p == kw {
			return true
		}
	}
	return false
}

func stripLeadingWithCTEs(stmt string) string {
	s := strings.TrimSpace(stmt)
	for strings.HasPrefix(s, "WITH") {
		rest := strings.TrimSpace(s[4:])
		if strings.HasPrefix(rest, "RECURSIVE") {
			rest = strings.TrimSpace(rest[len("RECURSIVE"):])
		}
		advanced := false
		for {
			rest = strings.TrimSpace(rest)
			j := 0
			for j < len(rest) {
				r := rune(rest[j])
				if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
					j++
					continue
				}
				break
			}
			if j == 0 {
				break
			}
			rest = strings.TrimSpace(rest[j:])
			if strings.HasPrefix(rest, "(") {
				end, ok := matchingParen(rest, 0)
				if !ok {
					return s
				}
				rest = strings.TrimSpace(rest[end+1:])
			}
			if !strings.HasPrefix(rest, "AS") {
				break
			}
			rest = strings.TrimSpace(rest[2:])
			if !strings.HasPrefix(rest, "(") {
				break
			}
			end, ok := matchingParen(rest, 0)
			if !ok {
				return s
			}
			rest = strings.TrimSpace(rest[end+1:])
			advanced = true
			if strings.HasPrefix(rest, ",") {
				rest = strings.TrimSpace(rest[1:])
				continue
			}
			s = rest
			break
		}
		if !advanced {
			break
		}
	}
	return s
}

func matchingParen(s string, openIdx int) (int, bool) {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}
