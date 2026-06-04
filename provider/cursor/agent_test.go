package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/types"
)

func mockCursorBinary(t *testing.T, lines []string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "agent")
	content := "#!/bin/sh\n"
	for _, line := range lines {
		content += fmt.Sprintf("echo '%s'\n", line)
	}
	content += fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestCursorTextDelta(t *testing.T) {
	bin := mockCursorBinary(t, []string{
		`{"type":"system","session_id":"s1"}`,
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}}`,
		`{"type":"result","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`,
	}, 0)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var deltas []string
	var turnEnds int
	var sessions []string
	for evt := range events {
		if evt.SessionID != "" {
			sessions = append(sessions, evt.SessionID)
		}
		switch evt.Type {
		case types.EventTextDelta:
			deltas = append(deltas, evt.TextDelta)
		case types.EventTurnEnd:
			turnEnds++
			if evt.TokenUsage == nil || evt.TokenUsage.InputTokens != 5 {
				t.Errorf("expected input_tokens=5, got %+v", evt.TokenUsage)
			}
		case types.EventError:
			t.Fatalf("unexpected error: %v", evt.Error)
		}
	}

	if len(deltas) != 1 || deltas[0] != "Hello, world!" {
		t.Errorf("expected text_delta 'Hello, world!', got %v", deltas)
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
	if len(sessions) == 0 || sessions[0] != "s1" {
		t.Errorf("expected session_id 's1', got %v", sessions)
	}
}

func TestCursorToolUse(t *testing.T) {
	bin := mockCursorBinary(t, []string{
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Let me check."}]}}`,
		`{"type":"assistant","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"tc1","name":"read","input":{"path":"/tmp"}}]}}`,
		`{"type":"assistant","message":{"id":"m3","role":"assistant","content":[{"type":"tool_result","tool_use_id":"tc1","text":"file contents","is_error":false}]}}`,
		`{"type":"result","stop_reason":"end_turn"}`,
	}, 0)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "read file"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var deltas []string
	var toolCalls []string
	var toolResults []string
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			deltas = append(deltas, evt.TextDelta)
		case types.EventToolCall:
			toolCalls = append(toolCalls, evt.ToolName)
		case types.EventToolResult:
			toolResults = append(toolResults, evt.ToolResultOutput)
		}
	}

	if len(deltas) != 1 || deltas[0] != "Let me check." {
		t.Errorf("expected text 'Let me check.', got %v", deltas)
	}
	if len(toolCalls) != 1 || toolCalls[0] != "read" {
		t.Errorf("expected tool_call 'read', got %v", toolCalls)
	}
	if len(toolResults) != 1 || toolResults[0] != "file contents" {
		t.Errorf("expected tool_result 'file contents', got %v", toolResults)
	}
}

func TestCursorNoPrompt(t *testing.T) {
	agent := New(Options{})
	_, err := agent.Run(context.Background(), types.RunInput{})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestCursorErrorEvent(t *testing.T) {
	bin := mockCursorBinary(t, []string{
		`{"type":"error","is_error":true,"result":"authentication failed"}`,
	}, 1)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var errors []string
	for evt := range events {
		if evt.Type == types.EventError {
			errors = append(errors, evt.Error.Error())
		}
	}

	if len(errors) == 0 {
		t.Fatal("expected at least one error event")
	}
}

func TestCursorRegisterIn(t *testing.T) {
	r := agentwrapper.NewRegistry()
	if err := RegisterIn(r); err != nil {
		t.Fatalf("RegisterIn: %v", err)
	}

	agent, err := r.Get("cursor", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Provider() != types.ProviderCursor {
		t.Errorf("expected provider 'cursor', got %q", agent.Provider())
	}
}

func TestCursorBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/agent"})
	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestCursorContextExceeded(t *testing.T) {
	script := `#!/bin/sh
echo '{"type":"system","session_id":"s1"}'
echo 'context_length_exceeded' >&2
exit 1
`
	dir := t.TempDir()
	p := filepath.Join(dir, "agent")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	agent := New(Options{BinaryPath: p})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for evt := range events {
		if evt.Type == types.EventError {
			if _, ok := evt.Error.(*harness.ContextLengthExceededError); ok {
				found = true
			}
		}
	}

	if !found {
		t.Error("expected ContextLengthExceededError")
	}
}
