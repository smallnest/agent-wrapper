package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

// mockCodexBinary creates a shell script that outputs JSONL events
// matching the `codex exec --json` protocol.
func mockCodexBinary(t *testing.T, jsonlLines []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	content := "#!/bin/sh\n"
	for _, line := range jsonlLines {
		content += fmt.Sprintf("echo '%s'\n", line)
	}
	content += "exec sleep 1\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func TestTextDelta(t *testing.T) {
	bin := mockCodexBinary(t, []string{
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		`{"type":"message_delta","delta":"Hello, "}`,
		`{"type":"message_delta","delta":"world!"}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("hi"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var textDeltas []string
	var turnEnds int
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			textDeltas = append(textDeltas, evt.TextDelta)
		case types.EventTurnEnd:
			turnEnds++
			if evt.StopReason != "end_turn" {
				t.Errorf("expected stopReason 'end_turn', got %q", evt.StopReason)
			}
			if evt.TokenUsage == nil {
				t.Fatal("expected TokenUsage")
			}
			if evt.TokenUsage.TotalTokens != 30 {
				t.Errorf("expected 30 total tokens, got %d", evt.TokenUsage.TotalTokens)
			}
		}
	}

	if len(textDeltas) != 2 {
		t.Fatalf("expected 2 text_delta events, got %d", len(textDeltas))
	}
	if textDeltas[0] != "Hello, " {
		t.Errorf("text delta 0: expected 'Hello, ', got %q", textDeltas[0])
	}
	if textDeltas[1] != "world!" {
		t.Errorf("text delta 1: expected 'world!', got %q", textDeltas[1])
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
}

func TestToolCall(t *testing.T) {
	bin := mockCodexBinary(t, []string{
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		`{"type":"message_delta","delta":"Let me check."}`,
		`{"type":"tool_call","tool_call_id":"call_1","tool_name":"read","tool_input":{"path":"main.go"}}`,
		`{"type":"turn.completed"}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("check"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolCalls []types.Event
	for evt := range events {
		if evt.Type == types.EventToolCall {
			toolCalls = append(toolCalls, evt)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool_call event, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ToolCallID != "call_1" {
		t.Errorf("expected call ID 'call_1', got %q", tc.ToolCallID)
	}
	if tc.ToolName != "read" {
		t.Errorf("expected tool name 'read', got %q", tc.ToolName)
	}
}

func TestToolResult(t *testing.T) {
	bin := mockCodexBinary(t, []string{
		`{"type":"tool_result","tool_call_result_id":"call_1","tool_result":"file contents","is_error":false}`,
		`{"type":"turn.completed"}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("read"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolResults []types.Event
	for evt := range events {
		if evt.Type == types.EventToolResult {
			toolResults = append(toolResults, evt)
		}
	}

	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResults))
	}
	tr := toolResults[0]
	if tr.ToolResultID != "call_1" {
		t.Errorf("expected tool result ID 'call_1', got %q", tr.ToolResultID)
	}
	if tr.ToolResultOutput != "file contents" {
		t.Errorf("expected 'file contents', got %q", tr.ToolResultOutput)
	}
}

func TestTurnFailed(t *testing.T) {
	bin := mockCodexBinary(t, []string{
		`{"type":"turn.failed","error":{"message":"something went wrong"}}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for evt := range events {
		if evt.Type == types.EventError {
			return
		}
	}
	t.Fatal("expected error event for turn.failed")
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/codex"})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestBinaryAutoDetect(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > /dev/null\n"), 0o755)

	t.Setenv("PATH", dir)

	agent := New(Options{})
	bin, err := agent.resolveBinary()
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if bin != script {
		t.Errorf("expected %s, got %s", script, bin)
	}
}

func TestBinaryAutoDetectNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	agent := New(Options{})
	_, err := agent.resolveBinary()
	if err == nil {
		t.Fatal("expected error when binary not found")
	}
}

func TestNoUserMessage(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/codex"})
	session := types.NewSession()

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err == nil {
		t.Fatal("expected error when no user message found")
	}
}

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	content := "#!/bin/sh\n"
	content += `echo '{"type":"message_delta","delta":"hi"}'` + "\n"
	content += "exec sleep 10\n"
	os.WriteFile(script, []byte(content), 0o755)

	agent := New(Options{BinaryPath: script})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	ctx, cancel := context.WithTimeout(context.Background(), 3000*time.Millisecond)
	defer cancel()

	events, err := agent.Run(ctx, types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotEvent bool
	for evt := range events {
		if evt.Type == types.EventTextDelta {
			gotEvent = true
		}
	}
	if !gotEvent {
		t.Error("expected at least one text_delta before cancellation")
	}
}

func TestNameAndProvider(t *testing.T) {
	agent := New(Options{})
	if agent.Name() != "Codex" {
		t.Errorf("expected 'Codex', got %q", agent.Name())
	}
	if agent.Provider() != types.ProviderCodex {
		t.Errorf("expected ProviderCodex, got %q", agent.Provider())
	}
	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestMessagesToOpenAI(t *testing.T) {
	msgs := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("world"),
		types.NewToolUseMessage("c1", "read", json.RawMessage(`{"path":"f.go"}`)),
		types.NewToolResultMessage("c1", "file contents", false),
	}

	result := messagesToOpenAI(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	if result[0]["role"] != "user" {
		t.Errorf("msg 0: expected role 'user', got %v", result[0]["role"])
	}
	if result[1]["role"] != "assistant" {
		t.Errorf("msg 1: expected role 'assistant', got %v", result[1]["role"])
	}
	if result[2]["role"] != "assistant" {
		t.Errorf("msg 2: expected role 'assistant' (tool_use), got %v", result[2]["role"])
	}
	tcs, ok := result[2]["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("msg 2: expected tool_calls, got %v", result[2]["tool_calls"])
	}
	tcMap, _ := tcs[0].(map[string]any)
	if tcMap["id"] != "c1" {
		t.Errorf("msg 2 tool_call: expected id 'c1', got %v", tcMap["id"])
	}
	if result[3]["role"] != "tool" {
		t.Errorf("msg 3: expected role 'tool', got %v", result[3]["role"])
	}
	if result[3]["tool_call_id"] != "c1" {
		t.Errorf("msg 3: expected tool_call_id 'c1', got %v", result[3]["tool_call_id"])
	}
}

func TestOpenAIToMessages(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "world"},
		{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{
				"id":   "c1",
				"type": "function",
				"function": map[string]any{
					"name":      "read",
					"arguments": `{"path":"f.go"}`,
				},
			},
		}},
		{"role": "tool", "tool_call_id": "c1", "content": "file contents"},
	}

	result := openAIToMessages(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	if result[0].Role != types.RoleUser {
		t.Errorf("msg 0: expected RoleUser, got %v", result[0].Role)
	}
	if result[1].Role != types.RoleAssistant {
		t.Errorf("msg 1: expected RoleAssistant, got %v", result[1].Role)
	}
	if result[2].Role != types.RoleToolUse {
		t.Errorf("msg 2: expected RoleToolUse, got %v", result[2].Role)
	}
	if result[2].ToolCallID != "c1" {
		t.Errorf("msg 2: expected ToolCallID 'c1', got %q", result[2].ToolCallID)
	}
	if result[3].Role != types.RoleToolResult {
		t.Errorf("msg 3: expected RoleToolResult, got %v", result[3].Role)
	}
	if result[3].ToolCallResultID != "c1" {
		t.Errorf("msg 3: expected ToolCallResultID 'c1', got %q", result[3].ToolCallResultID)
	}
}

func TestRoundTripConversion(t *testing.T) {
	original := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("world"),
		types.NewToolUseMessage("c1", "read", json.RawMessage(`{"path":"f.go"}`)),
		types.NewToolResultMessage("c1", "contents", false),
	}

	openAI := messagesToOpenAI(original)
	roundTripped := openAIToMessages(openAI)

	if len(roundTripped) != len(original) {
		t.Fatalf("round trip: expected %d messages, got %d", len(original), len(roundTripped))
	}
	for i, msg := range roundTripped {
		if msg.Role != original[i].Role {
			t.Errorf("msg %d: expected role %v, got %v", i, original[i].Role, msg.Role)
		}
		if msg.Content != original[i].Content {
			t.Errorf("msg %d: expected content %q, got %q", i, original[i].Content, msg.Content)
		}
	}
}
