package claude

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

// mockClaudeBinary creates a temporary shell script that reads stdin lines
// and writes predefined JSON-RPC notifications to stdout.
func mockClaudeBinary(t *testing.T, notifications []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	content := "#!/bin/sh\n"
	for _, n := range notifications {
		content += fmt.Sprintf("echo '%s'\n", n)
	}
	// Wait for stdin to close then exit
	content += "cat > /dev/null\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func TestNotifyTextDelta(t *testing.T) {
	bin := mockClaudeBinary(t, []string{
		`{"jsonrpc":"2.0","id":1,"result":{"status":"initialized"}}`,
		`{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Hello, "}}`,
		`{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"world!"}}`,
		`{"jsonrpc":"2.0","method":"notify/turn_end","params":{"stopReason":"end_turn","turnNumber":1,"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: nil,
	})
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
			if evt.TurnNumber != 1 {
				t.Errorf("expected turnNumber 1, got %d", evt.TurnNumber)
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

func TestNotifyToolUse(t *testing.T) {
	bin := mockClaudeBinary(t, []string{
		`{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Let me check."}}`,
		`{"jsonrpc":"2.0","method":"notify/tool_use","params":{"id":"call_1","name":"read","input":{"path":"main.go"}}}`,
		`{"jsonrpc":"2.0","method":"notify/turn_end","params":{"stopReason":"tool_use","turnNumber":1,"usage":{"input_tokens":5,"output_tokens":10,"total_tokens":15}}}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

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

func TestNotifyTurnEnd(t *testing.T) {
	bin := mockClaudeBinary(t, []string{
		`{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Done."}}`,
		`{"jsonrpc":"2.0","method":"notify/turn_end","params":{"stopReason":"end_turn","turnNumber":2,"usage":{"input_tokens":100,"output_tokens":200,"total_tokens":300}}}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for evt := range events {
		if evt.Type == types.EventTurnEnd {
			if evt.TurnNumber != 2 {
				t.Errorf("expected turnNumber 2, got %d", evt.TurnNumber)
			}
			if evt.TokenUsage.InputTokens != 100 {
				t.Errorf("expected 100 input tokens, got %d", evt.TokenUsage.InputTokens)
			}
			return
		}
	}
	t.Fatal("never received turn_end event")
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/claude"})
	session := types.NewSession()

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	// The error from resolveBinary won't be returned because BinaryPath is set;
	// StartProcess will fail instead.
}

func TestBinaryAutoDetect(t *testing.T) {
	// Create a mock binary in a temp dir and add it to PATH.
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > /dev/null\n"), 0o755)

	t.Setenv("PATH", dir)

	agent := New(Options{}) // empty BinaryPath triggers auto-detect
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

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	content := "#!/bin/sh\n"
	content += `echo '{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"hi"}}'` + "\n"
	content += "exec sleep 10\n"
	os.WriteFile(script, []byte(content), 0o755)

	agent := New(Options{BinaryPath: script})
	session := types.NewSession()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
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
	if agent.Name() != "Claude Code" {
		t.Errorf("expected 'Claude Code', got %q", agent.Name())
	}
	if agent.Provider() != types.ProviderClaudeCode {
		t.Errorf("expected ProviderClaudeCode, got %q", agent.Provider())
	}
	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestMessagesToContentBlocks(t *testing.T) {
	msgs := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("world"),
		types.NewToolUseMessage("c1", "read", json.RawMessage(`{"path":"f.go"}`)),
		types.NewToolResultMessage("c1", "file contents", false),
	}

	blocks := messagesToContentBlocks(msgs)
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}

	if blocks[0]["type"] != "user" {
		t.Errorf("block 0: expected type 'user', got %v", blocks[0]["type"])
	}
	if blocks[1]["type"] != "assistant" {
		t.Errorf("block 1: expected type 'assistant', got %v", blocks[1]["type"])
	}
	if blocks[2]["type"] != "tool_use" {
		t.Errorf("block 2: expected type 'tool_use', got %v", blocks[2]["type"])
	}
	if blocks[2]["id"] != "c1" {
		t.Errorf("block 2: expected id 'c1', got %v", blocks[2]["id"])
	}
	if blocks[3]["type"] != "tool_result" {
		t.Errorf("block 3: expected type 'tool_result', got %v", blocks[3]["type"])
	}
	if blocks[3]["tool_use_id"] != "c1" {
		t.Errorf("block 3: expected tool_use_id 'c1', got %v", blocks[3]["tool_use_id"])
	}
}

func TestContentBlocksToMessages(t *testing.T) {
	blocks := []map[string]any{
		{"type": "user", "text": "hello"},
		{"type": "assistant", "text": "world"},
		{"type": "tool_use", "id": "c1", "name": "read", "input": map[string]any{"path": "f.go"}},
		{"type": "tool_result", "tool_use_id": "c1", "content": "file contents", "is_error": false},
	}

	msgs := contentBlocksToMessages(blocks)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	if msgs[0].Role != types.RoleUser {
		t.Errorf("msg 0: expected RoleUser, got %v", msgs[0].Role)
	}
	if msgs[0].Content != "hello" {
		t.Errorf("msg 0: expected 'hello', got %q", msgs[0].Content)
	}
	if msgs[1].Role != types.RoleAssistant {
		t.Errorf("msg 1: expected RoleAssistant, got %v", msgs[1].Role)
	}
	if msgs[2].Role != types.RoleToolUse {
		t.Errorf("msg 2: expected RoleToolUse, got %v", msgs[2].Role)
	}
	if msgs[2].ToolCallID != "c1" {
		t.Errorf("msg 2: expected ToolCallID 'c1', got %q", msgs[2].ToolCallID)
	}
	if msgs[2].ToolName != "read" {
		t.Errorf("msg 2: expected ToolName 'read', got %q", msgs[2].ToolName)
	}
	if msgs[3].Role != types.RoleToolResult {
		t.Errorf("msg 3: expected RoleToolResult, got %v", msgs[3].Role)
	}
	if msgs[3].ToolCallResultID != "c1" {
		t.Errorf("msg 3: expected ToolCallResultID 'c1', got %q", msgs[3].ToolCallResultID)
	}
}

func TestRoundTripConversion(t *testing.T) {
	original := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("world"),
		types.NewToolUseMessage("c1", "read", json.RawMessage(`{"path":"f.go"}`)),
		types.NewToolResultMessage("c1", "contents", false),
	}

	blocks := messagesToContentBlocks(original)
	roundTripped := contentBlocksToMessages(blocks)

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
