package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures the cursor agent.
type Options struct {
	BinaryPath string         // path to agent binary; empty = auto-detect
	Model      string         // model name passed to --model
	Workspace  string         // workspace path (overrides input.WorkingDir)
	Yolo       bool           // auto-approve all tool calls
	Extra      map[string]any // provider-specific parameters
}

// Agent drives the Cursor Agent CLI via --print --output-format stream-json.
type Agent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an Agent.
func New(opts Options) *Agent {
	return &Agent{opts: opts}
}

func (a *Agent) Name() string { return "Cursor Agent" }
func (a *Agent) Provider() types.Provider {
	return types.ProviderCursor
}
func (a *Agent) Close() error { return nil }

func (a *Agent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"agent"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "agent"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"agent binary not found in PATH or ~/.local/bin. Install from https://cursor.com",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// RegisterIn replaces the cursor stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("cursor", func(opts map[string]any) (agentwrapper.Agent, error) {
		o := Options{}
		if v, ok := opts["binaryPath"].(string); ok {
			o.BinaryPath = v
		}
		if v, ok := opts["model"].(string); ok {
			o.Model = v
		}
		return New(o), nil
	}, true)
}

// Run starts a cursor agent subprocess in --print mode with stream-json output.
func (a *Agent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	if input.Prompt == "" {
		return nil, fmt.Errorf("cursor: no prompt provided")
	}

	args := []string{"--print", "--output-format", "stream-json"}
	if input.SessionID != "" {
		args = append(args, "--resume", input.SessionID)
	}
	if a.opts.Model != "" {
		args = append(args, "--model", a.opts.Model)
	}
	workspace := input.WorkingDir
	if a.opts.Workspace != "" {
		workspace = a.opts.Workspace
	}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	if a.opts.Yolo {
		args = append(args, "--yolo")
	}
	args = append(args, input.Prompt)

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("start cursor process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var sid string

		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseCursorEvent(frame.Data)
			if !ok {
				continue
			}
			if evt.SessionID != "" {
				sid = evt.SessionID
			}
			evt.SessionID = sid
			select {
			case events <- evt:
			case <-ctx.Done():
				events <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
			}
			if evt.Type == types.EventTurnEnd {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			wrapped := harness.WrapIfContextExceeded(err, proc.Stderr())
			select {
			case events <- types.Event{Type: types.EventError, Error: wrapped}:
			default:
			}
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

// cursorEvent is a single NDJSON event from `agent --print --output-format stream-json`.
type cursorEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	SessionID string `json:"session_id,omitempty"`

	Message *cursorMessage `json:"message,omitempty"`

	IsError    bool         `json:"is_error,omitempty"`
	Result     string       `json:"result,omitempty"`
	StopReason string       `json:"stop_reason,omitempty"`
	Usage      *cursorUsage `json:"usage,omitempty"`
}

type cursorMessage struct {
	ID      string           `json:"id"`
	Role    string           `json:"role"`
	Content []cursorContent  `json:"content"`
}

type cursorContent struct {
	Type string `json:"type"`
	Text string `json:"text"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type cursorUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
