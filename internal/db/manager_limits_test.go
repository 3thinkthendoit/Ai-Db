package db

import (
	"testing"
	"time"
)

// stubDriver embeds Driver (so it satisfies the interface) and records Close.
// Only Close is ever called in these tests; other methods would panic if used.
type stubDriver struct {
	Driver
	closed bool
}

func (s *stubDriver) Close() error {
	s.closed = true
	return nil
}

func TestCheckCapsPerOwnerAndGlobal(t *testing.T) {
	m := NewManager()
	m.limits = Limits{MaxGlobal: 2, MaxPerOwner: 1}
	m.entries["a"] = &entry{driver: &stubDriver{}, owner: "u1"}

	// u1 already holds its single allowed connection.
	if err := m.checkCapsLocked("u1"); err == nil {
		t.Error("expected per-owner cap to block u1")
	}
	// A different owner is under both caps (1 global entry, 0 for u2).
	if err := m.checkCapsLocked("u2"); err != nil {
		t.Errorf("expected u2 to be allowed, got %v", err)
	}
	// Fill to the global cap; now even a fresh owner is blocked.
	m.entries["b"] = &entry{driver: &stubDriver{}, owner: "u2"}
	if err := m.checkCapsLocked("u3"); err == nil {
		t.Error("expected global cap to block u3")
	}
}

func TestCheckCapsZeroIsUnlimited(t *testing.T) {
	m := NewManager() // zero Limits, the desktop posture
	for i := 0; i < 50; i++ {
		m.entries[string(rune('a'+i%26))+string(rune('0'+i/26))] = &entry{driver: &stubDriver{}, owner: "u1"}
	}
	if err := m.checkCapsLocked("u1"); err != nil {
		t.Errorf("zero limits must be unlimited, got %v", err)
	}
}

func TestReapIdleClosesStaleOnly(t *testing.T) {
	m := NewManager()
	stale := &stubDriver{}
	fresh := &stubDriver{}
	busy := &stubDriver{}
	switching := &stubDriver{}
	now := time.Now()
	old := now.Add(-2 * time.Minute)
	m.entries["stale"] = &entry{driver: stale, owner: "u1", lastUse: old}
	m.entries["fresh"] = &entry{driver: fresh, owner: "u1", lastUse: now}
	m.entries["busy"] = &entry{driver: busy, owner: "u1", lastUse: old, refs: 1}
	m.entries["switching"] = &entry{driver: switching, owner: "u1", lastUse: old, switching: true}

	m.reapIdle(time.Minute)

	if !stale.closed {
		t.Error("stale idle driver should be closed")
	}
	if _, ok := m.entries["stale"]; ok {
		t.Error("stale entry should be removed")
	}
	for _, id := range []string{"fresh", "busy", "switching"} {
		if _, ok := m.entries[id]; !ok {
			t.Errorf("%s entry should survive reaping", id)
		}
	}
	if fresh.closed || busy.closed || switching.closed {
		t.Error("only stale, idle, non-switching drivers may be closed")
	}
}

func TestSetLimitsStopIdempotent(t *testing.T) {
	m := NewManager()
	m.SetLimits(Limits{IdleTTL: time.Minute}) // starts the reaper goroutine
	m.Stop()
	m.Stop() // must not panic on a double close
	// A manager with no limits never starts a reaper, so Stop is a safe no-op.
	NewManager().Stop()
}
