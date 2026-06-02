package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// AgentProcess wraps os/exec.Cmd for managing an agent CLI subprocess.
type AgentProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr *bytes.Buffer // collected stderr output

	done      chan struct{} // closed when process exits
	closeOnce sync.Once
	mu        sync.Mutex
	exited    bool
	exitCode  int
}

// ProcessConfig holds configuration for starting an agent subprocess.
type ProcessConfig struct {
	Command string            // path to the CLI binary
	Args    []string          // arguments to pass
	WorkDir string            // working directory (optional)
	Env     map[string]string // extra environment variables (optional)
}

// StartProcess launches a subprocess with the given config.
// Returns an AgentProcess with stdin writer and stdout reader ready.
// When ctx is cancelled, the process receives SIGTERM, then SIGKILL after 5s.
func StartProcess(ctx context.Context, cfg ProcessConfig) (*AgentProcess, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	p := &AgentProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		done:   make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	go func() {
		defer close(p.done)
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				p.exitCode = exitErr.ExitCode()
			}
		}
		p.mu.Unlock()
	}()

	go func() {
		select {
		case <-ctx.Done():
			p.terminate()
		case <-p.done:
		}
	}()

	return p, nil
}

// terminate sends SIGTERM, waits 5s, then SIGKILL.
func (p *AgentProcess) terminate() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		exited := p.exited
		p.mu.Unlock()

		if exited {
			return
		}

		if p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
		}

		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Signal(syscall.SIGKILL)
			}
			<-p.done
		}
	})
}

func (p *AgentProcess) Stdin() io.Writer {
	return p.stdin
}

func (p *AgentProcess) Stdout() io.Reader {
	return p.stdout
}

func (p *AgentProcess) Stderr() string {
	return p.stderr.String()
}

func (p *AgentProcess) Wait() int {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// Close sends SIGTERM, waits 5 seconds, then SIGKILL. Safe to call multiple times.
func (p *AgentProcess) Close() error {
	p.terminate()
	<-p.done
	return nil
}

func (p *AgentProcess) Kill() error {
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}
