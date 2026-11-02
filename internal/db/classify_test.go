package db

import "testing"

func TestClassifySQL(t *testing.T) {
	cases := []struct {
		q    string
		want StmtClass
	}{
		{"SELECT 1", StmtRead},
		{"INSERT INTO t (a) VALUES (1)", StmtDML},
		{"UPDATE t SET a=1 WHERE id=1", StmtDML},
		{"DELETE FROM t WHERE id=1", StmtDML},
		{"CREATE TABLE t (id INT)", StmtDDL},
		{"DROP TABLE t", StmtDDL},
		{"TRUNCATE t", StmtDDL},
	}
	for _, tc := range cases {
		if got := classifySQLText(tc.q); got != tc.want {
			t.Fatalf("%q: got %s want %s", tc.q, got, tc.want)
		}
	}
}

func TestParseSimpleDML(t *testing.T) {
	d, err := ParseSimpleDML("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "delete" || d.Table != "users" || d.WhereSQL == "" {
		t.Fatalf("unexpected %+v", d)
	}
	u, err := ParseSimpleDML("UPDATE `orders` SET x=1 WHERE id=2")
	if err != nil {
		t.Fatal(err)
	}
	if u.Kind != "update" || u.Table != "orders" {
		t.Fatalf("unexpected %+v", u)
	}
	ins, err := ParseSimpleDML("INSERT INTO app.items (name) VALUES ('a')")
	if err != nil {
		t.Fatal(err)
	}
	if ins.Kind != "insert" || ins.Table != "app.items" {
		t.Fatalf("unexpected %+v", ins)
	}
	if _, err := ParseSimpleDML("INSERT INTO t SELECT * FROM u"); err == nil {
		t.Fatal("expected INSERT SELECT rejection")
	}
}
