package db

import (
	"fmt"
	"regexp"
	"strings"

	"AI-DB/internal/config"
)

// Create-database flow step labels, reported back to the dialog so a partial
// failure can name the statement that broke (DDL is not atomic across steps:
// MySQL auto-commits it and Postgres forbids CREATE DATABASE in a transaction).
const (
	StepCreateDatabase = "createDatabase"
	StepCreateAccount  = "createAccount"
	StepGrant          = "grant"
)

// CreateDatabaseSpec is the create-database dialog payload: database identity
// plus an optional login account that is authorised on the new database once
// creation succeeds.
type CreateDatabaseSpec struct {
	Name string `json:"name"`
	// Charset is MySQL CHARACTER SET; empty keeps the server default.
	Charset string `json:"charset,omitempty"`
	// Collate is MySQL COLLATE / PostgreSQL LC_COLLATE (+LC_CTYPE).
	Collate string `json:"collate,omitempty"`
	// Encoding is PostgreSQL ENCODING; ignored on MySQL.
	Encoding string `json:"encoding,omitempty"`
	// Account/Password mint a new login when both are set (optional pair).
	Account  string `json:"account,omitempty"`
	Password string `json:"password,omitempty"`
	// Host is the MySQL user host part (defaults to "%"); ignored on PostgreSQL.
	Host string `json:"host,omitempty"`
}

// CreateDatabaseStep is one statement of the creation flow plus its UI label.
type CreateDatabaseStep struct {
	Label string
	SQL   string
}

// CreateDatabaseResult reports the outcome back to the dialog. A step failure
// is a business outcome (OK=false), not a transport error: earlier steps stay
// applied because DDL cannot be rolled back.
type CreateDatabaseResult struct {
	OK        bool     `json:"ok"`
	Completed []string `json:"completed"`
	// FailedStep is the label of the statement that failed (empty when OK).
	FailedStep string `json:"failedStep,omitempty"`
	Error      string `json:"error,omitempty"`
}

// CharsetOption is one MySQL character set with its collations.
type CharsetOption struct {
	Name             string   `json:"name"`
	DefaultCollation string   `json:"defaultCollation"`
	Collations       []string `json:"collations"`
}

// CreateDatabaseOptions feeds the dialog dropdowns.
type CreateDatabaseOptions struct {
	// Charsets is MySQL-only (Postgres leaves it empty).
	Charsets []CharsetOption `json:"charsets"`
	// Encodings is PostgreSQL-only candidate ENCODING values.
	Encodings []string `json:"encodings"`
}

// MySQLCollationsSQL lists every collation with its charset and default flag.
// Read-only, so it passes the write gate like any other introspection query.
const MySQLCollationsSQL = "SELECT CHARACTER_SET_NAME, COLLATION_NAME, IS_DEFAULT " +
	"FROM INFORMATION_SCHEMA.COLLATIONS ORDER BY CHARACTER_SET_NAME, COLLATION_NAME"

// pgEncodings is the compile-time-fixed encoding catalogue (F1: PostgreSQL
// encodings are built in, so a static list needs no catalog query).
var pgEncodings = []string{
	"UTF8", "SQL_ASCII", "LATIN1", "LATIN2", "LATIN4", "LATIN5", "LATIN9", "LATIN10",
	"WIN866", "WIN874", "WIN1250", "WIN1251", "WIN1252", "WIN1256", "WIN1258",
	"ISO_8859_5", "ISO_8859_6", "ISO_8859_7", "ISO_8859_8",
	"KOI8R", "KOI8U", "GB18030", "GBK", "BIG5", "EUC_JP", "EUC_KR", "EUC_TW", "SJIS", "UHC",
}

// PGEncodings returns the candidate ENCODING values for the dialog.
func PGEncodings() []string {
	out := make([]string, len(pgEncodings))
	copy(out, pgEncodings)
	return out
}

