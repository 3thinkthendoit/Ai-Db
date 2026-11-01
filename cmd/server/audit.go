package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// auditLog emits security-relevant events as single-line JSON records prefixed
// with "AUDIT" so they are grep- and SIEM-friendly and never interleave mid-line.
// It deliberately logs identifiers and outcomes, never request bodies: bodies can
// carry connection passwords, LLM API keys and query data, none of which belong
// in a log. A failure detail may quote a short slice of the resulting error (which
// the authenticated caller already receives), so it is length-bounded to keep bulk
// data — including any SQL fragment a driver echoes — out of the aggregated log.
//
// It is server-only (the desktop build never constructs one) and a nil *auditLog
// is a no-op, so handlers stay nil-safe in tests. Writes are serialised by the
// standard log package, making it safe for concurrent use.
type auditLog struct {
	now func() time.Time // injectable for tests
}

func newAuditLog() *auditLog { return &auditLog{now: time.Now} }

// auditRecord is one audit line. Empty fields are omitted to keep lines compact.
type auditRecord struct {
	Time    string `json:"ts"`
	Event   string `json:"event"`
	Actor   string `json:"actor,omitempty"`
	Role    string `json:"role,omitempty"`
	IP      string `json:"ip,omitempty"`
	Method  string `json:"method,omitempty"`
	Target  string `json:"target,omitempty"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

func (a *auditLog) emit(rec auditRecord) {
	if a == nil {
		return
	}
	if rec.Time == "" {
		rec.Time = a.now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		log.Printf("audit: marshal failed: %v", err)
		return
	}
	log.Printf("AUDIT %s", line)
}

// —— auth events ——

func (a *auditLog) loginSuccess(r *http.Request, username, role string) {
	a.emit(auditRecord{Event: "login", Actor: username, Role: role, IP: clientIP(r), Outcome: "success"})
}

func (a *auditLog) loginFailure(r *http.Request, username string) {
	a.emit(auditRecord{Event: "login", Actor: username, IP: clientIP(r), Outcome: "failure", Detail: "bad credentials"})
}

func (a *auditLog) loginBlocked(r *http.Request, username string, wait time.Duration) {
	a.emit(auditRecord{Event: "login", Actor: username, IP: clientIP(r), Outcome: "rate_limited", Detail: "retry after " + wait.String()})
}

func (a *auditLog) logout(r *http.Request, username string) {
	a.emit(auditRecord{Event: "logout", Actor: username, IP: clientIP(r), Outcome: "success"})
}

// —— admin events ——

func (a *auditLog) adminCreateUser(r *http.Request, actor, target, role, outcome, detail string) {
	a.emit(auditRecord{Event: "admin_create_user", Actor: actor, IP: clientIP(r), Target: target, Role: role, Outcome: outcome, Detail: detail})
}

func (a *auditLog) adminListUsers(r *http.Request, actor string, count int, outcome string) {
	a.emit(auditRecord{Event: "admin_list_users", Actor: actor, IP: clientIP(r), Outcome: outcome, Detail: strconv.Itoa(count)})
}

// —— RPC events ——

// auditedRPCMethods are the mutating / egress-probing service methods whose
// SUCCESS is worth an audit line. Pure reads (ListConnections, ListObjects,
// GetLLMConfig, …) are excluded to keep the log high-signal; every method's
// FAILURE is audited regardless (see rpcOutcome), since a failed action — an
// egress block, a forbidden id, a rejected write — is exactly what an operator
// wants to see.
var auditedRPCMethods = map[string]bool{
	"UpsertConnection":   true,
	"DeleteConnection":   true,
	"Connect":            true,
	"Disconnect":         true,
	"SetActiveSchema":    true,
	"SaveLLMProfile":     true,
	"DeleteLLMProfile":   true,
	"ActivateLLMProfile": true,
	"SaveLLMSafety":      true,
	"SaveLLMConfig":      true,
	"AgentChat":          true,
	"TestConnection":     true,
	"TestLLMProfile":     true,
}

// rpc records an authenticated service call. detail is already-sanitised text
// (never a raw infrastructure error). An egress denial is promoted to its own
// outcome so SSRF probes are trivially greppable.
func (a *auditLog) rpc(r *http.Request, actor, method, outcome, detail string) {
	a.emit(auditRecord{Event: "rpc", Actor: actor, IP: clientIP(r), Method: method, Outcome: outcome, Detail: detail})
}

// rpcOutcome derives the audit outcome for a failed RPC. Egress blocks (the
// service layer prefixes them with "egress:") are tagged distinctly. The detail
// is length-bounded (see maxAuditDetail) so a verbose driver error cannot carry
// unbounded query data into the aggregated log.
func rpcOutcome(err error) (outcome, detail string) {
	msg := truncateDetail(err.Error())
	if strings.Contains(msg, "egress:") {
		return "egress_denied", msg
	}
	return "error", msg
}

// maxAuditDetail bounds how much of an error message reaches the audit log.
const maxAuditDetail = 200

// truncateDetail clips s to maxAuditDetail runes, appending an ellipsis when it
// cut anything, so audit lines stay compact and cannot smuggle bulk data.
func truncateDetail(s string) string {
	r := []rune(s)
	if len(r) <= maxAuditDetail {
		return s
	}
	return string(r[:maxAuditDetail]) + "…"
}
