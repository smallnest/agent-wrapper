package pi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

// mockPiBinary creates a shell script that outputs JSONL events.
func mockPiBinary(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
	content := "#!/bin/sh\n"
	for _, line := range lines {
		content += fmt.Sprintf("echo '%s'\n", line)
	}
	// Exit without waiting for stdin — the agent reads stdout and breaks after turn_end
	content += "exit 0\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func TestTextDelta(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"id":"1","type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Hello, "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"world!"}}`,
		`{"type":"turn_end","turnIndex":0}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: ptrMsg(types.NewUserMessage("hi")),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var deltas []string
	for evt := range events {
		if evt.Type == types.EventTextDelta {
			deltas = append(deltas, evt.TextDelta)
		}
	}

	if len(deltas) != 2 {
		t.Fatalf("expected 2 text_delta events, got %d", len(deltas))
	}
	if deltas[0] != "Hello, " {
		t.Errorf("delta 0: expected 'Hello, ', got %q", deltas[0])
	}
	if deltas[1] != "world!" {
		t.Errorf("delta 1: expected 'world!', got %q", deltas[1])
	}
}

func TestToolExecution(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"id":"1","type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Let me check."}}`,
		`{"type":"tool_execution_start","toolCallId":"tc_1","toolName":"read"}`,
		`{"type":"tool_execution_end","toolCallId":"tc_1","toolName":"read","data":{"output":"file contents"},"isError":false}`,
		`{"type":"turn_end","turnIndex":0}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: ptrMsg(types.NewUserMessage("check file")),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolCalls, toolResults int
	for evt := range events {
		if evt.Type == types.EventToolCall {
			toolCalls++
			if evt.ToolCallID != "tc_1" {
				t.Errorf("expected toolCallId 'tc_1', got %q", evt.ToolCallID)
			}
			if evt.ToolName != "read" {
				t.Errorf("expected toolName 'read', got %q", evt.ToolName)
			}
		}
		if evt.Type == types.EventToolResult {
			toolResults++
			if evt.ToolResultID != "tc_1" {
				t.Errorf("expected toolResultId 'tc_1', got %q", evt.ToolResultID)
			}
		}
	}

	if toolCalls != 1 {
		t.Errorf("expected 1 tool_call, got %d", toolCalls)
	}
	if toolResults != 1 {
		t.Errorf("expected 1 tool_result, got %d", toolResults)
	}
}

func TestTurnEnd(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"id":"1","type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Done."}}`,
		`{"type":"turn_end","turnIndex":0}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: ptrMsg(types.NewUserMessage("test")),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for evt := range events {
		if evt.Type == types.EventTurnEnd {
			if evt.TurnNumber != 1 {
				t.Errorf("expected turnNumber 1, got %d", evt.TurnNumber)
			}
			if evt.StopReason != "end_turn" {
				t.Errorf("expected stopReason 'end_turn', got %q", evt.StopReason)
			}
			return
		}
	}
	t.Fatal("never received turn_end event")
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/pi"})
	session := types.NewSession()

	_, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: ptrMsg(types.NewUserMessage("test")),
	})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestBinaryAutoDetect(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
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

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
	content := "#!/bin/sh\n"
	content += `echo '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hi"}}'` + "\n"
	content += "exec sleep 10\n"
	os.WriteFile(script, []byte(content), 0o755)

	agent := New(Options{BinaryPath: script})
	session := types.NewSession()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	events, err := agent.Run(ctx, types.RunInput{
		Session:    session,
		NewMessage: ptrMsg(types.NewUserMessage("test")),
	})
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
	if agent.Name() != "Pi Agent" {
		t.Errorf("expected 'Pi Agent', got %q", agent.Name())
	}
	if agent.Provider() != types.ProviderPiAgent {
		t.Errorf("expected ProviderPiAgent, got %q", agent.Provider())
	}
	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestMessagesToPrompt(t *testing.T) {
	msgs := []types.Message{
		types.NewUserMessage("first"),
		types.NewAssistantMessage("response"),
		types.NewUserMessage("second"),
	}

	result := messagesToPrompt(msgs)
	if result != "second" {
		t.Errorf("expected 'second', got %q", result)
	}

	empty := messagesToPrompt(nil)
	if empty != "" {
		t.Errorf("expected empty string for nil, got %q", empty)
	}
}

func ptrMsg(m types.Message) *types.Message { return &m }
