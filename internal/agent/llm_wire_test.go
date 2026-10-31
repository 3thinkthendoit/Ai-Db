package agent

import (
	"encoding/json"
	"testing"
)

func TestMessageWireReasoningContent(t *testing.T) {
	raw := `{"role":"assistant","content":"final","reasoning_content":" chain "}`
	var w messageWire
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	m := w.toMessage()
	if m.Content != "final" {
		t.Fatalf("content: got %q", m.Content)
	}
	if m.ReasoningContent != "chain" {
		t.Fatalf("reasoning: got %q", m.ReasoningContent)
	}
}

func TestMessageWireReasoningFallback(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"reasoning", `{"role":"assistant","content":null,"reasoning":" r1 "}`, "r1"},
		{"thinking", `{"role":"assistant","content":"","thinking":" t1 "}`, "t1"},
		{"prefer_reasoning_content", `{"role":"assistant","content":"","reasoning_content":"a","reasoning":"b","thinking":"c"}`, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w messageWire
			if err := json.Unmarshal([]byte(tc.raw), &w); err != nil {
				t.Fatal(err)
			}
			m := w.toMessage()
			if m.ReasoningContent != tc.want {
				t.Fatalf("got %q want %q", m.ReasoningContent, tc.want)
			}
		})
	}
}

func TestDecodeContentString(t *testing.T) {
	if got := decodeContentString(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
	if got := decodeContentString(json.RawMessage("null")); got != "" {
		t.Fatalf("null: %q", got)
	}
	if got := decodeContentString(json.RawMessage(`"hello"`)); got != "hello" {
		t.Fatalf("string: %q", got)
	}
	parts := json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`)
	if got := decodeContentString(parts); got != "ab" {
		t.Fatalf("parts: %q", got)
	}
}

func TestMessageWireToolCallsPreserved(t *testing.T) {
	raw := `{
		"role":"assistant",
		"content":"looking up",
		"tool_calls":[{"id":"1","type":"function","function":{"name":"list_tables","arguments":"{}"}}]
	}`
	var w messageWire
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}
	m := w.toMessage()
	if m.Content != "looking up" {
		t.Fatalf("content: %q", m.Content)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].Function.Name != "list_tables" {
		t.Fatalf("tool_calls: %+v", m.ToolCalls)
	}
}
