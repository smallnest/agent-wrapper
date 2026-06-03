package acp

import (
	"context"
	"encoding/json"
	"testing"

	agentwrapper "github.com/smallnest/agent-wrapper"
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

	// Verify acpx appears in registry list.
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

	// BinaryPath passed through options.
	agent2, err := r.Get("acp", map[string]any{"binaryPath": "/custom/acpx"})
	if err != nil {
		t.Fatalf("Get with options: %v", err)
	}
	if agent2.Name() != "ACP" {
		t.Errorf("expected 'ACP', got %q", agent2.Name())
	}
}

// mockACPServer produces a minimal ACP JSON-RPC response enough to test the
// collector logic without a real process.
type mockACPServer struct{}

func TestSessionUpdateRouting(t *testing.T) {
	// Verify collector routes to correct Event types.
	col := &collector{}

	// Agent message.
	col.events = nil
	col.SessionUpdate(context.Background(), newTextUpdate("session_1", "hello"))
	if len(col.events) != 1 || col.events[0].Type != types.EventTextDelta {
		t.Fatalf("expected 1 text_delta, got %v", col.events)
	}

	// Tool call.
	col.events = nil
	col.SessionUpdate(context.Background(), newToolCallUpdate("session_1", "call_1", "edit", map[string]any{"path": "f.go"}))
	if len(col.events) != 1 || col.events[0].Type != types.EventToolCall {
		t.Fatalf("expected 1 tool_call, got %v", col.events)
	}

	// Tool call update (result).
	col.events = nil
	col.SessionUpdate(context.Background(), newToolUpdate("session_1", "call_1", map[string]any{"output": "ok"}))
	if len(col.events) != 1 || col.events[0].Type != types.EventToolResult {
		t.Fatalf("expected 1 tool_result, got %v", col.events)
	}
}

func newTextUpdate(sid, text string) acp_sdk.SessionNotification {
	return acp_sdk.SessionNotification{
		SessionId: acp_sdk.SessionId(sid),
		Update:    acp_sdk.UpdateAgentMessageText(text),
	}
}

func newToolCallUpdate(sid, callID, kind string, input any) acp_sdk.SessionNotification {
	return acp_sdk.SessionNotification{
		SessionId: acp_sdk.SessionId(sid),
		Update: acp_sdk.StartToolCall(acp_sdk.ToolCallId(callID), "a tool",
			acp_sdk.WithStartKind(acp_sdk.ToolKind(kind)),
			acp_sdk.WithStartRawInput(input),
		),
	}
}

func newToolUpdate(sid, callID string, output any) acp_sdk.SessionNotification {
	return acp_sdk.SessionNotification{
		SessionId: acp_sdk.SessionId(sid),
		Update: acp_sdk.UpdateToolCall(acp_sdk.ToolCallId(callID),
			acp_sdk.WithUpdateRawOutput(output),
			acp_sdk.WithUpdateStatus(acp_sdk.ToolCallStatusCompleted),
		),
	}
}

func TestCollectorPermissions(t *testing.T) {
	col := &collector{}
	resp, err := col.RequestPermission(context.Background(), acp_sdk.RequestPermissionRequest{
		Options: []acp_sdk.PermissionOption{
			{OptionId: acp_sdk.PermissionOptionId("opt1"), Name: "Allow", Kind: "allow"},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if resp.Outcome.Selected == nil {
		t.Fatal("expected auto-selected first option")
	}

	// Empty options = cancelled.
	resp2, err := col.RequestPermission(context.Background(), acp_sdk.RequestPermissionRequest{})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if resp2.Outcome.Cancelled == nil {
		t.Fatal("expected cancelled when no options")
	}
}

// Ensure acp_sdk types are used (compile-time check).
var _ = acp_sdk.TextBlock
var _ = json.Marshal
