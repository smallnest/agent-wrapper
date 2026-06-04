package agy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures the agy agent.
type Options struct {
	BinaryPath string         // path to agy binary; empty = auto-detect
	Extra      map[string]any // provider-specific parameters
}

// Agent drives the Google Antigravity CLI via --print non-interactive mode.
type Agent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an Agent.
func New(opts Options) *Agent {
	return &Agent{opts: opts}
}

func (a *Agent) Name() string { return "Antigravity" }
func (a *Agent) Provider() types.Provider {
	return types.ProviderAgy
}
func (a *Agent) Close() error { return nil }

func (a *Agent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"agy"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "agy"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"agy binary not found in PATH or ~/.local/bin",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// RegisterIn replaces the agy stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("agy", func(opts map[string]any) (agentwrapper.Agent, error) {
		o := Options{}
		if v, ok := opts["binaryPath"].(string); ok {
			o.BinaryPath = v
		}
		return New(o), nil
	}, true)
}

// Run starts an agy subprocess in --print mode and returns an event channel.
func (a *Agent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	if input.Prompt == "" {
		return nil, fmt.Errorf("agy: no prompt provided")
	}

	args := []string{"--print", input.Prompt}
	if input.SessionID != "" {
		args = append(args, "--conversation", input.SessionID)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start agy process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		// Read all stdout as text (agy --print has no JSON mode).
		output, err := io.ReadAll(proc.Stdout())
		if err != nil {
			wrapped := harness.WrapIfContextExceeded(err, proc.Stderr())
			select {
			case events <- types.Event{Type: types.EventError, Error: wrapped}:
			default:
			}
			return
		}

		text := string(output)
		if text != "" {
			select {
			case events <- types.Event{Type: types.EventTextDelta, TextDelta: text}:
			case <-ctx.Done():
				events <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
			}
		}

		select {
		case events <- types.Event{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"}:
		case <-ctx.Done():
			events <- types.Event{Type: types.EventError, Error: ctx.Err()}
			return
		}

		if ec := proc.Wait(); ec != 0 {
			if stderr := proc.Stderr(); stderr != "" {
				wrapped := harness.WrapIfContextExceeded(fmt.Errorf("exit %d: %s", ec, stderr), stderr)
				if _, ok := wrapped.(*harness.ContextLengthExceededError); ok {
					select {
					case events <- types.Event{Type: types.EventError, Error: wrapped}:
					default:
					}
				}
			}
		}
	}()

	return events, nil
}
