package db

import "testing"

func TestBuildRollbackSQL_DeleteInsertUpdate(t *testing.T) {
	del := &RowSnapshot{
		Kind: "delete", Table: "t", QuoteStyle: "mysql",
		Columns: []string{"id", "name"}, PKColumns: []string{"id"},
		Rows: [][]string{{"1", "a"}},
	}
	stmts, err := BuildRollbackSQL(del)
	if err != nil || len(stmts) != 1 || stmts[0] != "INSERT INTO `t` (`id`, `name`) VALUES ('1', 'a')" {
		t.Fatalf("delete rollback: %v %v", stmts, err)
	}

	ins := &RowSnapshot{
		Kind: "insert", Table: "t", QuoteStyle: "mysql",
		Columns: []string{"id", "name"}, PKColumns: []string{"id"},
		Rows: [][]string{{"2", "b"}},
	}
	stmts, err = BuildRollbackSQL(ins)
	if err != nil || len(stmts) != 1 || stmts[0] != "DELETE FROM `t` WHERE `id` = '2'" {
		t.Fatalf("insert rollback: %v %v", stmts, err)
	}

	upd := &RowSnapshot{
		Kind: "update", Table: "t", QuoteStyle: "postgres",
		Columns: []string{"id", "name"}, PKColumns: []string{"id"},
		Rows: [][]string{{"3", "old"}},
	}
	stmts, err = BuildRollbackSQL(upd)
	want := `UPDATE "t" SET "id" = '3', "name" = 'old' WHERE "id" = '3'`
	if err != nil || len(stmts) != 1 || stmts[0] != want {
		t.Fatalf("update rollback: got %q want %q err=%v", stmts, want, err)
	}
}

func TestParseSimpleDML_Lowercase(t *testing.T) {
	d, err := ParseSimpleDML("delete from users where id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "delete" || d.Table != "users" {
		t.Fatalf("unexpected %+v", d)
	}
}

func TestQuoteLit_NullSentinel(t *testing.T) {
	if quoteLit(nullSentinel) != "NULL" {
		t.Fatal("sentinel should become SQL NULL")
	}
	if quoteLit("NULL") != "'NULL'" {
		t.Fatal("text NULL must stay quoted")
	}
}
