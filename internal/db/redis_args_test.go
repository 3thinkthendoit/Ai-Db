package db

import "testing"

func TestSplitRedisArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"GET foo", []string{"GET", "foo"}},
		{`GET "foo bar"`, []string{"GET", "foo bar"}},
		{`DEL 'a b'`, []string{"DEL", "a b"}},
		{`SET "a\"b" 1`, []string{"SET", `a"b`, "1"}},
		{"  PING  ", []string{"PING"}},
	}
	for _, c := range cases {
		got := splitRedisArgs(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %v want %v", c.in, got, c.want)
			}
		}
	}
}
