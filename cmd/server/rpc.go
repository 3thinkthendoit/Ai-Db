package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"AI-DB/internal/agent"
	"AI-DB/internal/app"
	"AI-DB/internal/auth"
	"AI-DB/internal/config"
	"AI-DB/internal/db"
)

// This file maps every internal/app.Service method to a POST /api/{Method}
// endpoint so the browser transport in frontend/src/lib/api.ts can drive the same
// service layer as the desktop Wails bridge. The wire contract mirrors the
// generated Wails bindings one-for-one: the body is {args:[...]} with positional
// arguments matching App.d.ts, and the response is the binding's return value.
//
// Error semantics deliberately match each desktop binding so the frontend needs
// no per-transport branching:
//   - Bindings returning (T, error) reject on failure → here a non-2xx {error},
//     which api.ts turns into a thrown Error (same as a bridge reject).
//   - Bindings with NO error return (ListConnections, GetLLMConfig,
//     ListLLMProfiles, WritesAllowed, IsMutatingQuery, TestLLMProfile) swallow
//     service errors and resolve with the zero value → here HTTP 200 + zero JSON.

// rpcFunc is one bound method's implementation: decode its positional args, call
// the service with the authenticated principal, return the JSON-able result (or
// an error to surface as a non-2xx).
type rpcFunc func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error)

// badRequestError marks an argument-decoding failure so handleRPC answers 400
// (malformed envelope from the client) instead of 500.
type badRequestError struct{ err error }

func (e *badRequestError) Error() string { return e.err.Error() }
func (e *badRequestError) Unwrap() error { return e.err }

// maxRPCBodyBytes caps a single RPC request body. All 23 endpoints share
// decodeArgs, so this one limit protects the whole authenticated surface: without
// it a tenant could POST a multi-GB {args:[...]} envelope and exhaust the heap of
// the shared server process, taking down every other tenant. 4 MiB is ample for
// the largest legitimate payload (an agent chat history).
const maxRPCBodyBytes = 4 << 20 // 4 MiB

// decodeArgs reads the {args:[...]} envelope into positional raw messages.
func decodeArgs(r *http.Request) ([]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxRPCBodyBytes)
	var env struct {
		Args []json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		return nil, err
	}
	return env.Args, nil
}

// argString decodes args[i] as a string.
func argString(args []json.RawMessage, i int) (string, error) {
	return argJSON[string](args, i)
}

// argJSON decodes args[i] into T, reporting a 400-class error when the argument
// is absent or malformed.
func argJSON[T any](args []json.RawMessage, i int) (T, error) {
	var v T
	if i >= len(args) {
		return v, &badRequestError{fmt.Errorf("missing argument %d", i)}
	}
	if err := json.Unmarshal(args[i], &v); err != nil {
		return v, &badRequestError{err}
	}
	return v, nil
}

// handleRPC wraps an rpcFunc with the session gate, principal injection, arg
// envelope decoding and the standard error contract.
func (s *server) handleRPC(method string, fn rpcFunc) http.Handler {
	return s.gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		out, err := fn(r.Context(), p, args)
		if err != nil {
			status := http.StatusInternalServerError
			var bre *badRequestError
			if errors.As(err, &bre) {
				status = http.StatusBadRequest
			}
			msg := err.Error()
			// Sanitize persistence-layer (infrastructure) faults so a transient
			// backend failure never leaks internal addresses to a tenant; the detail
			// goes to the server log instead. Business/validation errors and
			// user-facing results (e.g. a TestConnection probe) pass through verbatim,
			// mirroring the desktop Wails bridge error contract.
			internal := app.IsInternalError(err)
			if status == http.StatusInternalServerError && internal {
				log.Printf("rpc %s: internal store error: %v", method, err)
				msg = "内部服务错误"
			}
			// Every failed action is audited (egress blocks, forbidden ids, rejected
			// writes are exactly what an operator wants). The audit detail never
			// carries the raw infrastructure message — only the sanitised outcome.
			outcome, detail := rpcOutcome(err)
			if internal {
				detail = "internal error"
			}
			s.audit.rpc(r, p.Username, method, outcome, detail)
			writeJSON(w, status, map[string]string{"error": msg})
			return
		}
		// Success is audited only for mutating / egress-probing methods; pure reads
		// are skipped to keep the log high-signal.
		if auditedRPCMethods[method] {
			s.audit.rpc(r, p.Username, method, "ok", "")
		}
		writeJSON(w, http.StatusOK, out)
	}))
}

// route registers POST /api/{method} guarded by the session cookie.
func (s *server) route(mux *http.ServeMux, method string, fn rpcFunc) {
	mux.Handle("POST /api/"+method, s.handleRPC(method, fn))
}

// —— request/response DTOs ——
// These mirror the package-main Wails DTOs (identical JSON tags) so the browser
// payload — shaped by the desktop bindings — decodes unchanged. They are
// redeclared here because cmd/server cannot import package main; keeping the
// Wails DTOs in package main preserves the main.* binding namespace, which is
// the desktop zero-regression guarantee.

type saveLLMProfileRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
	ClearKey bool   `json:"clearKey"`
}

type agentChatRequest struct {
	ConnectionID    string          `json:"connectionId"`
	History         []agent.Message `json:"history"`
	Message         string          `json:"message"`
	ConfirmWrites   bool            `json:"confirmWrites"`
	PreferredSchema string          `json:"preferredSchema"`
}

type llmTestResult struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Model      string `json:"model"`
	DurationMs int64  `json:"durationMs"`
}

// registerAPIRoutes maps all 23 bound service methods to HTTP endpoints. The
// order and signatures mirror frontend/wailsjs/go/main/App.d.ts. Every handler
// runs behind the session middleware and re-verifies connection ownership inside
// the service layer (findConnLocked), so a tenant can only ever reach their own
// connections.
func (s *server) registerAPIRoutes(mux *http.ServeMux) {
	// —— connections ——
	s.route(mux, "ListConnections", func(ctx context.Context, p app.Principal, _ []json.RawMessage) (any, error) {
		out, err := s.svc.ListConnections(ctx, p)
		if err != nil || out == nil {
			return []config.Connection{}, nil // binding has no error → empty slice, never null
		}
		return out, nil
	})
	s.route(mux, "UpsertConnection", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		c, err := argJSON[config.Connection](args, 0)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.UpsertConnection(ctx, p, c)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
	s.route(mux, "DeleteConnection", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.DeleteConnection(ctx, p, id)
	})
	s.route(mux, "TestConnection", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		c, err := argJSON[config.Connection](args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.TestConnection(ctx, p, c)
	})
	s.route(mux, "Connect", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.Connect(ctx, p, id)
	})
	s.route(mux, "Disconnect", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.Disconnect(ctx, p, id)
	})

	// —— object tree / query ——
	s.route(mux, "ListObjects", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.ListObjects(ctx, p, connID)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
	s.route(mux, "ExpandObject", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		path, err := argJSON[[]string](args, 1)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.ExpandObject(ctx, p, connID, path)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
	s.route(mux, "ExecuteQuery", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		query, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.ExecuteQuery(ctx, p, connID, query)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
	s.route(mux, "CreateDatabaseOptions", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return s.svc.CreateDatabaseOptions(ctx, p, connID)
	})
	s.route(mux, "CreateDatabase", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		spec, err := argJSON[db.CreateDatabaseSpec](args, 1)
		if err != nil {
			return nil, err
		}
		return s.svc.CreateDatabase(ctx, p, connID, spec)
	})
	s.route(mux, "SetActiveSchema", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		schema, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.SetActiveSchema(ctx, p, connID, schema)
	})
	s.route(mux, "IsMutatingQuery", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		query, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		mutating, _ := s.svc.IsMutatingQuery(ctx, p, connID, query) // binding has no error → swallow
		return mutating, nil
	})
	s.route(mux, "WritesAllowed", func(ctx context.Context, p app.Principal, _ []json.RawMessage) (any, error) {
		allowed, _ := s.svc.WritesAllowed(ctx, p) // binding has no error → swallow
		return allowed, nil
	})

	// —— LLM ——
	s.route(mux, "GetLLMConfig", func(ctx context.Context, p app.Principal, _ []json.RawMessage) (any, error) {
		cfg, _ := s.svc.GetLLMConfig(ctx, p) // binding has no error → zero value
		return cfg, nil
	})
	s.route(mux, "ListLLMProfiles", func(ctx context.Context, p app.Principal, _ []json.RawMessage) (any, error) {
		out, err := s.svc.ListLLMProfiles(ctx, p)
		if err != nil || out == nil {
			return []config.LLMProfileView{}, nil // binding has no error → empty slice
		}
		return out, nil
	})
	s.route(mux, "SaveLLMProfile", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		req, err := argJSON[saveLLMProfileRequest](args, 0)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.SaveLLMProfile(ctx, p, req.ID, req.Name, req.BaseURL, req.Model, req.APIKey, req.ClearKey)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
	s.route(mux, "DeleteLLMProfile", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.DeleteLLMProfile(ctx, p, id)
	})
	s.route(mux, "ActivateLLMProfile", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.ActivateLLMProfile(ctx, p, id)
	})
	s.route(mux, "SaveLLMSafety", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		allowWrites, err := argJSON[bool](args, 0)
		if err != nil {
			return nil, err
		}
		maxRows, err := argJSON[int](args, 1)
		if err != nil {
			return nil, err
		}
		maxAgentSteps, err := argJSON[int](args, 2)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.SaveLLMSafety(ctx, p, allowWrites, maxRows, maxAgentSteps)
	})
	s.route(mux, "TestLLMProfile", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		profileID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		baseURL, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		model, err := argString(args, 2)
		if err != nil {
			return nil, err
		}
		apiKey, err := argString(args, 3)
		if err != nil {
			return nil, err
		}
		// TestLLMProfile never returns a Go error; the outcome rides in the struct
		// (mirrors the desktop binding's main.LLMTestResult).
		ok, message, resolvedModel, durationMs := s.svc.TestLLMProfile(ctx, p, profileID, baseURL, model, apiKey)
		return llmTestResult{OK: ok, Message: message, Model: resolvedModel, DurationMs: durationMs}, nil
	})
	s.route(mux, "SaveLLMConfig", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		llm, err := argJSON[config.LLMConfig](args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.SaveLLMConfig(ctx, p, llm)
	})

	// —— agent ——
	s.route(mux, "AgentChat", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		req, err := argJSON[agentChatRequest](args, 0)
		if err != nil {
			return nil, err
		}
		res, err := s.svc.AgentChat(ctx, p, req.ConnectionID, req.History, req.Message, req.ConfirmWrites, req.PreferredSchema)
		if err != nil {
			return nil, err
		}
		return res, nil
	})

	// —— mutation gate / DBA review (server-only behaviour; desktop has no store) ——
	s.route(mux, "ListMutations", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		status := ""
		if len(args) > 0 {
			status, _ = argString(args, 0)
		}
		limit := 100
		if len(args) > 1 {
			if n, err := argJSON[int](args, 1); err == nil {
				limit = n
			}
		}
		mineOnly := false
		if len(args) > 2 {
			if b, err := argJSON[bool](args, 2); err == nil {
				mineOnly = b
			}
		}
		out, err := s.svc.ListMutations(ctx, p, status, limit, mineOnly)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return []app.MutationView{}, nil
		}
		return out, nil
	})
	s.route(mux, "SubmitMutation", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		sqlText, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		title, remark, demand := "", "", ""
		if len(args) > 2 {
			title, _ = argString(args, 2)
		}
		if len(args) > 3 {
			remark, _ = argString(args, 3)
		}
		if len(args) > 4 {
			demand, _ = argString(args, 4)
		}
		return s.svc.SubmitMutation(ctx, p, connID, sqlText, title, remark, demand)
	})
	s.route(mux, "ListQueryAudit", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		limit := 100
		if len(args) > 0 {
			if n, err := argJSON[int](args, 0); err == nil {
				limit = n
			}
		}
		out, err := s.svc.ListQueryAudit(ctx, p, limit)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return []app.QueryAuditView{}, nil
		}
		return out, nil
	})
	s.route(mux, "ApproveMutation", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return nil, s.svc.ApproveMutation(ctx, p, id, note)
	})
	s.route(mux, "ExecuteMutation", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return s.svc.ExecuteMutation(ctx, p, id, note)
	})
	s.route(mux, "RejectMutation", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return nil, s.svc.RejectMutation(ctx, p, id, note)
	})
	s.route(mux, "RollbackMutation", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return s.svc.RollbackMutation(ctx, p, id, note)
	})

	// —— table-level query grants / privilege applications ——
	s.route(mux, "ListConnectionCatalog", func(ctx context.Context, p app.Principal, _ []json.RawMessage) (any, error) {
		out, err := s.svc.ListConnectionCatalog(ctx, p)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return []app.ConnectionCatalogItem{}, nil
		}
		return out, nil
	})
	s.route(mux, "ListGrants", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		limit := 200
		if len(args) > 0 {
			if n, err := argJSON[int](args, 0); err == nil {
				limit = n
			}
		}
		out, err := s.svc.ListGrants(ctx, p, limit)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return []app.GrantView{}, nil
		}
		return out, nil
	})
	s.route(mux, "ListPrivilegeRequests", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		status := ""
		if len(args) > 0 {
			status, _ = argString(args, 0)
		}
		limit := 200
		if len(args) > 1 {
			if n, err := argJSON[int](args, 1); err == nil {
				limit = n
			}
		}
		mineOnly := false
		if len(args) > 2 {
			if b, err := argJSON[bool](args, 2); err == nil {
				mineOnly = b
			}
		}
		out, err := s.svc.ListPrivilegeRequests(ctx, p, status, limit, mineOnly)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return []app.PrivilegeRequestView{}, nil
		}
		return out, nil
	})
	s.route(mux, "SubmitPrivilegeRequest", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		connID, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		dbName, err := argString(args, 1)
		if err != nil {
			return nil, err
		}
		tables, err := argString(args, 2)
		if err != nil {
			return nil, err
		}
		remark := ""
		if len(args) > 3 {
			remark, _ = argString(args, 3)
		}
		return nil, s.svc.SubmitPrivilegeRequest(ctx, p, connID, dbName, tables, remark)
	})
	s.route(mux, "ApprovePrivilegeRequest", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return nil, s.svc.ApprovePrivilegeRequest(ctx, p, id, note)
	})
	s.route(mux, "RejectPrivilegeRequest", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		note := ""
		if len(args) > 1 {
			note, _ = argString(args, 1)
		}
		return nil, s.svc.RejectPrivilegeRequest(ctx, p, id, note)
	})
	s.route(mux, "RevokeGrant", func(ctx context.Context, p app.Principal, args []json.RawMessage) (any, error) {
		id, err := argString(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, s.svc.RevokeGrant(ctx, p, id)
	})
}
