package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"AI-DB/internal/agent"
	"AI-DB/internal/app"
	"AI-DB/internal/auth"
)

// sseEvent is one Server-Sent-Events frame payload for the streaming agent
// endpoint. Kind is "step" for live per-step progress, "done" for the terminal
// frame carrying the full ChatResponse, or "error" for a terminal failure.
type sseEvent struct {
	Kind  string              `json:"kind"`
	Step  *agent.StepEvent    `json:"step,omitempty"`
	Final *agent.ChatResponse `json:"final,omitempty"`
	Error string              `json:"error,omitempty"`
}

// handleAgentChatStream runs the agent loop and streams each StepEvent as it
// happens (thinking / tool / tool_result / message), ending with a "done" frame
// holding the complete ChatResponse — or an "error" frame. It is the streaming
// sibling of POST /api/AgentChat and is server-only: the desktop uses the one-shot
// Wails binding, and this endpoint is deliberately not a bound method, so
// frontend/wailsjs stays unchanged.
//
// SSE is used rather than a WebSocket because it needs no extra dependency, rides
// a plain POST response, and the client (fetch + ReadableStream in lib/api.ts)
// drives it with the session cookie. Client disconnects cancel r.Context(), which
// aborts the in-flight agent loop.
func (s *server) handleAgentChatStream(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	args, err := decodeArgs(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req, err := argJSON[agentChatRequest](args, 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		// net/http always provides a Flusher; this guard only trips under an exotic
		// ResponseWriter. Fall back to the one-shot JSON result so the client still
		// gets an answer instead of a broken stream. The error handling mirrors the
		// SSE path and rpc.go: internal store errors are logged + generalized (never
		// leaked), and the outcome is audited.
		res, err := s.svc.AgentChat(r.Context(), p, req.ConnectionID, req.History, req.Message, req.ConfirmWrites, req.PreferredSchema)
		if err != nil {
			msg := err.Error()
			internal := app.IsInternalError(err)
			if internal {
				log.Printf("agent stream fallback: internal error: %v", err)
				msg = "内部服务错误"
			}
			outcome, detail := rpcOutcome(err)
			if internal {
				detail = "internal error"
			}
			s.audit.rpc(r, p.Username, "AgentChatStream", outcome, detail)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": msg})
			return
		}
		s.audit.rpc(r, p.Username, "AgentChatStream", "ok", "")
		writeJSON(w, http.StatusOK, res)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // defeat reverse-proxy (nginx) buffering
	w.WriteHeader(http.StatusOK)

	send := func(ev sseEvent) {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		// Ignore write errors: a disconnected client cancels r.Context(), which
		// unwinds the agent loop; there is nothing useful to do here.
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	res, runErr := s.svc.AgentChatStream(r.Context(), p, req.ConnectionID, req.History, req.Message, req.ConfirmWrites, req.PreferredSchema, func(step agent.StepEvent) {
		send(sseEvent{Kind: "step", Step: &step})
	})
	if runErr != nil {
		msg := runErr.Error()
		switch {
		case app.IsInternalError(runErr):
			log.Printf("agent stream: internal error: %v", runErr)
			s.audit.rpc(r, p.Username, "AgentChatStream", "error", "internal error")
			msg = "内部服务错误"
		default:
			outcome, detail := rpcOutcome(runErr)
			s.audit.rpc(r, p.Username, "AgentChatStream", outcome, detail)
		}
		send(sseEvent{Kind: "error", Error: msg})
		return
	}
	s.audit.rpc(r, p.Username, "AgentChatStream", "ok", "")
	send(sseEvent{Kind: "done", Final: res})
}

// isEventStream reports whether a response was served as SSE (used by tests).
func isEventStream(h http.Header) bool {
	return strings.HasPrefix(h.Get("Content-Type"), "text/event-stream")
}
