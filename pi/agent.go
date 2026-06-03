package pi

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

// Options configures a PiAgent.
type Options struct {
	BinaryPath string         // path to pi binary; empty = auto-detect
	Model      string         // model pattern or ID
	Provider   string         // provider name (default: "google")
	Extra      map[string]any // provider-specific parameters
}

// PiAgent drives the Pi CLI via `-p --mode json` non-interactive JSONL protocol.
type PiAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates a PiAgent.
func New(opts Options) *PiAgent {
	return &PiAgent{opts: opts}
}

func (a *PiAgent) Name() string { return "Pi Agent" }
func (a *PiAgent) Provider() types.Provider {
	return types.ProviderPiAgent
}
func (a *PiAgent) Close() error { return nil }

func (a *PiAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"pi"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "pi"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"pi binary not found in PATH or ~/.local/bin. Install with: npm install -g @anthropic-ai/pi or see https://github.com/earendil-works/pi",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// piEvent is a single JSONL event from `pi -p ... --mode json`.
type piEvent struct {
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	ID      string `json:"id,omitempty"`

	// message_update event
	AssistantMessageEvent *piAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// turn_end
	TurnIndex   int            `json:"turnIndex,omitempty"`
	ToolResults []piToolResult `json:"toolResults,omitempty"`

	// message_start / message_end / turn_end contain a message object
	Message *piMessage `json:"message,omitempty"`

	// agent_end
	Messages []piMessage `json:"messages,omitempty"`

	// error
	Error *piErrorDetail `json:"error,omitempty"`
}

type piAssistantMessageEvent struct {
	Type         string `json:"type"`
	ContentIndex int    `json:"contentIndex"`
	Delta        string `json:"delta,omitempty"`
	Content      string `json:"content,omitempty"`
}

type piMessage struct {
	Role    string      `json:"role"`
	Content []piContent `json:"content,omitempty"`
	Usage   *piUsage    `json:"usage,omitempty"`
}

type piContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type piUsage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	TotalTokens int `json:"totalTokens"`
}

type piToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Output     string `json:"output"`
	IsError    bool   `json:"isError"`
}

type piErrorDetail struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// RegisterIn replaces the pi-agent stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("pi-agent", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts a pi subprocess in non-interactive JSON mode and returns an event channel.
func (a *PiAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	// Build the prompt from session messages or new message.
	messages := input.Session.Messages
	if input.NewMessage != nil {
		messages = append(messages, *input.NewMessage)
	}
	prompt := lastUserMessage(messages)
	if prompt == "" {
		return nil, fmt.Errorf("pi: no user message found in input")
	}

	args := []string{"-p", prompt, "--mode", "json", "--no-session"}
	if a.opts.Model != "" {
		args = append(args, "--model", a.opts.Model)
	}
	if a.opts.Provider != "" {
		args = append(args, "--provider", a.opts.Provider)
	}
	if input.SystemPrompt != "" {
		args = append(args, "--system-prompt", input.SystemPrompt)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = proc.Close() }()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var turnNumber int

		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parsePiEvent(frame.Data)
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

func parsePiEvent(data []byte) (types.Event, bool) {
	var raw piEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}

	switch raw.Type {
	case "message_update":
		if raw.AssistantMessageEvent == nil {
			return types.Event{}, false
		}
		switch raw.AssistantMessageEvent.Type {
		case "text_delta":
			return types.Event{
				Type:      types.EventTextDelta,
				TextDelta: raw.AssistantMessageEvent.Delta,
			}, true
		case "toolcall_delta":
			return types.Event{}, false
		}

	case "tool_execution_start":
		return types.Event{
			Type:       types.EventToolCall,
			ToolCallID: raw.ToolCallID,
			ToolName:   raw.ToolName,
		}, true

	case "tool_execution_end":
		resultJSON, _ := json.Marshal(raw.Data)
		return types.Event{
			Type:             types.EventToolResult,
			ToolResultID:     raw.ToolCallID,
			ToolResultOutput: string(resultJSON),
			ToolResultError:  raw.IsError,
		}, true

	case "turn_end":
		evt := types.Event{
			Type:       types.EventTurnEnd,
			StopReason: "end_turn",
		}
		if raw.Message != nil && raw.Message.Usage != nil {
			evt.TokenUsage = &types.TokenUsage{
				InputTokens:  raw.Message.Usage.Input,
				OutputTokens: raw.Message.Usage.Output,
				TotalTokens:  raw.Message.Usage.TotalTokens,
			}
		}
		return evt, true

	case "agent_end":
		// Session complete — we already emitted turn_end above, so skip.
		return types.Event{}, false

	case "error":
		msg := "unknown error"
		if raw.Error != nil {
			msg = raw.Error.Message
		}
		return types.Event{
			Type:  types.EventError,
			Error: fmt.Errorf("pi: %s", msg),
		}, true
	}

	return types.Event{}, false
}

// lastUserMessage returns the last user message content from the message list.
func lastUserMessage(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
