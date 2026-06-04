package acp

import (
	"context"
	"encoding/json"
	"testing"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/types"
)

func TestNameAndProvider(t *testing.T) {
	agent := New(Options{})
	if agent.Name() != "ACP" {
		t.Errorf("expected 'ACP', got %q", agent.Name())
	}
	if agent.Provider() != "acp" {
		t.Errorf("expected 'acp', got %q", agent.Provider())
	}
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/acpx"})
	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "test"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestEmptyPrompt(t *testing.T) {
	agent := New(Options{})
	_, err := agent.Run(context.Background(), types.RunInput{Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestClose(t *testing.T) {
	agent := New(Options{})
	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRegisterIn(t *testing.T) {
	r := agentwrapper.NewRegistry()
	if err := RegisterIn(r); err != nil {
		t.Fatalf("RegisterIn: %v", err)
	}

	agent, err := r.Get("acp", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Name() != "ACP" {
		t.Errorf("expected 'ACP', got %q", agent.Name())
	}

	found := false
	for _, name := range r.List() {
		if name == "acp" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'acp' in registry list")
	}

	agent2, err := r.Get("acp", map[string]any{"binaryPath": "/custom/acpx"})
	if err != nil {
		t.Fatalf("Get with options: %v", err)
	}
	if agent2.Name() != "ACP" {
		t.Errorf("expected 'ACP', got %q", agent2.Name())
	}
}

func TestParseAcpxTextDelta(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"text_delta","delta":"hello"}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.Type != types.EventTextDelta || evt.TextDelta != "hello" {
		t.Errorf("got type=%s text=%q", evt.Type, evt.TextDelta)
	}
}

func TestParseAcpxToolCall(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"tool_call","tool_call_id":"c1","tool_name":"read","tool_input":{}}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.Type != types.EventToolCall || evt.ToolCallID != "c1" {
		t.Errorf("got type=%s id=%q", evt.Type, evt.ToolCallID)
	}
}

func TestParseAcpxToolResult(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"tool_result","tool_result_id":"c1","tool_result_output":"ok"}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.Type != types.EventToolResult || evt.ToolResultID != "c1" {
		t.Errorf("got type=%s id=%q", evt.Type, evt.ToolResultID)
	}
}

func TestParseAcpxTurnEnd(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"turn_end","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.Type != types.EventTurnEnd {
		t.Errorf("got type=%s", evt.Type)
	}
	if evt.TokenUsage == nil || evt.TokenUsage.TotalTokens != 15 {
		t.Errorf("expected usage 15, got %v", evt.TokenUsage)
	}
}

func TestParseAcpxSessionID(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"init","session_id":"sess-123"}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.SessionID != "sess-123" {
		t.Errorf("expected sess-123, got %q", evt.SessionID)
	}
}

func TestParseAcpxError(t *testing.T) {
	evt, ok := parseAcpxEvent([]byte(`{"type":"error","error":"context length exceeded"}`))
	if !ok {
		t.Fatal("expected ok")
	}
	if evt.Type != types.EventError {
		t.Errorf("got type=%s", evt.Type)
	}
	if !harness.IsContextLengthExceeded(evt.Error) {
		t.Error("expected context-length typed error")
	}
}

func TestParseAcpxUnknownType(t *testing.T) {
	_, ok := parseAcpxEvent([]byte(`{"type":"garbage"}`))
	if ok {
		t.Error("expected false for unknown type")
	}
}

// Ensure json is imported.
var _ = json.Marshal
