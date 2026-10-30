package agent

import (
	"strings"
	"testing"

	"AI-DB/internal/config"
)

func TestBuildSystemPromptPreferredSchema(t *testing.T) {
	conn := config.Connection{Name: "demo", Type: config.ConnMySQL, Host: "127.0.0.1", Database: "app"}
	llm := config.LLMConfig{MaxRows: 100}

	got := buildSystemPrompt(conn, llm, "orders_db")
	if !strings.Contains(got, "User-selected database/schema: orders_db") {
		t.Fatalf("expected preferred schema line, got:\n%s", got)
	}
	if !strings.Contains(got, "Prefer this schema") {
		t.Fatalf("expected prefer guidance, got:\n%s", got)
	}

	empty := buildSystemPrompt(conn, llm, "  ")
	if strings.Contains(empty, "User-selected database/schema:") {
		t.Fatalf("blank preferred schema should be omitted, got:\n%s", empty)
	}
}
