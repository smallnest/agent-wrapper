package opencode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

// mockOpenCodeBinary creates a temporary shell script that simulates
// `opencode -p <prompt> -f json -q` output.
func mockOpenCodeBinary(t *testing.T, response string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	content := "#!/bin/sh\n"
	// opencode outputs a single JSON line to stdout
	content += "echo '" + response + "'\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

// mockOpenCodeBinaryWithDelay creates a mock binary that sleeps before responding.
func mockOpenCodeBinaryWithDelay(t *testing.T, response string, delay time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	content := "#!/bin/sh\n"
	content += "sleep " + fmtDuration(delay) + "\n"
	content += "echo '" + response + "'\n"
	os.WriteFile(script, []byte(content), 0o755)
	return script
}

func fmtDuration(d time.Duration) string {
	return fmt.Sprintf("%.1f", d.Seconds())
}

func TestRunTextResponse(t *testing.T) {
	bin := mockOpenCodeBinary(t, `{"response":"Hello, I can help with that."}`)

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("help me"))

	events, err := agent.Run(context.Background(), types.RunInput{
		Session: session,
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
		}
	}

	if len(textDeltas) != 1 {
		t.Fatalf("expected 1 text_delta event, got %d", len(textDeltas))
	}
	if textDeltas[0] != "Hello, I can help with that." {
		t.Errorf("expected 'Hello, I can help with that.', got %q", textDeltas[0])
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
}

func TestRunWithNewMessage(t *testing.T) {
	bin := mockOpenCodeBinary(t, `{"response":"Got it."}`)

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()

	events, err := agent.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("new message"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotResponse bool
	for evt := range events {
		if evt.Type == types.EventTextDelta && evt.TextDelta == "Got it." {
			gotResponse = true
		}
	}
	if !gotResponse {
		t.Error("expected text_delta with 'Got it.'")
	}
}

func TestRunEmptyResponse(t *testing.T) {
	bin := mockOpenCodeBinary(t, `{"response":""}`)

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("hello"))

	events, err := agent.Run(context.Background(), types.RunInput{
		Session: session,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turnEnds int
	for evt := range events {
		if evt.Type == types.EventTurnEnd {
			turnEnds++
			if evt.StopReason != "error" {
				t.Errorf("expected stopReason 'error' for empty response, got %q", evt.StopReason)
			}
		}
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
}

func TestRunNoUserMessage(t *testing.T) {
	agent := New(Options{BinaryPath: "/fake/opencode"})
	session := types.NewSession()

	_, err := agent.Run(context.Background(), types.RunInput{
		Session: session,
	})
	if err == nil {
		t.Fatal("expected error when no user message found")
	}
}

func TestBinaryAutoDetect(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "opencode")
	os.WriteFile(script, []byte("#!/bin/sh\necho '{}'\n"), 0o755)

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

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/path/opencode"})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	_, err := agent.Run(context.Background(), types.RunInput{Session: session})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestContextCancellationClosesChannel(t *testing.T) {
	// Verify that cancelling the context eventually closes the event channel.
	// The mock binary sleeps 2 seconds — we cancel after 200ms.
	bin := mockOpenCodeBinaryWithDelay(t, `{"response":"done"}`, 2*time.Second)

	agent := New(Options{BinaryPath: bin})
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("test"))

	ctx, cancel := context.WithCancel(context.Background())

	events, err := agent.Run(ctx, types.RunInput{Session: session})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Cancel after a short delay.
	time.AfterFunc(200*time.Millisecond, cancel)

	// Drain events; channel must close.
	timeout := time.After(15 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // channel closed — expected
			}
		case <-timeout:
			t.Fatal("event channel did not close after context cancellation")
		}
	}
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
