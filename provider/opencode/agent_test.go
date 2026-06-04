package opencode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/types"
)

// mockOpenCodeBinary creates a shell script that outputs JSONL events
// matching the `opencode run --format json` protocol.
func mockOpenCodeBinary(t *testing.T, jsonlLines []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	content := "#!/bin/sh\n"
	for _, line := range jsonlLines {
		content += fmt.Sprintf("echo '%s'\n", line)
	}
	content += "exec sleep 1\n"
	_ = os.WriteFile(script, []byte(content), 0o755)
	return script
}

func TestTextDelta(t *testing.T) {
	bin := mockOpenCodeBinary(t, []string{
		`{"type":"content_start"}`,
		`{"type":"content_delta","content":"Hello, "}`,
		`{"type":"content_delta","content":"world!"}`,
		`{"type":"content_stop"}`,
		`{"type":"complete","response":{"content":"Hello, world!","usage":{"inputTokens":10,"outputTokens":20},"finishReason":"stop"}}`,
	})

	agent := New(Options{BinaryPath: bin})

	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
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

func TestToolUse(t *testing.T) {
	bin := mockOpenCodeBinary(t, []string{
		`{"type":"content_delta","content":"Let me check."}`,
		`{"type":"tool_use_start","toolCall":{"id":"call_1","name":"read","finished":false}}`,
		`{"type":"tool_use_delta","toolCall":{"id":"call_1","input":"{\"path\":\"main.go\"}"}}`,
		`{"type":"tool_use_stop","toolCall":{"id":"call_1"}}`,
		`{"type":"complete","response":{"toolCalls":[{"id":"call_1","name":"read","input":"{\"path\":\"main.go\"}"}],"finishReason":"tool_use","usage":{"inputTokens":5,"outputTokens":10}}}`,
	})

	agent := New(Options{BinaryPath: bin})

	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
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

func TestErrorEvent(t *testing.T) {
	bin := mockOpenCodeBinary(t, []string{
		`{"type":"error","error":{"name":"AuthError","message":"API key missing"}}`,
	})

	agent := New(Options{BinaryPath: bin})

	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for evt := range events {
		if evt.Type == types.EventError {
			if evt.Error == nil {
				t.Fatal("expected error in event")
			}
			return
		}
	}
	t.Fatal("never received error event")
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/opencode"})

	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestBinaryAutoDetect(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho '{}'\n"), 0o755)

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
	agent := New(Options{BinaryPath: "/fake/opencode"})

	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error when no user message found")
	}
}

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	content := "#!/bin/sh\n"
	content += `echo '{"type":"content_delta","content":"hi"}'` + "\n"
	content += "exec sleep 10\n"
	_ = os.WriteFile(script, []byte(content), 0o755)

	agent := New(Options{BinaryPath: script})

	ctx, cancel := context.WithTimeout(context.Background(), 3000*time.Millisecond)
	defer cancel()

	events, err := agent.Run(ctx, types.RunInput{Prompt: "hi"})
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

func TestContextLengthError(t *testing.T) {
	// Mock a subprocess that writes a context-length error to stderr and fails.
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	content := "#!/bin/sh\n"
	content += `echo '{"type":"content_delta","content":"ok"}'` + "\n"
	content += "echo 'max_tokens exceeded' >&2\n"
	content += "exit 1\n"
	_ = os.WriteFile(script, []byte(content), 0o755)

	agent := New(Options{BinaryPath: script})

	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for evt := range events {
		if evt.Type == types.EventError {
			if evt.Error == nil {
				t.Fatal("expected non-nil error in EventError")
			}
			if !harness.IsContextLengthExceeded(evt.Error) {
				t.Errorf("expected ContextLengthExceededError, got %T: %v", evt.Error, evt.Error)
			}
			return
		}
	}
	t.Fatal("never received error event")
}

func TestNameAndProvider(t *testing.T) {
	agent := New(Options{})
	if agent.Name() != "OpenCode" {
		t.Errorf("expected 'OpenCode', got %q", agent.Name())
	}
	if agent.Provider() != types.ProviderOpenCode {
		t.Errorf("expected ProviderOpenCode, got %q", agent.Provider())
	}
	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRegisterIn(t *testing.T) {
	r := agentwrapper.NewRegistry()
	if err := RegisterIn(r); err != nil {
		t.Fatalf("RegisterIn: %v", err)
	}

	agent, err := r.Get("opencode", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Name() != "OpenCode" {
		t.Errorf("expected 'OpenCode', got %q", agent.Name())
	}
}

func TestMessagesToPrompt(t *testing.T) {
	msgs := []types.Message{
		types.NewUserMessage("first"),
		types.NewAssistantMessage("response"),
		types.NewUserMessage("second"),
	}

	prompt := messagesToPrompt(msgs)
	if prompt != "second" {
		t.Errorf("expected 'second', got %q", prompt)
	}
}

func TestMessagesToPromptEmpty(t *testing.T) {
	prompt := messagesToPrompt(nil)
	if prompt != "" {
		t.Errorf("expected empty string, got %q", prompt)
	}
}

func TestMessagesToPromptNoUser(t *testing.T) {
	msgs := []types.Message{
		types.NewAssistantMessage("hello"),
	}
	prompt := messagesToPrompt(msgs)
	if prompt != "" {
		t.Errorf("expected empty string, got %q", prompt)
	}
}
