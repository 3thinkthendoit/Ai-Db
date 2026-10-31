package db

import (
	"testing"

	"AI-DB/internal/config"
)

func TestMongoAuthSource(t *testing.T) {
	if got := mongoAuthSource(config.Connection{}); got != "admin" {
		t.Fatalf("default: got %q", got)
	}
	if got := mongoAuthSource(config.Connection{AuthSource: "  appdb  "}); got != "appdb" {
		t.Fatalf("explicit: got %q", got)
	}
}

func TestShortenIndexDef(t *testing.T) {
	got := shortenIndexDef(`CREATE INDEX idx ON public.t USING btree ((lower(name)))`)
	if got != "(lower(name))" {
		t.Fatalf("expr index: got %q", got)
	}
	got = shortenIndexDef(`CREATE UNIQUE INDEX u ON public.t USING btree (a, b)`)
	if got != "a, b" {
		t.Fatalf("cols: got %q", got)
	}
	if shortenIndexDef("") != "" {
		t.Fatal("empty def")
	}
}
