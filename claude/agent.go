package claude

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

// Options configures a ClaudeCodeAgent.
type Options struct {
	BinaryPath string         // path to claude binary; empty = auto-detect
	Model      string         // model name passed to --model
	Extra      map[string]any // provider-specific parameters
}

// ClaudeCodeAgent drives the Claude Code CLI via non-interactive mode
// with stream-json output (NDJSON).
type ClaudeCodeAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates a ClaudeCodeAgent.
func New(opts Options) *ClaudeCodeAgent {
	return &ClaudeCodeAgent{opts: opts}
}

func (a *ClaudeCodeAgent) Name() string { return "Claude Code" }
func (a *ClaudeCodeAgent) Provider() types.Provider {
	return types.ProviderClaudeCode
}
func (a *ClaudeCodeAgent) Close() error { return nil }

func (a *ClaudeCodeAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"claude"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "claude"),
				filepath.Join(home, ".npm-global", "bin", "claude"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"claude binary not found in PATH, ~/.local/bin, or ~/.npm-global/bin. Install with: npm install -g @anthropic-ai/claude-code",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// claudeEvent is a single NDJSON event from `claude -p --output-format stream-json --verbose`.
type claudeEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// system/init event
	SessionID string `json:"session_id,omitempty"`

	// assistant event
	Message *claudeMessage `json:"message,omitempty"`

	// result event
	IsError    bool         `json:"is_error,omitempty"`
	Result     string       `json:"result,omitempty"`
	StopReason string       `json:"stop_reason,omitempty"`
	Usage      *claudeUsage `json:"usage,omitempty"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
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

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Run starts a claude subprocess in non-interactive mode and returns an event channel.
func (a *ClaudeCodeAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	// Use Prompt from RunInput.
	if input.Prompt == "" {
		return nil, fmt.Errorf("claude: no prompt provided")
	}

	args := []string{"-p", input.Prompt, "--output-format", "stream-json", "--verbose"}
	if a.opts.Model != "" {
		args = append(args, "--model", a.opts.Model)
	}
	if input.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", input.MaxTurns))
	}
	if len(input.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, input.AllowedTools...)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start claude process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())

		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseClaudeEvent(frame.Data)
			if !ok {
				continue
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

func parseClaudeEvent(data []byte) (types.Event, bool) {
	var raw claudeEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}

	switch raw.Type {
	case "assistant":
		if raw.Message == nil {
			return types.Event{}, false
		}
		for _, c := range raw.Message.Content {
			switch c.Type {
			case "text":
				return types.Event{Type: types.EventTextDelta, TextDelta: c.Text}, true
			case "tool_use":
				return types.Event{
					Type:       types.EventToolCall,
					ToolCallID: c.ID,
					ToolName:   c.Name,
					ToolInput:  c.Input,
				}, true
			case "tool_result":
				return types.Event{
					Type:             types.EventToolResult,
					ToolResultID:     c.ToolUseID,
					ToolResultOutput: c.Text,
					ToolResultError:  c.IsError,
				}, true
			}
		}

	case "result":
		evt := types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: 1,
			StopReason: raw.StopReason,
		}
		if raw.StopReason == "" {
			evt.StopReason = "end_turn"
		}
		if raw.Usage != nil {
			evt.TokenUsage = &types.TokenUsage{
				InputTokens:  raw.Usage.InputTokens,
				OutputTokens: raw.Usage.OutputTokens,
				TotalTokens:  raw.Usage.InputTokens + raw.Usage.OutputTokens,
			}
		}
		return evt, true
	}

	return types.Event{}, false
}

