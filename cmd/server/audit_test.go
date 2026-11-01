package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// captureAudit redirects the standard logger into a buffer for the duration of a
// test so audit lines can be asserted, then restores stderr via cleanup.
func captureAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// TestAuditLoginEventsAndNoPasswordLeak asserts login success/failure produce
// structured AUDIT lines and — critically — never contain the password.
func TestAuditLoginEventsAndNoPasswordLeak(t *testing.T) {
	buf := captureAudit(t)
	s := newLoginTestServer(t, usersWith(t, "alice", "s3cret-pass"))
	s.loginLimit = newLoginLimiter(5, time.Minute, 5*time.Minute)
	s.loginIPLimit = newLoginLimiter(50, time.Minute, 5*time.Minute)
	s.audit = newAuditLog()

	doLogin(t, s, "alice", "wrong-pass")  // failure
	doLogin(t, s, "alice", "s3cret-pass") // success

	out := buf.String()
	if !strings.Contains(out, "AUDIT ") {
		t.Fatalf("no AUDIT line emitted: %s", out)
	}
	for _, want := range []string{`"event":"login"`, `"outcome":"failure"`, `"outcome":"success"`} {
		if !strings.Contains(out, want) {
			t.Errorf("audit log missing %s: %s", want, out)
		}
	}
	if strings.Contains(out, "s3cret-pass") || strings.Contains(out, "wrong-pass") {
		t.Errorf("audit leaked a password: %s", out)
	}
}

// TestNilAuditIsNoop guards the desktop/test path: a nil *auditLog never panics,
// so handlers that skip constructing one stay safe.
func TestNilAuditIsNoop(t *testing.T) {
	var a *auditLog
	a.loginSuccess(nil, "x", "user")
	a.loginFailure(nil, "x")
	a.loginBlocked(nil, "x", time.Minute)
	a.logout(nil, "x")
	a.rpc(nil, "x", "Connect", "ok", "")
	a.adminCreateUser(nil, "actor", "target", "user", "success", "")
	a.adminListUsers(nil, "actor", 0, "success")
}

// TestRPCOutcomeTagsEgress verifies an egress denial is promoted to its own
// greppable outcome (an SSRF probe signal) while other errors stay generic.
func TestRPCOutcomeTagsEgress(t *testing.T) {
	outcome, detail := rpcOutcome(errors.New(`egress: host "169.254.169.254" (169.254.169.254) is blocked by policy`))
	if outcome != "egress_denied" {
		t.Errorf("outcome = %q, want egress_denied", outcome)
	}
	if !strings.Contains(detail, "169.254.169.254") {
		t.Errorf("detail lost the egress message: %q", detail)
	}
	if o2, _ := rpcOutcome(errors.New("maxRows must be positive")); o2 != "error" {
		t.Errorf("outcome = %q, want error", o2)
	}
}

// TestRPCOutcomeTruncatesLongDetail verifies the memory/privacy bound: a verbose
// driver error (which can echo a SQL fragment) is clipped so the aggregated audit
// log never carries unbounded query data. A short error is left intact.
func TestRPCOutcomeTruncatesLongDetail(t *testing.T) {
	long := strings.Repeat("x", maxAuditDetail+80)
	_, detail := rpcOutcome(errors.New(long))
	if got := len([]rune(detail)); got != maxAuditDetail+1 { // +1 for the ellipsis rune
		t.Errorf("detail rune len = %d, want %d", got, maxAuditDetail+1)
	}
	if !strings.HasSuffix(detail, "…") {
		t.Errorf("truncated detail missing ellipsis: %q", detail)
	}

	short := "connection refused"
	if _, d := rpcOutcome(errors.New(short)); d != short {
		t.Errorf("short detail altered: %q, want %q", d, short)
	}
}
