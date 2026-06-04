package kimicode

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

func mockKimiBinary(t *testing.T, lines []string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "kimi")
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

func TestKimiTextDelta(t *testing.T) {
	bin := mockKimiBinary(t, []string{
		`{"type":"system","session_id":"k1"}`,
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Hello from Kimi!"}]}}`,
		`{"type":"result","stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":4}}`,
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
			if evt.TokenUsage == nil || evt.TokenUsage.InputTokens != 8 {
				t.Errorf("expected input_tokens=8, got %+v", evt.TokenUsage)
			}
		case types.EventError:
			t.Fatalf("unexpected error: %v", evt.Error)
		}
	}

	if len(deltas) != 1 || deltas[0] != "Hello from Kimi!" {
		t.Errorf("expected text_delta 'Hello from Kimi!', got %v", deltas)
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
	if len(sessions) == 0 || sessions[0] != "k1" {
		t.Errorf("expected session_id 'k1', got %v", sessions)
	}
}

func TestKimiToolUse(t *testing.T) {
	bin := mockKimiBinary(t, []string{
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Checking..."}]}}`,
		`{"type":"assistant","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"bash","input":{"cmd":"ls"}}]}}`,
		`{"type":"assistant","message":{"id":"m3","role":"assistant","content":[{"type":"tool_result","tool_use_id":"t1","text":"README.md","is_error":false}]}}`,
		`{"type":"result","stop_reason":"end_turn"}`,
	}, 0)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "list files"})
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

	if len(deltas) != 1 || deltas[0] != "Checking..." {
		t.Errorf("expected 'Checking...', got %v", deltas)
	}
	if len(toolCalls) != 1 || toolCalls[0] != "bash" {
		t.Errorf("expected tool_call 'bash', got %v", toolCalls)
	}
	if len(toolResults) != 1 || toolResults[0] != "README.md" {
		t.Errorf("expected 'README.md', got %v", toolResults)
	}
}

func TestKimiNoPrompt(t *testing.T) {
	agent := New(Options{})
	_, err := agent.Run(context.Background(), types.RunInput{})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestKimiErrorEvent(t *testing.T) {
	bin := mockKimiBinary(t, []string{
		`{"type":"error","is_error":true,"result":"no model configured"}`,
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

func TestKimiRegisterIn(t *testing.T) {
	r := agentwrapper.NewRegistry()
	if err := RegisterIn(r); err != nil {
		t.Fatalf("RegisterIn: %v", err)
	}

	agent, err := r.Get("kimi-code", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Provider() != types.ProviderKimiCode {
		t.Errorf("expected provider 'kimi-code', got %q", agent.Provider())
	}
}

func TestKimiBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/kimi"})
	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestKimiContextExceeded(t *testing.T) {
	script := `#!/bin/sh
echo '{"type":"system","session_id":"k1"}'
echo 'context_length_exceeded' >&2
exit 1
`
	dir := t.TempDir()
	p := filepath.Join(dir, "kimi")
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

func TestKimiBinaryDiscovery(t *testing.T) {
	// Create ~/.kimi-code/bin/kimi mock to verify discovery paths.
	home := t.TempDir()
	codeDir := filepath.Join(home, ".kimi-code", "bin")
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
echo '{"type":"system","session_id":"discovered"}'
echo '{"type":"result","stop_reason":"end_turn"}'
`
	p := filepath.Join(codeDir, "kimi")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	agent := New(Options{BinaryPath: p})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sid string
	for evt := range events {
		if evt.SessionID != "" {
			sid = evt.SessionID
		}
	}
	if sid != "discovered" {
		t.Errorf("expected session_id 'discovered', got %q", sid)
	}
}
