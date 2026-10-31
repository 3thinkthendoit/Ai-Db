package db

import (
	"sync"
	"testing"
	"time"
)

func TestPostgresSetActiveSchemaEmptyOK(t *testing.T) {
	d := &postgresDriver{activeDB: "app"}
	if err := d.SetActiveSchema(""); err != nil {
		t.Fatal(err)
	}
	if d.activeDB != "app" {
		t.Fatalf("activeDB=%q", d.activeDB)
	}
}

func TestManagerSetActiveSchemaBusy(t *testing.T) {
	m := NewManager()
	d := &postgresDriver{activeDB: "app"}
	m.entries["c1"] = &entry{driver: d, refs: 1}
	prev := setActiveSchemaWait
	setActiveSchemaWait = 40 * time.Millisecond
	defer func() { setActiveSchemaWait = prev }()
	err := m.SetActiveSchema("c1", "other")
	if err == nil {
		t.Fatal("want busy error")
	}
}

func TestManagerSetActiveSchemaWaitsForIdle(t *testing.T) {
	m := NewManager()
	d := &postgresDriver{activeDB: "app"}
	m.entries["c1"] = &entry{driver: d, refs: 1}
	go func() {
		time.Sleep(30 * time.Millisecond)
		m.mu.Lock()
		m.entries["c1"].refs = 0
		m.mu.Unlock()
	}()
	prev := setActiveSchemaWait
	setActiveSchemaWait = 500 * time.Millisecond
	defer func() { setActiveSchemaWait = prev }()
	if err := m.SetActiveSchema("c1", ""); err != nil {
		t.Fatal(err)
	}
}

func TestManagerSetActiveSchemaClearsSwitching(t *testing.T) {
	m := NewManager()
	d := &postgresDriver{activeDB: "app"}
	m.entries["c1"] = &entry{driver: d}
	if err := m.SetActiveSchema("c1", ""); err != nil {
		t.Fatal(err)
	}
	if m.entries["c1"].switching {
		t.Fatal("switching still set")
	}
	if m.entries["c1"].refs != 0 {
		t.Fatalf("refs=%d after switch, want 0", m.entries["c1"].refs)
	}
}

// slowSwitchDriver blocks inside SetActiveSchema so tests can race Acquire.
type slowSwitchDriver struct {
	Driver
	started chan struct{}
	release chan struct{}
}

func (s *slowSwitchDriver) SetActiveSchema(schema string) error {
	close(s.started)
	<-s.release
	return nil
}

func TestManagerAcquireWaitsDuringSwitch(t *testing.T) {
	m := NewManager()
	started := make(chan struct{})
	release := make(chan struct{})
	m.entries["c1"] = &entry{driver: &slowSwitchDriver{
		Driver:  &postgresDriver{activeDB: "app"},
		started: started,
		release: release,
	}}

	prevAcq := acquireWait
	acquireWait = 500 * time.Millisecond
	defer func() { acquireWait = prevAcq }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = m.SetActiveSchema("c1", "other")
	}()
	<-started

	done := make(chan struct{})
	go func() {
		drv, rel, err := m.Acquire("c1")
		if err != nil {
			t.Errorf("Acquire during switch: %v", err)
		} else {
			rel()
			_ = drv
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Acquire returned before switch finished")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Acquire did not complete after switch")
	}
	wg.Wait()
}

func TestManagerAcquireBusyWhenSwitchStuck(t *testing.T) {
	m := NewManager()
	m.entries["c1"] = &entry{driver: &postgresDriver{activeDB: "app"}, switching: true}
	prev := acquireWait
	acquireWait = 40 * time.Millisecond
	defer func() { acquireWait = prev }()
	_, _, err := m.Acquire("c1")
	if err == nil {
		t.Fatal("want busy error while switching")
	}
}
