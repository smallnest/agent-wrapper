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

// mockPiBinary creates a shell script that outputs JSONL events
// matching the `pi -p ... --mode json` protocol.
func mockPiBinary(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
	content := "#!/bin/sh\n"
	for _, line := range lines {
		content += fmt.Sprintf("echo '%s'\n", line)
	}
	content += "exec sleep 1\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func TestTextDelta(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Hello, "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"world!"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}],"usage":{"input":10,"output":20,"totalTokens":30}}}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}],"usage":{"input":10,"output":20,"totalTokens":30}},"toolResults":[]}`,
		`{"type":"agent_end","messages":[]}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("hi"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var deltas []string
	var turnEnds int
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			deltas = append(deltas, evt.TextDelta)
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

	if len(deltas) != 2 {
		t.Fatalf("expected 2 text_delta events, got %d", len(deltas))
	}
	if deltas[0] != "Hello, " {
		t.Errorf("delta 0: expected 'Hello, ', got %q", deltas[0])
	}
	if deltas[1] != "world!" {
		t.Errorf("delta 1: expected 'world!', got %q", deltas[1])
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
}

func TestToolExecution(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Let me check."}}`,
		`{"type":"tool_execution_start","toolCallId":"tc_1","toolName":"read"}`,
		`{"type":"tool_execution_end","toolCallId":"tc_1","toolName":"read","data":{"output":"file contents"},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]},"toolResults":[]}`,
		`{"type":"agent_end","messages":[]}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("check file"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
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
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Done."}}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"Done."}],"usage":{"input":100,"output":200,"totalTokens":300}},"toolResults":[]}`,
		`{"type":"agent_end","messages":[]}`,
	})

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	events, err := agent.Run(context.Background(), types.RunInput{Session: session})
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
			if evt.TokenUsage == nil {
				t.Fatal("expected TokenUsage")
			}
			if evt.TokenUsage.TotalTokens != 300 {
				t.Errorf("expected 300 total tokens, got %d", evt.TokenUsage.TotalTokens)
			}
			return
		}
	}
	t.Fatal("never received turn_end event")
}

func TestErrorEvent(t *testing.T) {
	bin := mockPiBinary(t, []string{
		`{"type":"error","error":{"name":"AuthError","message":"API key missing"}}`,
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
			if evt.Error == nil {
				t.Fatal("expected error in event")
			}
			return
		}
	}
	t.Fatal("never received error event")
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/pi"})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
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

func TestNoUserMessage(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/pi"})
	session := types.NewSession()

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err == nil {
		t.Fatal("expected error when no user message found")
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
