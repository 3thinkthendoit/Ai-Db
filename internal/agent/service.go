package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AI-DB/internal/config"
	"AI-DB/internal/db"
)

// StepEvent is a single agent loop step for the UI.
type StepEvent struct {
	Kind    string `json:"kind"` // thinking|tool|tool_result|message|error
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
}

// ChatRequest is a user turn for the agent.
type ChatRequest struct {
	ConnectionID string    `json:"connectionId"`
	Messages     []Message `json:"messages"`
}

// ChatResponse is the agent turn result.
type ChatResponse struct {
	Messages []Message   `json:"messages"`
	Steps    []StepEvent `json:"steps"`
	Final    string      `json:"final"`
}

// Service orchestrates multi-step tool-calling agent loops.
type Service struct {
	mgr *db.Manager
	// queryTimeout bounds each individual tool operation (query/list/describe).
	// Zero means no timeout, which is the desktop default.
	queryTimeout time.Duration
}

// NewService creates an agent service.
func NewService(mgr *db.Manager) *Service {
	return &Service{mgr: mgr}
}

// SetQueryTimeout bounds every tool operation the agent runs. The server sets it
// from its hardening config; the desktop leaves it at zero (unbounded).
func (s *Service) SetQueryTimeout(d time.Duration) {
	s.queryTimeout = d
}

// Run executes the agent loop until the model stops calling tools or hits the step
// limit. confirmWrites must be true (user confirmed this turn) for writes even when
// llmCfg.AllowWrites is on. It is the non-streaming entry point (the desktop Wails
// binding calls it) and is exactly RunStream with a nil emitter.
func (s *Service) Run(ctx context.Context, llmCfg config.LLMConfig, conn config.Connection, history []Message, userText string, confirmWrites bool, preferredSchema string) (*ChatResponse, error) {
	return s.RunStream(ctx, llmCfg, conn, history, userText, confirmWrites, preferredSchema, nil)
}

