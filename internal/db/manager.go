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
	owner     string    // tenant that opened this driver ("local" on desktop)
	lastUse   time.Time // last Acquire/Get; drives idle reaping
	refs      int
	switching bool // catalog/DB swap in progress — Acquire must wait
}

// Limits bounds how many live drivers the Manager keeps. The zero value means
// unlimited with no idle reaping, which is what the desktop build uses (a single
// local user, no need to police itself). Only the server injects non-zero limits.
type Limits struct {
	MaxGlobal   int           // 0 = no global cap
	MaxPerOwner int           // 0 = no per-owner cap
	IdleTTL     time.Duration // 0 = never reap idle drivers
}

// Manager holds open drivers keyed by connection ID.
type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
	limits  Limits
	stop    chan struct{} // closed by Stop to end the reaper; nil until SetLimits
}

// NewManager creates an empty connection manager with no limits.
func NewManager() *Manager {
	return &Manager{entries: map[string]*entry{}}
}

// SetLimits applies connection caps and, when IdleTTL > 0, starts a background
// reaper that closes drivers idle past the TTL (only those with no in-flight refs
// and no schema switch underway). It is safe to call once at startup; the desktop
// build never calls it and therefore keeps the historical unlimited behaviour.
func (m *Manager) SetLimits(l Limits) {
	m.mu.Lock()
	m.limits = l
	start := l.IdleTTL > 0 && m.stop == nil
	if start {
		m.stop = make(chan struct{})
	}
	stop := m.stop
	m.mu.Unlock()
	if start {
		go m.reapLoop(stop, l.IdleTTL)
	}
}

// Stop ends the idle reaper, if one was started. It is idempotent.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
	m.mu.Unlock()
}

// reapLoop periodically closes drivers idle past ttl until stop is closed.
func (m *Manager) reapLoop(stop <-chan struct{}, ttl time.Duration) {
	interval := ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.reapIdle(ttl)
		}
	}
}

// reapIdle closes and forgets drivers unused for longer than ttl. Selection runs
// under the lock; the (potentially slow) Close calls run outside it.
func (m *Manager) reapIdle(ttl time.Duration) {
	now := time.Now()
	m.mu.Lock()
	var stale []Driver
	for id, e := range m.entries {
		if e.refs == 0 && !e.switching && now.Sub(e.lastUse) > ttl {
			stale = append(stale, e.driver)
			delete(m.entries, id)
		}
	}
	m.mu.Unlock()
	for _, d := range stale {
		_ = d.Close()
	}
}

// checkCapsLocked rejects a new driver when a global or per-owner cap is reached.
// Caps of zero mean unlimited. Called with m.mu held, after any same-id reconnect
// has already removed the old entry so a reconnect never counts against the cap.
func (m *Manager) checkCapsLocked(owner string) error {
	if m.limits.MaxGlobal > 0 && len(m.entries) >= m.limits.MaxGlobal {
		return fmt.Errorf("server connection limit reached (%d); disconnect an idle connection first", m.limits.MaxGlobal)
	}
	if m.limits.MaxPerOwner > 0 {
		n := 0
		for _, e := range m.entries {
			if e.owner == owner {
				n++
			}
		}
		if n >= m.limits.MaxPerOwner {
			return fmt.Errorf("per-user connection limit reached (%d); disconnect an idle connection first", m.limits.MaxPerOwner)
		}
	}
	return nil
}

// Connect opens (or reopens) a driver for the connection on behalf of owner.
// Fails if the existing connection still has in-flight operations or a DB switch,
// or if opening a new one would exceed the configured caps.
func (m *Manager) Connect(ctx context.Context, owner string, c config.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.entries[c.ID]; ok {
		if old.refs > 0 || old.switching {
			return fmt.Errorf("connection busy: finish running queries before reconnecting")
		}
		_ = old.driver.Close()
		delete(m.entries, c.ID)
	}
	if err := m.checkCapsLocked(owner); err != nil {
		return err
	}
	d, err := Open(ctx, c)
	if err != nil {
		return err
	}
	m.entries[c.ID] = &entry{driver: d, owner: owner, lastUse: time.Now()}
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
			e.lastUse = time.Now()
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
	e.lastUse = time.Now()
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
