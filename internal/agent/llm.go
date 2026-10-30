package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"AI-DB/internal/config"
)

// Message is a chat message (OpenAI-compatible).
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is an OpenAI tool call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries name + JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec describes a tool for the model.
type ToolSpec struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

// ToolFunctionSchema is the JSON schema for a tool.
type ToolFunctionSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolSpec `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Client talks to an OpenAI-compatible Chat Completions API.
type Client struct {
	http *http.Client
	cfg  config.LLMConfig
}

// NewClient creates an LLM client.
func NewClient(cfg config.LLMConfig) *Client {
	return &Client{
		http: &http.Client{Timeout: 120 * time.Second},
		cfg:  cfg,
	}
}

// Chat sends a non-streaming chat completion request.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (Message, error) {
	if c.cfg.Model == "" {
		return Message{}, fmt.Errorf("LLM model is not configured")
	}
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	if base == "" {
		return Message{}, fmt.Errorf("LLM base URL is not configured")
	}
	// Cloud hosts need a key; local gateways (Ollama etc.) often do not.
	if c.cfg.APIKey == "" && !isLocalLLMBase(base) {
		return Message{}, fmt.Errorf("LLM API key is not configured")
	}
	url := base + "/chat/completions"

	body, err := json.Marshal(chatRequest{
		Model:    c.cfg.Model,
		Messages: messages,
		Tools:    tools,
	})
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, err
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Message{}, fmt.Errorf("invalid LLM response: %w (%s)", err, truncate(string(raw), 200))
	}
	if parsed.Error != nil {
		return Message{}, fmt.Errorf("LLM error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return Message{}, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	if len(parsed.Choices) == 0 {
		return Message{}, fmt.Errorf("LLM returned no choices")
	}
	return parsed.Choices[0].Message, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isLocalLLMBase(base string) bool {
	u := strings.ToLower(base)
	return strings.Contains(u, "localhost") ||
		strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "[::1]") ||
		strings.Contains(u, "0.0.0.0")
}
