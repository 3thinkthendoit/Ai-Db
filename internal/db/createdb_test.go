package db

import (
	"strings"
	"testing"

	"AI-DB/internal/config"
)

func TestBuildCreateDatabaseStatementsMySQL(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnMySQL, CreateDatabaseSpec{
		Name:    "shop",
		Charset: "utf8mb4",
		Collate: "utf8mb4_general_ci",
	})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	want := "CREATE DATABASE `shop` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"
	if steps[0].SQL != want {
		t.Errorf("SQL = %q, want %q", steps[0].SQL, want)
	}
	if steps[0].Label != StepCreateDatabase {
		t.Errorf("label = %q", steps[0].Label)
	}
}

func TestBuildCreateDatabaseStatementsMySQLWithAccount(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnMySQL, CreateDatabaseSpec{
		Name:     "a`b",
		Account:  "bob",
		Password: `p'w\z`,
	})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	if !strings.Contains(steps[0].SQL, "`a``b`") {
		t.Errorf("ident not escaped: %q", steps[0].SQL)
	}
	wantUser := "CREATE USER 'bob'@'%' IDENTIFIED BY 'p''w\\\\z'"
	if steps[1].SQL != wantUser {
		t.Errorf("create user = %q, want %q", steps[1].SQL, wantUser)
	}
	wantGrant := "GRANT ALL PRIVILEGES ON `a``b`.* TO 'bob'@'%'"
	if steps[2].SQL != wantGrant {
		t.Errorf("grant = %q, want %q", steps[2].SQL, wantGrant)
	}
}

func TestBuildCreateDatabaseStatementsMySQLHost(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnMySQL, CreateDatabaseSpec{
		Name: "db1", Account: "u", Password: "p", Host: "10.0.%",
	})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if !strings.Contains(steps[1].SQL, "'u'@'10.0.%'") {
		t.Errorf("host not applied: %q", steps[1].SQL)
	}
}

func TestBuildCreateDatabaseStatementsPostgres(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnPostgres, CreateDatabaseSpec{
		Name:     `rep"ort`,
		Encoding: "UTF8",
		Collate:  "C",
		Account:  "bob",
		Password: "p'x",
	})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	want := `CREATE DATABASE "rep""ort" WITH TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C'`
	if steps[0].SQL != want {
		t.Errorf("SQL = %q, want %q", steps[0].SQL, want)
	}
	wantRole := `CREATE ROLE "bob" LOGIN PASSWORD 'p''x'`
	if steps[1].SQL != wantRole {
		t.Errorf("create role = %q, want %q", steps[1].SQL, wantRole)
	}
	wantOwner := `ALTER DATABASE "rep""ort" OWNER TO "bob"`
	if steps[2].SQL != wantOwner {
		t.Errorf("owner = %q, want %q", steps[2].SQL, wantOwner)
	}
}

func TestBuildCreateDatabaseStatementsPostgresDefaults(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnPostgres, CreateDatabaseSpec{Name: "plain"})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	if len(steps) != 1 || steps[0].SQL != `CREATE DATABASE "plain"` {
		t.Errorf("steps = %+v, want bare CREATE DATABASE", steps)
	}
}

// PG LC_COLLATE is a quoted literal: dotted locale spellings must pass and be
// escaped as string literals, not rejected by the identifier whitelist.
func TestBuildCreateDatabaseStatementsPostgresLocaleCollate(t *testing.T) {
	steps, err := BuildCreateDatabaseStatements(config.ConnPostgres, CreateDatabaseSpec{
		Name: "loc", Encoding: "UTF8", Collate: "en_US.utf8",
	})
	if err != nil {
		t.Fatalf("Build = %v", err)
	}
	want := `CREATE DATABASE "loc" WITH TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'en_US.utf8' LC_CTYPE 'en_US.utf8'`
	if len(steps) != 1 || steps[0].SQL != want {
		t.Errorf("SQL = %+v, want %q", steps, want)
	}
}

func TestBuildCreateDatabaseStatementsValidation(t *testing.T) {
	cases := []struct {
		name string
		typ  config.ConnType
		spec CreateDatabaseSpec
	}{
		{"empty name", config.ConnMySQL, CreateDatabaseSpec{}},
		{"account without password", config.ConnMySQL, CreateDatabaseSpec{Name: "d", Account: "u"}},
		{"charset injection", config.ConnMySQL, CreateDatabaseSpec{Name: "d", Charset: "utf8; DROP"}},
		{"mysql collate injection", config.ConnMySQL, CreateDatabaseSpec{Name: "d", Collate: "utf8' OR '1'='1"}},
		{"pg encoding injection", config.ConnPostgres, CreateDatabaseSpec{Name: "d", Encoding: "UTF8'; DROP"}},
		{"pg collate NUL", config.ConnPostgres, CreateDatabaseSpec{Name: "d", Collate: "C\x00"}},
		{"mysql name too long", config.ConnMySQL, CreateDatabaseSpec{Name: strings.Repeat("x", 65)}},
		{"pg name too long", config.ConnPostgres, CreateDatabaseSpec{Name: strings.Repeat("x", 64)}},
		{"unsupported type", config.ConnRedis, CreateDatabaseSpec{Name: "d"}},
		{"unsupported type mongo", config.ConnMongo, CreateDatabaseSpec{Name: "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildCreateDatabaseStatements(tc.typ, tc.spec); err == nil {
				t.Errorf("Build(%v, %+v) succeeded, want error", tc.typ, tc.spec)
			}
		})
	}
}

func TestGroupMySQLCollations(t *testing.T) {
	res := &QueryResult{
		Columns: []string{"CHARACTER_SET_NAME", "COLLATION_NAME", "IS_DEFAULT"},
		Rows: [][]string{
			{"latin1", "latin1_bin", ""},
			{"latin1", "latin1_swedish_ci", "YES"},
			{"utf8mb4", "utf8mb4_bin", ""},
			{"utf8mb4", "utf8mb4_general_ci", "YES"},
		},
	}
	got := GroupMySQLCollations(res)
	if len(got.Charsets) != 2 {
		t.Fatalf("charsets = %d, want 2", len(got.Charsets))
	}
	first := got.Charsets[0]
	if first.Name != "latin1" || first.DefaultCollation != "latin1_swedish_ci" || len(first.Collations) != 2 {
		t.Errorf("first = %+v", first)
	}
	if got.Charsets[1].Name != "utf8mb4" || got.Charsets[1].DefaultCollation != "utf8mb4_general_ci" {
		t.Errorf("second = %+v", got.Charsets[1])
	}
	if len(got.Encodings) != 0 {
		t.Errorf("encodings = %v, want empty", got.Encodings)
	}
}
