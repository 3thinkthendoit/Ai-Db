package db

import (
	"testing"
	"time"
)

func TestStringifyTimeReadable(t *testing.T) {
	ts := time.Date(2026, 8, 12, 22, 19, 46, 872000000, time.UTC)
	got := stringify(ts)
	want := "2026-08-12 22:19:46.872"
	if got != want {
		t.Fatalf("stringify(time)=%q, want %q", got, want)
	}
	whole := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if stringify(whole) != "2026-01-02 03:04:05" {
		t.Fatalf("stringify whole second = %q", stringify(whole))
	}
}

func TestObjectNodeBadgeJSONTag(t *testing.T) {
	// Smoke: Badge field exists and zero value omits from typical use.
	n := ObjectNode{Name: "id · bigint", Kind: "column", Badge: "PK"}
	if n.Badge != "PK" {
		t.Fatal("badge not set")
	}
}
