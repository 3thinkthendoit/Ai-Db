package db

import (
	"context"
	"strings"
	"testing"
)

func TestFolderHelpers(t *testing.T) {
	my := mysqlSchemaFolders()
	if len(my) != 6 {
		t.Fatalf("mysql folders: got %d want 6", len(my))
	}
	for _, n := range my {
		if n.Kind != "folder" || !n.Lazy {
			t.Fatalf("folder node invalid: %+v", n)
		}
	}
	pg := postgresSchemaFolders()
	if len(pg) != 5 {
		t.Fatalf("postgres folders: got %d want 5", len(pg))
	}
	detail := tableDetailFolders()
	if len(detail) != 2 || detail[0].Name != CatColumns || detail[1].Name != CatIndexes {
		t.Fatalf("table detail folders: %+v", detail)
	}
	nodes := namesToNodes([]string{"a", "b"}, "table", true)
	if len(nodes) != 2 || nodes[0].Kind != "table" || !nodes[0].Lazy {
		t.Fatalf("namesToNodes: %+v", nodes)
	}
}

func TestEscapeSQLLiteral(t *testing.T) {
	if got := escapeSQLLiteral("a'b"); got != "a''b" {
		t.Fatalf("escape quote: %q", got)
	}
	if got := escapeSQLLiteral("plain"); got != "plain" {
		t.Fatalf("escape plain: %q", got)
	}
}

func TestMySQLExpandObjectPathNoDB(t *testing.T) {
	d := &mysqlDriver{}
	ctx := context.Background()

	folders, err := d.ExpandObject(ctx, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 6 || folders[0].Name != CatTables {
		t.Fatalf("schema expand: %+v", folders)
	}

	detail, err := d.ExpandObject(ctx, []string{"demo", CatTables, "users"})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail) != 2 || detail[0].Name != CatColumns {
		t.Fatalf("table expand: %+v", detail)
	}

	_, err = d.ExpandObject(ctx, []string{"demo", "nope"})
	if err == nil || !strings.Contains(err.Error(), "unsupported object path") {
		t.Fatalf("want bad path error, got %v", err)
	}
}

func TestPostgresExpandObjectPathNoDB(t *testing.T) {
	d := &postgresDriver{}
	ctx := context.Background()

	folders, err := d.ExpandObject(ctx, []string{"public"})
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 5 || folders[0].Name != CatTables {
		t.Fatalf("schema expand: %+v", folders)
	}

	_, err = d.ExpandObject(ctx, []string{"public", CatEvents})
	if err == nil {
		t.Fatal("postgres should reject mysql-only events path")
	}
}

func TestRedisExpandObjectBadPath(t *testing.T) {
	d := &redisDriver{}
	_, err := d.ExpandObject(context.Background(), []string{"db0", "extra"})
	if err == nil {
		t.Fatal("want bad path")
	}
}
