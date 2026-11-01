package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginLimiter throttles repeated failed logins to blunt brute-force and
// credential-stuffing. Each key accumulates failures inside a sliding window;
// once maxFails is reached the key is blocked for blockDur and the window
// resets. A successful login clears its key.
//
// It is server-only (the desktop build never constructs one) and a nil
// *loginLimiter is a no-op, so handlers stay nil-safe in tests. All methods are
// safe for concurrent use.
type loginLimiter struct {
	mu       sync.Mutex
	maxFails int
	window   time.Duration
	blockDur time.Duration
	now      func() time.Time // injectable for tests
	buckets  map[string]*failBucket
}

// maxBuckets bounds the limiter's memory. A distributed attacker can mint many
// distinct (IP+username) keys that fail once and never return; once the map
// reaches this size, recordFailure sweeps entries whose window has lapsed and
// that are not actively blocked, so live throttling state is never evicted.
const maxBuckets = 10000

type failBucket struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

// newLoginLimiter builds a limiter, substituting sane defaults for any
// non-positive knob so a misconfigured env value cannot disable throttling.
func newLoginLimiter(maxFails int, window, blockDur time.Duration) *loginLimiter {
	if maxFails <= 0 {
		maxFails = 5
	}
	if window <= 0 {
		window = 10 * time.Minute
	}
	if blockDur <= 0 {
		blockDur = 15 * time.Minute
	}
	return &loginLimiter{
		maxFails: maxFails,
		window:   window,
		blockDur: blockDur,
		now:      time.Now,
		buckets:  make(map[string]*failBucket),
	}
}

// blocked reports whether key is currently locked out and, if so, how long the
// client should wait before retrying (rounded up to a whole second so the
// Retry-After header is never "0").
func (l *loginLimiter) blocked(key string) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		return 0, false
	}
	now := l.now()
	if now.Before(b.blockedUntil) {
		wait := b.blockedUntil.Sub(now).Round(time.Second)
		if wait < time.Second {
			wait = time.Second
		}
		return wait, true
	}
	// Not blocked. If the window has fully lapsed this bucket is dead weight;
	// drop it opportunistically so stale keys do not accumulate.
	if now.Sub(b.windowStart) > l.window {
		delete(l.buckets, key)
	}
	return 0, false
}

// recordFailure registers one failed attempt for key. Failures older than the
// window start a fresh count; reaching maxFails blocks the key for blockDur.
func (l *loginLimiter) recordFailure(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.buckets) >= maxBuckets {
		l.sweepStale(now)
	}
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.windowStart) > l.window {
		b = &failBucket{windowStart: now}
		l.buckets[key] = b
	}
	b.count++
	if b.count >= l.maxFails {
		b.blockedUntil = now.Add(l.blockDur)
		b.count = 0
		b.windowStart = now
	}
}

// sweepStale drops buckets whose window has lapsed and that are not actively
// blocked. Callers must hold l.mu. It bounds memory under a spray of distinct
// keys without ever evicting a key that is still enforcing a block.
func (l *loginLimiter) sweepStale(now time.Time) {
	for k, b := range l.buckets {
		if now.Before(b.blockedUntil) {
			continue // still blocking; keep
		}
		if now.Sub(b.windowStart) > l.window {
			delete(l.buckets, k)
		}
	}
}

// recordSuccess clears a key so a legitimate login is never blocked by earlier
// typos, and drops the bucket to keep the map from growing without bound.
func (l *loginLimiter) recordSuccess(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// clientIP returns the host part of the direct peer address (port stripped). It
// deliberately does NOT trust X-Forwarded-For: a client could spoof it to evade
// the limiter or to poison another user's bucket. Operators behind a reverse
// proxy should terminate connections there and forward the real peer, or front
// the limiter with a proxy-level control.
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
