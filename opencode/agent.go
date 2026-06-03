package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures an OpenCodeAgent.
type Options struct {
	BinaryPath string         // path to opencode binary; empty = auto-detect
	Model      string         // model in provider/model format passed via -m
	Extra      map[string]any // provider-specific parameters
}

// OpenCodeAgent drives the OpenCode CLI via `opencode run <message> --format json`.
//
// OpenCode's --format json outputs streaming ProviderEvent JSON objects.
type OpenCodeAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an OpenCodeAgent.
func New(opts Options) *OpenCodeAgent {
	return &OpenCodeAgent{opts: opts}
}

func (a *OpenCodeAgent) Name() string { return "OpenCode" }
func (a *OpenCodeAgent) Provider() types.Provider {
	return types.ProviderOpenCode
}
func (a *OpenCodeAgent) Close() error { return nil }

func (a *OpenCodeAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"opencode"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "opencode"),
				filepath.Join(home, "go", "bin", "opencode"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"opencode binary not found in PATH, ~/.local/bin, or ~/go/bin. Install with: go install github.com/opencode-ai/opencode@latest",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// opencodeEvent is a streaming event from `opencode run --format json`.
type opencodeEvent struct {
	Type     string `json:"type"`
	Content  string `json:"content,omitempty"`
	Thinking string `json:"thinking,omitempty"`

	// tool_use_start / tool_use_delta / tool_use_stop
	ToolCall *opencodeToolCall `json:"toolCall,omitempty"`

	// complete event
	Response *opencodeResponse `json:"response,omitempty"`

	// error event
	Error *opencodeErrorDetail `json:"error,omitempty"`

	// session metadata
	Timestamp int64  `json:"timestamp,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
}

type opencodeToolCall struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    string `json:"input,omitempty"`
	Finished bool   `json:"finished,omitempty"`
}

type opencodeResponse struct {
	Content      string             `json:"content,omitempty"`
	ToolCalls    []opencodeToolCall `json:"toolCalls,omitempty"`
	Usage        *opencodeUsage     `json:"usage,omitempty"`
	FinishReason string             `json:"finishReason,omitempty"`
}

type opencodeUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type opencodeErrorDetail struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// RegisterIn replaces the opencode stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("opencode", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts an opencode subprocess in non-interactive mode and returns an event channel.
func (a *OpenCodeAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	// Use Prompt from RunInput.
	if input.Prompt == "" {
		return nil, fmt.Errorf("opencode: no prompt provided")
	}

	args := []string{"run", input.Prompt, "--format", "json"}
	if input.SessionID != "" {
		args = append(args, "--session", input.SessionID)
	}
	if a.opts.Model != "" {
		args = append(args, "-m", a.opts.Model)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start opencode process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var turnNumber int

		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseOpenCodeEvent(frame.Data)
			if !ok {
				continue
			}
			if evt.Type == types.EventTurnEnd {
				turnNumber++
				evt.TurnNumber = turnNumber
			}
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
			wrapped := agentwrapper.WrapIfContextExceeded(err, proc.Stderr())
			select {
			case events <- types.Event{Type: types.EventError, Error: wrapped}:
			default:
			}
		}
		// Check if subprocess exited with stderr indicating context-length error.
		if ec := proc.Wait(); ec != 0 {
			if stderr := proc.Stderr(); stderr != "" {
				wrapped := agentwrapper.WrapIfContextExceeded(fmt.Errorf("exit %d: %s", ec, stderr), stderr)
				if _, ok := wrapped.(*agentwrapper.ContextLengthExceededError); ok {
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

func parseOpenCodeEvent(data []byte) (types.Event, bool) {
	var raw opencodeEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}

	switch raw.Type {
	case "content_delta":
		if raw.Content != "" {
			return types.Event{
				Type:      types.EventTextDelta,
				TextDelta: raw.Content,
			}, true
		}

	case "tool_use_start":
		if raw.ToolCall != nil {
			var input json.RawMessage
			if raw.ToolCall.Input != "" {
				input = json.RawMessage(raw.ToolCall.Input)
			}
			return types.Event{
				Type:       types.EventToolCall,
				ToolCallID: raw.ToolCall.ID,
				ToolName:   raw.ToolCall.Name,
				ToolInput:  input,
			}, true
		}

	case "tool_use_stop":
		// Tool call completed — we already emitted at tool_use_start
		return types.Event{}, false

	case "complete":
		evt := types.Event{
			Type:       types.EventTurnEnd,
			StopReason: "end_turn",
		}
		if raw.Response != nil {
			switch raw.Response.FinishReason {
			case "tool_use":
				evt.StopReason = "tool_use"
			case "length":
				evt.StopReason = "length"
			case "error":
				evt.StopReason = "error"
			}
			if raw.Response.Usage != nil {
				evt.TokenUsage = &types.TokenUsage{
					InputTokens:  raw.Response.Usage.InputTokens,
					OutputTokens: raw.Response.Usage.OutputTokens,
					TotalTokens:  raw.Response.Usage.InputTokens + raw.Response.Usage.OutputTokens,
				}
			}
		}
		return evt, true

	case "error":
		msg := "unknown error"
		if raw.Error != nil {
			msg = raw.Error.Message
		}
		return types.Event{
			Type:  types.EventError,
			Error: fmt.Errorf("opencode: %s", msg),
		}, true
	}

	return types.Event{}, false
}

