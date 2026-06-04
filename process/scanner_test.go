package process

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

// --- JSONRPCScanner ---

func TestJSONRPCScanner_ThreeValidFrames(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}
{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Hello"}}
{"jsonrpc":"2.0","method":"notify/turn_end","params":{"stopReason":"end_turn"}}`

	s := NewJSONRPCScanner(strings.NewReader(input))
	var frames []Frame
	for s.Scan() {
		frames = append(frames, s.Frame())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if string(frames[0].Data) != `{"jsonrpc":"2.0","id":1,"result":{"status":"ok"}}` {
		t.Errorf("frame 0 data mismatch: %s", frames[0].Data)
	}
	if string(frames[1].Data) != `{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Hello"}}` {
		t.Errorf("frame 1 data mismatch: %s", frames[1].Data)
	}
}

func TestJSONRPCScanner_SkipBlankLines(t *testing.T) {
	input := "\n\n{\"a\":1}\n\n\n{\"b\":2}\n\n"

	s := NewJSONRPCScanner(strings.NewReader(input))
	var frames []Frame
	for s.Scan() {
		frames = append(frames, s.Frame())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
}

func TestJSONRPCScanner_InvalidJSON(t *testing.T) {
	input := "{\"valid\": true}\n{broken json}\n"

	s := NewJSONRPCScanner(strings.NewReader(input))
	// First frame succeeds
	if !s.Scan() {
		t.Fatal("expected first scan to succeed")
	}
	// Second frame should fail
	if s.Scan() {
		t.Fatal("expected scan to fail on invalid JSON")
	}
	err := s.Err()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	pe, ok := err.(*types.ProtocolError)
	if !ok {
		t.Fatalf("expected *ProtocolError, got %T: %v", err, err)
	}
	if len(pe.RawBytes) == 0 {
		t.Error("ProtocolError.RawBytes should not be empty")
	}
}

// --- AgentProcess ---

func TestAgentProcess_EchoHello(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := StartProcess(ctx, ProcessConfig{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(p.Stdout())

	p.Wait()

	if !bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Errorf("expected stdout to contain 'hello', got %q", buf.String())
	}
}

func TestAgentProcess_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	p, err := StartProcess(ctx, ProcessConfig{
		Command: "sleep",
		Args:    []string{"60"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	cancel()

	done := make(chan struct{})
	go func() {
		p.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Process was terminated as expected
	case <-time.After(10 * time.Second):
		t.Fatal("process was not terminated after context cancel")
	}
}

func TestAgentProcess_StderrCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := StartProcess(ctx, ProcessConfig{
		Command: "sh",
		Args:    []string{"-c", "echo stderr_msg >&2 && echo stdout_msg"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	var stdout bytes.Buffer
	_, _ = stdout.ReadFrom(p.Stdout())

	p.Wait()

	if !bytes.Contains(stdout.Bytes(), []byte("stdout_msg")) {
		t.Errorf("expected stdout to contain 'stdout_msg', got %q", stdout.String())
	}

	stderr := p.Stderr()
	if !strings.Contains(stderr, "stderr_msg") {
		t.Errorf("expected stderr to contain 'stderr_msg', got %q", stderr)
	}
}

func TestAgentProcess_WorkDir(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	p, err := StartProcess(ctx, ProcessConfig{
		Command: "pwd",
		WorkDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(p.Stdout())
	p.Wait()

	output := strings.TrimSpace(buf.String())
	// macOS may resolve symlinks in temp paths
	realTmp, _ := filepath.EvalSymlinks(tmpDir)
	realOutput, _ := filepath.EvalSymlinks(output)
	if realOutput != realTmp {
		t.Errorf("expected workdir %q, got %q", realTmp, realOutput)
	}
}

func TestAgentProcess_EnvVars(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := StartProcess(ctx, ProcessConfig{
		Command: "env",
		Env:     map[string]string{"TEST_AGENT_WRAPPER": "hello_env"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(p.Stdout())
	p.Wait()

	if !strings.Contains(buf.String(), "TEST_AGENT_WRAPPER=hello_env") {
		t.Errorf("expected env var in output, got %q", buf.String())
	}
}
