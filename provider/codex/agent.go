package codex

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

// Options configures a CodexAgent.
type Options struct {
	BinaryPath string         // path to codex binary; empty = auto-detect
	Model      string         // model name (default: "codex-mini-latest")
	Extra      map[string]any // provider-specific parameters
}

// CodexAgent drives the OpenAI Codex CLI via non-interactive exec mode with JSONL output.
type CodexAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates a CodexAgent.
func New(opts Options) *CodexAgent {
	return &CodexAgent{opts: opts}
}

func (a *CodexAgent) Name() string { return "Codex" }
func (a *CodexAgent) Provider() types.Provider {
	return types.ProviderCodex
}
func (a *CodexAgent) Close() error { return nil }

func (a *CodexAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"codex"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "codex"),
				filepath.Join(home, ".npm-global", "bin", "codex"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"codex binary not found in PATH, ~/.local/bin, or ~/.npm-global/bin. Install with: npm install -g @openai/codex",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// codexEvent is a single JSONL event from `codex exec --json`.
type codexEvent struct {
	Type    string      `json:"type"`
	Message string      `json:"message,omitempty"`
	Error   *codexError `json:"error,omitempty"`

	// message_delta / text content
	Delta string `json:"delta,omitempty"`

	// tool call
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// tool result
	ToolCallResultID string `json:"tool_call_result_id,omitempty"`
	ToolResult       string `json:"tool_result,omitempty"`
	IsError          bool   `json:"is_error,omitempty"`

	// turn completed
	Usage *codexUsage `json:"usage,omitempty"`
}

type codexError struct {
	Message string `json:"message,omitempty"`
}

type codexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// RegisterIn replaces the codex stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("codex", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts a codex exec subprocess and returns an event channel.
func (a *CodexAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	// Use Prompt from RunInput.
	if input.Prompt == "" {
		return nil, fmt.Errorf("codex: no prompt provided")
	}

	args := []string{"exec", input.Prompt, "--json"}
	if input.SessionID != "" {
		args = append(args, "--resume", input.SessionID)
	}
	if a.opts.Model != "" {
		args = append(args, "--model", a.opts.Model)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start codex process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var turnNumber int

		for scanner.Scan() {
			frame := scanner.Frame()
			var evt codexEvent
			if err := json.Unmarshal(frame.Data, &evt); err != nil {
				continue
			}

			var out types.Event
			switch evt.Type {
			case "message_delta":
				if evt.Delta != "" {
					out = types.Event{Type: types.EventTextDelta, TextDelta: evt.Delta}
				}
			case "tool_call":
				out = types.Event{
					Type:       types.EventToolCall,
					ToolCallID: evt.ToolCallID,
					ToolName:   evt.ToolName,
					ToolInput:  evt.ToolInput,
				}
			case "tool_result":
				out = types.Event{
					Type:             types.EventToolResult,
					ToolResultID:     evt.ToolCallResultID,
					ToolResultOutput: evt.ToolResult,
					ToolResultError:  evt.IsError,
				}
			case "turn.completed":
				turnNumber++
				out = types.Event{
					Type:       types.EventTurnEnd,
					TurnNumber: turnNumber,
					StopReason: "end_turn",
				}
				if evt.Usage != nil {
					out.TokenUsage = &types.TokenUsage{
						InputTokens:  evt.Usage.InputTokens,
						OutputTokens: evt.Usage.OutputTokens,
						TotalTokens:  evt.Usage.TotalTokens,
					}
				}
			case "turn.failed":
				out = types.Event{
					Type:  types.EventError,
					Error: fmt.Errorf("codex turn failed: %s", evt.Error.Message),
				}
			default:
				continue
			}

			if out.Type == "" {
				continue
			}

			select {
			case events <- out:
			case <-ctx.Done():
				events <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
			}
			if out.Type == types.EventTurnEnd {
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
		// Check if subprocess exited with stderr indicating context-length error.
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
