package main

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// TestLimiterWindowBlockAndClear is the unit-level contract: a key blocks only
// after maxFails inside the window, unblocks once blockDur elapses, and a success
// clears the accumulated failures. now is injected so no real sleeping happens.
func TestLimiterWindowBlockAndClear(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLoginLimiter(2, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	l.recordFailure("k")
	if _, blocked := l.blocked("k"); blocked {
		t.Fatal("blocked after 1 failure, want allowed")
	}
	l.recordFailure("k") // reaches maxFails → block
	wait, blocked := l.blocked("k")
	if !blocked {
		t.Fatal("not blocked after reaching maxFails")
	}
	if wait <= 0 || wait > 10*time.Minute {
		t.Errorf("wait = %s, want in (0, 10m]", wait)
	}

	now = now.Add(11 * time.Minute) // block expired
	if _, blocked := l.blocked("k"); blocked {
		t.Error("still blocked after blockDur elapsed")
	}

	l.recordFailure("k")
	l.recordFailure("k")
	l.recordSuccess("k")
	if _, blocked := l.blocked("k"); blocked {
		t.Error("blocked after recordSuccess cleared the bucket")
	}
}

// TestLimiterWindowResetsStaleCount verifies failures spaced beyond the window do
// not accumulate into a block (a slow, legitimate typo pattern must never lock out).
func TestLimiterWindowResetsStaleCount(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLoginLimiter(2, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	l.recordFailure("k")
	now = now.Add(2 * time.Minute) // beyond the 1m window
	l.recordFailure("k")           // starts a fresh count, not the 2nd of a pair
	if _, blocked := l.blocked("k"); blocked {
		t.Error("blocked despite failures falling in separate windows")
	}
}

// TestLimiterEvictsStaleBuckets verifies the memory bound: a bucket whose window
// has fully lapsed (and that is not blocked) is dropped on the next lookup, and
// recordFailure sweeps stale entries once the map hits maxBuckets, so a spray of
// distinct one-shot keys cannot grow the map without bound.
func TestLimiterEvictsStaleBuckets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newLoginLimiter(5, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	// Opportunistic eviction: a stale, unblocked bucket is removed on lookup.
	l.recordFailure("stale")
	now = now.Add(2 * time.Minute) // beyond the 1m window
	if _, blocked := l.blocked("stale"); blocked {
		t.Fatal("stale bucket reported blocked")
	}
	if _, ok := l.buckets["stale"]; ok {
		t.Error("stale unblocked bucket was not evicted on lookup")
	}

	// Sweep on overflow: fill past maxBuckets with distinct stale keys, then trip
	// recordFailure and confirm the map was pruned back under the cap.
	now = time.Unix(1_700_000_000, 0)
	for i := 0; i < maxBuckets+50; i++ {
		l.recordFailure("k" + strconv.Itoa(i))
	}
	now = now.Add(2 * time.Minute) // every window above has now lapsed
	l.recordFailure("trigger")
	if len(l.buckets) > maxBuckets {
		t.Errorf("map size = %d after sweep, want <= %d", len(l.buckets), maxBuckets)
	}
}

// TestNilLimiterIsNoop guards the desktop/test path: a nil limiter never blocks
// and never panics, so handlers that skip construction stay safe.
func TestNilLimiterIsNoop(t *testing.T) {
	var l *loginLimiter
	l.recordFailure("k")
	l.recordSuccess("k")
	if wait, blocked := l.blocked("k"); blocked || wait != 0 {
		t.Errorf("nil limiter: blocked=%v wait=%s, want false/0", blocked, wait)
	}
}

// TestLoginRateLimited429 drives the handler: after maxFails wrong passwords the
// next attempt — even with the correct password — is refused with 429 + Retry-After
// before any bcrypt work, proving the throttle sits in front of verification.
func TestLoginRateLimited429(t *testing.T) {
	s := newLoginTestServer(t, usersWith(t, "alice", "right"))
	s.loginLimit = newLoginLimiter(3, time.Minute, 5*time.Minute)
	s.loginIPLimit = newLoginLimiter(100, time.Minute, 5*time.Minute) // high, so the per-user bucket trips first
	s.audit = newAuditLog()

	for i := 0; i < 3; i++ {
		if rec := doLogin(t, s, "alice", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i, rec.Code)
		}
	}
	rec := doLogin(t, s, "alice", "right")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after threshold: got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}
}

// TestLoginSuccessClearsLimiter verifies a good login resets the failure count, so
// intermittent typos never accumulate into a lockout for a legitimate user.
func TestLoginSuccessClearsLimiter(t *testing.T) {
	s := newLoginTestServer(t, usersWith(t, "alice", "right"))
	s.loginLimit = newLoginLimiter(3, time.Minute, 5*time.Minute)
	s.loginIPLimit = newLoginLimiter(100, time.Minute, 5*time.Minute)
	s.audit = newAuditLog()

	doLogin(t, s, "alice", "wrong")
	doLogin(t, s, "alice", "wrong")
	if rec := doLogin(t, s, "alice", "right"); rec.Code != http.StatusOK {
		t.Fatalf("success: got %d, want 200", rec.Code)
	}
	// The bucket was cleared, so two more failures still leave room before a block.
	doLogin(t, s, "alice", "wrong")
	doLogin(t, s, "alice", "wrong")
	if rec := doLogin(t, s, "alice", "right"); rec.Code != http.StatusOK {
		t.Fatalf("post-clear success: got %d, want 200 (limiter wrongly stuck)", rec.Code)
	}
}
