package agent

import (
	"context"
	"fmt"
	"strings"

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
}

// NewService creates an agent service.
func NewService(mgr *db.Manager) *Service {
	return &Service{mgr: mgr}
}

// Run executes the agent loop until the model stops calling tools or hits the step limit.
// confirmWrites must be true (user confirmed this turn) for writes even when llmCfg.AllowWrites is on.
func (s *Service) Run(ctx context.Context, llmCfg config.LLMConfig, conn config.Connection, history []Message, userText string, confirmWrites bool, preferredSchema string) (*ChatResponse, error) {
	if _, err := s.mgr.Get(conn.ID); err != nil {
		return nil, fmt.Errorf("open the connection before using Agent: %w", err)
	}
	maxSteps := llmCfg.MaxAgentSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}
	allowWrite := llmCfg.AllowWrites && confirmWrites
	if preferredSchema = strings.TrimSpace(preferredSchema); preferredSchema != "" {
		_ = s.mgr.SetActiveSchema(conn.ID, preferredSchema)
	}
	runner := NewToolRunner(s.mgr, conn.ID, conn.Type, allowWrite, llmCfg.MaxRows)
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

	for step := 0; step < maxSteps; step++ {
		msg, err := client.Chat(ctx, messages, tools)
		if err != nil {
			out.Steps = append(out.Steps, StepEvent{Kind: "error", Content: err.Error()})
			return out, err
		}
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			out.Final = msg.Content
			out.Messages = append(out.Messages, Message{Role: "assistant", Content: msg.Content})
			out.Steps = append(out.Steps, StepEvent{Kind: "message", Content: msg.Content})
			return out, nil
		}

		out.Steps = append(out.Steps, StepEvent{Kind: "thinking", Content: fmt.Sprintf("tool calls: %d", len(msg.ToolCalls))})
		for _, tc := range msg.ToolCalls {
			out.Steps = append(out.Steps, StepEvent{Kind: "tool", Name: tc.Function.Name, Content: tc.Function.Arguments})
			result, err := runner.Run(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			// Cap tool result size to keep context manageable.
			if len(result) > 12000 {
				result = result[:12000] + "...(truncated)"
			}
			out.Steps = append(out.Steps, StepEvent{Kind: "tool_result", Name: tc.Function.Name, Content: result})
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
	out.Steps = append(out.Steps, StepEvent{Kind: "message", Content: out.Final})
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
		b.WriteString(fmt.Sprintf("User-selected database/schema: %s\n", s))
		b.WriteString("Prefer this schema for list_tables/describe and qualify SQL accordingly (MySQL: USE when helpful).\n")
	}
	b.WriteString(fmt.Sprintf("Safety mode: %s\n", mode))
	b.WriteString(fmt.Sprintf("Result row limit: %d\n", llm.MaxRows))
	b.WriteString("When answering, cite what you queried. Do not invent schema.\n")
	return b.String()
}
