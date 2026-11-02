package db

import (
	"strings"

	"AI-DB/internal/config"
)

// StmtClass is the mutation-policy class of a statement.
type StmtClass string

const (
	StmtRead StmtClass = "read"
	StmtDML  StmtClass = "dml"  // INSERT / UPDATE / DELETE / REPLACE — rollback candidates
	StmtDDL  StmtClass = "ddl"  // schema / privilege / truncate — approvable, not auto-rollbackable
	StmtOther StmtClass = "other" // redis/mongo / unknown writes
)

var dmlKeywords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "MERGE": true,
}

var ddlKeywords = map[string]bool{
	"DROP": true, "CREATE": true, "ALTER": true, "TRUNCATE": true,
	"GRANT": true, "REVOKE": true, "RENAME": true, "COMMENT": true,
	"CALL": true, "EXECUTE": true, "LOAD": true, "COPY": true,
	"ATTACH": true, "DETACH": true,
}

// ClassifySQL classifies a SQL (or redis/mongo) statement for the mutation gate.
func ClassifySQL(connType config.ConnType, query string) StmtClass {
	switch connType {
	case config.ConnRedis:
		if EnsureRedisReadOnly(query, false) != nil {
			return StmtOther
		}
		return StmtRead
	case config.ConnMongo:
		if MongoQueryMutating(query) {
			return StmtOther
		}
		return StmtRead
	default:
		return classifySQLText(query)
	}
}

func classifySQLText(query string) StmtClass {
	normalized := normalizeSQL(query)
	if normalized == "" {
		return StmtRead
	}
	if containsSQLKeyword(normalized, "INTO") &&
		(containsSQLKeyword(normalized, "OUTFILE") || containsSQLKeyword(normalized, "DUMPFILE")) {
		return StmtDDL
	}
	class := StmtRead
	for _, stmt := range splitStatements(normalized) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		check := stmt
		if strings.HasPrefix(check, "EXPLAIN") {
			rest := strings.TrimSpace(check[len("EXPLAIN"):])
			if strings.HasPrefix(rest, "ANALYZE") {
				return StmtDDL
			}
			check = rest
		}
		check = strings.TrimSpace(stripLeadingWithCTEs(check))
		if check == "" {
			continue
		}
		first := firstKeyword(check)
		if dmlKeywords[first] {
			if class == StmtRead {
				class = StmtDML
			}
			continue
		}
		if ddlKeywords[first] || first == "COMMENT" {
			return StmtDDL
		}
		for kw := range dmlKeywords {
			if containsSQLKeyword(check, kw) {
				if class == StmtRead {
					class = StmtDML
				}
			}
		}
		for kw := range ddlKeywords {
			if containsSQLKeyword(check, kw) {
				return StmtDDL
			}
		}
	}
	return class
}

// IsMutatingClass reports whether the class requires approval under the mutation gate.
func IsMutatingClass(c StmtClass) bool {
	return c == StmtDML || c == StmtDDL || c == StmtOther
}
