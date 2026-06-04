package acp

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

// Options configures an AcpAgent.
type Options struct {
	BinaryPath string // path to acpx; empty = auto-detect
}

// AcpAgent wraps the acpx CLI via subprocess stdout.
type AcpAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an AcpAgent.
func New(opts Options) *AcpAgent {
	return &AcpAgent{opts: opts}
}

func (a *AcpAgent) Name() string             { return "ACP" }
func (a *AcpAgent) Provider() types.Provider { return "acp" }
func (a *AcpAgent) Close() error             { return nil }

func (a *AcpAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}
	var onceErr error
	a.once.Do(func() {
		candidates := []string{"acpx"}
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "acpx"),
				filepath.Join(home, ".npm-global", "bin", "acpx"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf("acpx binary not found in PATH. Install with: npm install -g acpx")
	})
	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// acpxEvent is a single JSON line from `acpx --format json`.
type acpxEvent struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Delta   string `json:"delta,omitempty"`
	Text    string `json:"text,omitempty"`

	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	ToolResultID     string `json:"tool_result_id,omitempty"`
	ToolResultOutput string `json:"tool_result_output,omitempty"`
	ToolResultError  bool   `json:"tool_result_error,omitempty"`

	StopReason string     `json:"stop_reason,omitempty"`
	Usage      *acpxUsage `json:"usage,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type acpxUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Run starts an acpx subprocess and returns an event channel.
func (a *AcpAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	if input.Prompt == "" {
		return nil, fmt.Errorf("acp: no prompt provided")
	}

	args := []string{"--format", "json"}
	if input.SessionID != "" {
		args = append(args, "--session", input.SessionID)
	}
	args = append(args, input.Prompt)
	if input.WorkingDir != "" {
		args = append(args, "--cwd", input.WorkingDir)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start acp process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var sid string

		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseAcpxEvent(frame.Data)
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

func parseAcpxEvent(data []byte) (types.Event, bool) {
	var raw acpxEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}

	switch raw.Type {
	case "text_delta", "content_delta":
		text := raw.Delta
		if text == "" {
			text = raw.Text
		}
		if text == "" {
			text = raw.Content
		}
		return types.Event{Type: types.EventTextDelta, TextDelta: text}, true

	case "tool_call":
		return types.Event{
			Type:       types.EventToolCall,
			ToolCallID: raw.ToolCallID,
			ToolName:   raw.ToolName,
			ToolInput:  raw.ToolInput,
		}, true

	case "tool_result":
		return types.Event{
			Type:             types.EventToolResult,
			ToolResultID:     raw.ToolResultID,
			ToolResultOutput: raw.ToolResultOutput,
			ToolResultError:  raw.ToolResultError,
		}, true

	case "turn_end", "complete":
		evt := types.Event{
			Type:       types.EventTurnEnd,
			StopReason: raw.StopReason,
		}
		if evt.StopReason == "" {
			evt.StopReason = "end_turn"
		}
		if raw.Usage != nil {
			evt.TokenUsage = &types.TokenUsage{
				InputTokens:  raw.Usage.InputTokens,
				OutputTokens: raw.Usage.OutputTokens,
				TotalTokens:  raw.Usage.TotalTokens,
			}
		}
		return evt, true

	case "error":
		msg := raw.Error
		if msg == "" {
			msg = "unknown acpx error"
		}
		err := fmt.Errorf("acpx: %s", msg)
		if harness.IsContextLengthExceeded(err) {
			err = &harness.ContextLengthExceededError{Err: err}
		}
		return types.Event{Type: types.EventError, Error: err}, true

	case "init", "system":
		return types.Event{SessionID: raw.SessionID}, true
	}

	return types.Event{}, false
}

// RegisterIn registers the acp provider.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("acp", func(opts map[string]any) (agentwrapper.Agent, error) {
		o := Options{}
		if v, ok := opts["binaryPath"].(string); ok {
			o.BinaryPath = v
		}
		return New(o), nil
	}, true)
}