// RunStream is Run with an optional emit callback invoked for each StepEvent as it
// happens, letting a caller (the server's SSE endpoint) surface live progress
// instead of waiting for the whole multi-step loop to finish. A nil emit behaves
// identically to Run, and the returned *ChatResponse is the same either way — so
// the desktop path (Run) and its Wails binding are unchanged. Token-level LLM
// streaming is intentionally out of scope: the client is non-streaming, so the
// granularity here is one event per agent step (thinking / tool / result / final).
func (s *Service) RunStream(ctx context.Context, llmCfg config.LLMConfig, conn config.Connection, history []Message, userText string, confirmWrites bool, preferredSchema string, emit func(StepEvent)) (*ChatResponse, error) {
	if _, err := s.mgr.Get(conn.ID); err != nil {
		return nil, fmt.Errorf("open the connection before using Agent: %w", err)
	}
	maxSteps := llmCfg.MaxAgentSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}
	allowWrite := llmCfg.AllowWrites && confirmWrites
	if preferredSchema = strings.TrimSpace(preferredSchema); preferredSchema != "" {
		if err := s.mgr.SetActiveSchema(conn.ID, preferredSchema); err != nil {
			return nil, fmt.Errorf("switch database/schema %q: %w", preferredSchema, err)
		}
	}
	runner := NewToolRunner(s.mgr, conn.ID, conn.Type, allowWrite, llmCfg.MaxRows)
	runner.queryTimeout = s.queryTimeout
	client := NewClient(llmCfg)

	effective := llmCfg
	effective.AllowWrites = allowWrite
	system := buildSystemPrompt(conn, effective, preferredSchema)
	messages := make([]Message, 0, len(history)+4)
	messages = append(messages, Message{Role: "system", Content: system})
	for _, m := range history {
		if m.Role == "system" {
			continue
		}
		messages = append(messages, m)
	}
	messages = append(messages, Message{Role: "user", Content: userText})

	out := &ChatResponse{Messages: []Message{}, Steps: []StepEvent{}}
	tools := runner.Specs()
	// push records a step in the response and, when streaming, emits it live.
	push := func(ev StepEvent) {
		out.Steps = append(out.Steps, ev)
		if emit != nil {
			emit(ev)
		}
	}

	for step := 0; step < maxSteps; step++ {
		msg, err := client.Chat(ctx, messages, tools)
		if err != nil {
			push(StepEvent{Kind: "error", Content: err.Error()})
			return out, err
		}
		messages = append(messages, msg)

		if think := strings.TrimSpace(msg.ReasoningContent); think != "" {
			push(StepEvent{Kind: "thinking", Content: think})
		} else if len(msg.ToolCalls) > 0 {
			// Some models put interim thoughts in content alongside tool_calls.
			if think := strings.TrimSpace(msg.Content); think != "" {
				push(StepEvent{Kind: "thinking", Content: think})
			}
		}

		if len(msg.ToolCalls) == 0 {
			out.Final = msg.Content
			out.Messages = append(out.Messages, Message{Role: "assistant", Content: msg.Content})
			push(StepEvent{Kind: "message", Content: msg.Content})
			return out, nil
		}

		for _, tc := range msg.ToolCalls {
			push(StepEvent{Kind: "tool", Name: tc.Function.Name, Content: tc.Function.Arguments})
			result, err := runner.Run(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			// Cap tool result size to keep context manageable.
			if len(result) > 12000 {
				result = result[:12000] + "...(truncated)"
			}
			push(StepEvent{Kind: "tool_result", Name: tc.Function.Name, Content: result})
			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}

	out.Final = "Agent stopped: max steps reached. Try a more specific question."
	out.Messages = append(out.Messages, Message{Role: "assistant", Content: out.Final})
	push(StepEvent{Kind: "message", Content: out.Final})
	return out, nil
}

func buildSystemPrompt(conn config.Connection, llm config.LLMConfig, preferredSchema string) string {
	mode := "READ-ONLY"
	if llm.AllowWrites {
		mode = "WRITES ALLOWED (still prefer SELECT/read; confirm destructive ops carefully)"
	}
	var b strings.Builder
	b.WriteString("You are AI-DB, a database assistant embedded in a desktop client.\n")
	b.WriteString("Use tools to inspect schema and run queries. Prefer concise answers.\n")
	b.WriteString(fmt.Sprintf("Connection name: %s\n", conn.Name))
	b.WriteString(fmt.Sprintf("Engine: %s\n", conn.Type))
	b.WriteString(fmt.Sprintf("Host: %s  Database/DB index: %s\n", conn.Host, conn.Database))
	if s := strings.TrimSpace(preferredSchema); s != "" {
		switch conn.Type {
		case config.ConnPostgres:
			b.WriteString(fmt.Sprintf("User-selected PostgreSQL database (catalog): %s\n", s))
			b.WriteString("list_schemas returns databases. list_tables/describe_table use schema= as a namespace (default public). Pass database= to inspect another catalog temporarily (does not change the workbench database). Qualify SQL as schema.table against the selected catalog.\n")
		case config.ConnRedis:
			b.WriteString(fmt.Sprintf("User-selected Redis logical DB index: %s\n", s))
			b.WriteString("Keys/commands run against this logical DB. Do not assume DB 0 unless the user selected it.\n")
		default:
			b.WriteString(fmt.Sprintf("User-selected database/schema: %s\n", s))
			b.WriteString("Prefer this schema for list_tables/describe and qualify SQL accordingly (MySQL: USE when helpful).\n")
		}
	}
	b.WriteString(fmt.Sprintf("Safety mode: %s\n", mode))
	b.WriteString(fmt.Sprintf("Result row limit: %d\n", llm.MaxRows))
	b.WriteString("When answering, cite what you queried. Do not invent schema.\n")
	return b.String()
}
