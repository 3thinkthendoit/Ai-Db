package db

import (
	"context"
	"fmt"
	"sync"

	"AI-DB/internal/config"
)

type entry struct {
	driver Driver
	refs   int
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
// Fails if the existing connection still has in-flight operations.
func (m *Manager) Connect(ctx context.Context, c config.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.entries[c.ID]; ok {
		if old.refs > 0 {
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
	if e.refs > 0 {
		return fmt.Errorf("connection busy: finish running queries before disconnecting")
	}
	err := e.driver.Close()
	delete(m.entries, id)
	return err
}

// Acquire borrows a driver and increments the in-flight refcount.
// Caller must invoke the returned release function.
func (m *Manager) Acquire(id string) (Driver, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil, nil, fmt.Errorf("connection not open: %s (connect first)", id)
	}
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
	return e.driver, release, nil
}

// Get returns an open driver without refcounting (prefer Acquire).
func (m *Manager) Get(id string) (Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil, fmt.Errorf("connection not open: %s (connect first)", id)
	}
	return e.driver, nil
}

// SetActiveSchema updates the UI-selected database/schema on an open driver.
func (m *Manager) SetActiveSchema(id, schema string) error {
	drv, err := m.Get(id)
	if err != nil {
		return err
	}
	drv.SetActiveSchema(schema)
	return nil
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
