package db

import "testing"

func TestPostgresSSLMode(t *testing.T) {
	cases := map[string]string{
		"":            "prefer",
		"  ":          "prefer",
		"bogus":       "prefer",
		"disable":     "disable",
		"Require":     "require",
		" verify-ca ": "verify-ca",
	}
	for in, want := range cases {
		if got := postgresSSLMode(in); got != want {
			t.Fatalf("postgresSSLMode(%q)=%q want %q", in, got, want)
		}
	}
}