// charsetIdentRe guards names spliced into DDL without quotes: MySQL
// CHARACTER SET / COLLATE sit in keyword position, and Postgres encoding names
// are word-constituents. Postgres LC_COLLATE is a quoted string literal (any
// locale spelling allowed), so it is NOT subject to this whitelist.
var charsetIdentRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// BuildCreateDatabaseStatements returns the ordered statements for connType.
// Only MySQL and Postgres have create-database semantics; Redis/Mongo do not.
func BuildCreateDatabaseStatements(connType config.ConnType, spec CreateDatabaseSpec) ([]CreateDatabaseStep, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("database name is required")
	}
	if strings.ContainsRune(name, 0) {
		return nil, fmt.Errorf("database name must not contain NUL")
	}
	account := strings.TrimSpace(spec.Account)
	if account != "" && spec.Password == "" {
		return nil, fmt.Errorf("password is required when an account is set")
	}

	switch connType {
	case config.ConnMySQL:
		if len(name) > 64 {
			return nil, fmt.Errorf("mysql database name too long (max 64 bytes)")
		}
		// CHARACTER SET / COLLATE are spliced unquoted (keyword position).
		for _, pair := range [][2]string{{"charset", spec.Charset}, {"collate", spec.Collate}} {
			if pair[1] != "" && !charsetIdentRe.MatchString(pair[1]) {
				return nil, fmt.Errorf("invalid %s name: %q", pair[0], pair[1])
			}
		}
		var b strings.Builder
		b.WriteString("CREATE DATABASE ")
		b.WriteString(quoteMySQLIdent(name))
		if spec.Charset != "" {
			b.WriteString(" CHARACTER SET " + spec.Charset)
		}
		if spec.Collate != "" {
			b.WriteString(" COLLATE " + spec.Collate)
		}
		steps := []CreateDatabaseStep{{StepCreateDatabase, b.String()}}
		if account != "" {
			host := strings.TrimSpace(spec.Host)
			if host == "" {
				host = "%"
			}
			userHost := quoteMySQLString(account) + "@" + quoteMySQLString(host)
			steps = append(steps,
				CreateDatabaseStep{StepCreateAccount,
					fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s", userHost, quoteMySQLString(spec.Password))},
				CreateDatabaseStep{StepGrant,
					fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s", quoteMySQLIdent(name), userHost)},
			)
		}
		return steps, nil

	case config.ConnPostgres:
		if len(name) > 63 {
			return nil, fmt.Errorf("postgres database name too long (max 63 bytes)")
		}
		if spec.Encoding != "" && !charsetIdentRe.MatchString(spec.Encoding) {
			return nil, fmt.Errorf("invalid encoding name: %q", spec.Encoding)
		}
		// LC_COLLATE/LC_CTYPE are quoted literals: any locale spelling (en_US.utf8,
		// C.UTF-8 …) is acceptable; only NUL cannot appear in a SQL literal.
		if strings.ContainsRune(spec.Collate, 0) {
			return nil, fmt.Errorf("collate must not contain NUL")
		}
		// A non-default encoding/collation is only accepted from the pristine
		// template, so pin template0 whenever either is requested.
		var opts []string
		if spec.Encoding != "" || spec.Collate != "" {
			opts = append(opts, "TEMPLATE template0")
		}
		if spec.Encoding != "" {
			opts = append(opts, "ENCODING "+quotePGString(spec.Encoding))
		}
		if spec.Collate != "" {
			opts = append(opts,
				"LC_COLLATE "+quotePGString(spec.Collate),
				"LC_CTYPE "+quotePGString(spec.Collate))
		}
		sql := "CREATE DATABASE " + quotePGIdent(name)
		if len(opts) > 0 {
			sql += " WITH " + strings.Join(opts, " ")
		}
		steps := []CreateDatabaseStep{{StepCreateDatabase, sql}}
		if account != "" {
			steps = append(steps,
				CreateDatabaseStep{StepCreateAccount,
					fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s", quotePGIdent(account), quotePGString(spec.Password))},
				// OWNER covers database- and schema-level privileges in one
				// statement; a GRANT chain on pg namespaces would not.
				CreateDatabaseStep{StepGrant,
					fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", quotePGIdent(name), quotePGIdent(account))},
			)
		}
		return steps, nil

	default:
		return nil, fmt.Errorf("create database is not supported for %s connections", connType)
	}
}

// GroupMySQLCollations folds a MySQLCollationsSQL result into dialog options,
// preserving the charset order of the (already sorted) result rows.
func GroupMySQLCollations(res *QueryResult) CreateDatabaseOptions {
	cs, cc, isDef := -1, -1, -1
	for i, col := range res.Columns {
		switch strings.ToUpper(col) {
		case "CHARACTER_SET_NAME":
			cs = i
		case "COLLATION_NAME":
			cc = i
		case "IS_DEFAULT":
			isDef = i
		}
	}
	out := CreateDatabaseOptions{}
	if cs < 0 || cc < 0 {
		return out
	}
	index := map[string]int{}
	for _, row := range res.Rows {
		if cs >= len(row) || cc >= len(row) {
			continue
		}
		charset, collation := row[cs], row[cc]
		pos, ok := index[charset]
		if !ok {
			pos = len(out.Charsets)
			index[charset] = pos
			out.Charsets = append(out.Charsets, CharsetOption{Name: charset})
		}
		opt := &out.Charsets[pos]
		opt.Collations = append(opt.Collations, collation)
		if isDef >= 0 && isDef < len(row) && strings.EqualFold(row[isDef], "YES") {
			opt.DefaultCollation = collation
		}
	}
	return out
}

// quoteMySQLString escapes for the default sql_mode (backslashes are escape
// characters unless NO_BACKSLASH_ESCAPES is set, so double them too).
// Identifier quoting reuses quoteMySQLIdent/quotePGIdent from the drivers.
func quoteMySQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `''`)
	return "'" + s + "'"
}

// quotePGString relies on standard_conforming_strings=on (default since 9.1):
// backslashes are literal, only single quotes need doubling.
func quotePGString(s string) string {
	return "'" + strings.ReplaceAll(s, `'`, `''`) + "'"
}
