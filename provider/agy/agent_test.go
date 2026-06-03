package agy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

// mockAgyBinary creates a shell script that outputs plain text and exits with given code.
func mockAgyBinary(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "agy")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\nexit %d\n", output, exitCode)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestTextDelta(t *testing.T) {
	bin := mockAgyBinary(t, "Hello, world!", 0)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
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
}

func TestEmptyOutput(t *testing.T) {
	bin := mockAgyBinary(t, "", 0)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var deltas int
	var turnEnds int
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			deltas++
		case types.EventTurnEnd:
			turnEnds++
		}
	}

	if deltas != 0 {
		t.Errorf("expected 0 text_delta for empty output, got %d", deltas)
	}
	if turnEnds != 1 {
		t.Errorf("expected 1 turn_end, got %d", turnEnds)
	}
}

func TestNoPrompt(t *testing.T) {
	agent := New(Options{})
	_, err := agent.Run(context.Background(), types.RunInput{})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestBinaryNotFound(t *testing.T) {
	agent := New(Options{BinaryPath: "/nonexistent/agy"})
	_, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestSessionResume(t *testing.T) {
	// Mock script checks --conversation flag is passed.
	mockScript := `#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "--conversation" ]; then
    printf "resumed"
    exit 0
  fi
done
exit 1
`
	dir := t.TempDir()
	script := filepath.Join(dir, "agy")
	if err := os.WriteFile(script, []byte(mockScript), 0o755); err != nil {
		t.Fatal(err)
	}

	agent := New(Options{BinaryPath: script})
	events, err := agent.Run(context.Background(), types.RunInput{
		Prompt:    "hi",
		SessionID: "abc-123",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var text string
	for evt := range events {
		if evt.Type == types.EventTextDelta {
			text = evt.TextDelta
		}
	}

	if text != "resumed" {
		t.Errorf("expected 'resumed', got %q", text)
	}
}

func TestExitError(t *testing.T) {
	bin := mockAgyBinary(t, "partial output", 1)

	agent := New(Options{BinaryPath: bin})
	events, err := agent.Run(context.Background(), types.RunInput{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Drain events — should still get text and turn_end, stderr is empty so no error event.
	var deltas []string
	for evt := range events {
		if evt.Type == types.EventTextDelta {
			deltas = append(deltas, evt.TextDelta)
		}
	}

	if len(deltas) != 1 || deltas[0] != "partial output" {
		t.Errorf("expected text 'partial output', got %v", deltas)
	}
}

func TestContextExceededError(t *testing.T) {
	script := `#!/bin/sh
printf 'some text'
echo 'context_length_exceeded' >&2
exit 1
`
	dir := t.TempDir()
	p := filepath.Join(dir, "agy")
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
			if _, ok := evt.Error.(*agentwrapper.ContextLengthExceededError); ok {
				found = true
			}
		}
	}

	if !found {
		t.Error("expected ContextLengthExceededError")
	}
}

func TestRegisterIn(t *testing.T) {
	r := agentwrapper.NewRegistry()
	if err := RegisterIn(r); err != nil {
		t.Fatalf("RegisterIn: %v", err)
	}

	agent, err := r.Get("agy", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Provider() != types.ProviderAgy {
		t.Errorf("expected provider 'agy', got %q", agent.Provider())
	}
}
