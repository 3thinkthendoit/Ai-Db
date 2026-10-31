package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"AI-DB/internal/config"
)

type entry struct {
	driver    Driver
	refs      int
	switching bool // catalog/DB swap in progress — Acquire must wait
}

// Manager holds open drivers keyed by connection ID.
type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// NewManager creates an empty connection manager.
func NewManager() *Manager {
	return &Manager{entries: map[string]*entry{}}
}

// Connect opens (or reopens) a driver for the connection.
// Fails if the existing connection still has in-flight operations or a DB switch.
func (m *Manager) Connect(ctx context.Context, c config.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.entries[c.ID]; ok {
		if old.refs > 0 || old.switching {
			return fmt.Errorf("connection busy: finish running queries before reconnecting")
		}
		_ = old.driver.Close()
		delete(m.entries, c.ID)
	}
	d, err := Open(ctx, c)
	if err != nil {
		return err
	}
	m.entries[c.ID] = &entry{driver: d}
	return nil
}

// Disconnect closes an open driver.
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil
	}
	if e.refs > 0 || e.switching {
		return fmt.Errorf("connection busy: finish running queries before disconnecting")
	}
	err := e.driver.Close()
	delete(m.entries, id)
	return err
}

// acquireWait is how long Acquire polls while a catalog/DB switch is in progress.
var acquireWait = 3 * time.Second

// Acquire borrows a driver and increments the in-flight refcount.
// Waits briefly if a SetActiveSchema swap is in progress so callers never hold a
// pool/client that is about to be Closed.
// Caller must invoke the returned release function.
func (m *Manager) Acquire(id string) (Driver, func(), error) {
	deadline := time.Now().Add(acquireWait)
	for {
		m.mu.Lock()
		e, ok := m.entries[id]
		if !ok {
			m.mu.Unlock()
			return nil, nil, fmt.Errorf("connection not open: %s (connect first)", id)
		}
		if !e.switching {
			e.refs++
			released := false
			release := func() {
				m.mu.Lock()
				defer m.mu.Unlock()
				if released {
					return
				}
				released = true
				if ent, ok := m.entries[id]; ok && ent == e && ent.refs > 0 {
					ent.refs--
				}
			}
			drv := e.driver
			m.mu.Unlock()
			return drv, release, nil
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			return nil, nil, fmt.Errorf("connection busy: database switch in progress")
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// Get returns an open driver without refcounting (prefer Acquire).
func (m *Manager) Get(id string) (Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil, fmt.Errorf("connection not open: %s (connect first)", id)
	}
	if e.switching {
		return nil, fmt.Errorf("connection busy: database switch in progress")
	}
	return e.driver, nil
}

// setActiveSchemaWait is how long we poll for in-flight Acquire refs to drop before
// refusing a catalog/DB switch. Object-tree Expand/List often overlaps the UI schema effect.
var setActiveSchemaWait = 3 * time.Second

// SetActiveSchema updates the UI-selected database/schema on an open driver.
// Waits for in-flight Acquire refs to drop, then sets switching so concurrent
// Acquire cannot borrow a pool/client that is about to be replaced.
func (m *Manager) SetActiveSchema(id, schema string) error {
	deadline := time.Now().Add(setActiveSchemaWait)
	for {
		m.mu.Lock()
		e, ok := m.entries[id]
		if !ok {
			m.mu.Unlock()
			return fmt.Errorf("connection not open: %s (connect first)", id)
		}
		if e.refs == 0 && !e.switching {
			e.switching = true
			drv := e.driver
			m.mu.Unlock()
			err := drv.SetActiveSchema(schema)
			m.mu.Lock()
			if ent, ok := m.entries[id]; ok && ent == e {
				ent.switching = false
			}
			m.mu.Unlock()
			return err
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			return fmt.Errorf("connection busy: finish running queries before switching database")
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// CloseAll closes every open driver.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.entries {
		_ = e.driver.Close()
		delete(m.entries, id)
	}
}

// TestConnection opens a temporary driver, pings, and closes it.
func TestConnection(ctx context.Context, c config.Connection) error {
	d, err := Open(ctx, c)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Ping(ctx)
}
