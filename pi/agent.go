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

// PiAgent drives the Pi CLI via --mode rpc JSONL protocol.
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

// rpcCommand is a JSONL command sent to pi --mode rpc.
type rpcCommand struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// rawEvent is the envelope for any JSONL event from pi.
type rawEvent struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Success bool            `json:"success,omitempty"`
	Command string          `json:"command,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`

	// Fields from agent session events
	AssistantMessageEvent *struct {
		Type         string `json:"type"`
		ContentIndex int    `json:"contentIndex"`
		Delta        string `json:"delta"`
	} `json:"assistantMessageEvent,omitempty"`

	TurnIndex   int `json:"turnIndex,omitempty"`
	ToolCallID  string `json:"toolCallId,omitempty"`
	ToolName    string `json:"toolName,omitempty"`
	IsError     bool   `json:"isError,omitempty"`
}

// RegisterIn replaces the pi-agent stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("pi-agent", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts a pi subprocess in RPC mode and returns an event channel.
func (a *PiAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	args := []string{"--mode", "rpc", "--no-session"}
	if a.opts.Model != "" {
		args = append(args, "--model", a.opts.Model)
	}
	if a.opts.Provider != "" {
		args = append(args, "--provider", a.opts.Provider)
	}
	if input.SystemPrompt != "" {
		args = append(args, "--system-prompt", input.SystemPrompt)
	}
	if input.WorkingDir != "" {
		args = append(args, "--cwd", input.WorkingDir)
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start pi process: %w", err)
	}

	// Build the user message from NewMessage or session history.
	userMsg := ""
	if input.NewMessage != nil {
		userMsg = input.NewMessage.Content
	} else if len(input.Session.Messages) > 0 {
		last := input.Session.Messages[len(input.Session.Messages)-1]
		if last.Role == types.RoleUser {
			userMsg = last.Content
		}
	}

	cmd := rpcCommand{
		ID:      "1",
		Type:    "prompt",
		Message: userMsg,
	}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		proc.Close()
		return nil, fmt.Errorf("marshal prompt command: %w", err)
	}
	if _, err := fmt.Fprintf(proc.Stdin(), "%s\n", cmdBytes); err != nil {
		proc.Close()
		return nil, fmt.Errorf("write prompt command: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer proc.Close()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseEvent(frame.Data)
			if !ok {
				continue
			}
			if evt.Type == types.EventTurnEnd && evt.StopReason == "agent_end" {
				// agent_end means the session ended, don't forward as turn_end
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
			select {
			case events <- types.Event{Type: types.EventError, Error: err}:
			default:
			}
		}
	}()

	return events, nil
}

func parseEvent(data []byte) (types.Event, bool) {
	var raw rawEvent
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
			// Accumulate tool call deltas — we emit the tool call when we get
			// a tool_execution_start event with the full name and ID.
			return types.Event{}, false
		}

	case "tool_execution_start":
		return types.Event{
			Type:       types.EventToolCall,
			ToolCallID: raw.ToolCallID,
			ToolName:   raw.ToolName,
		}, true

	case "tool_execution_end":
		// Pi sends tool results as separate events.
		resultJSON, _ := json.Marshal(raw.Data)
		return types.Event{
			Type:             types.EventToolResult,
			ToolResultID:     raw.ToolCallID,
			ToolResultOutput: string(resultJSON),
			ToolResultError:  raw.IsError,
		}, true

	case "turn_end":
		return types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: raw.TurnIndex + 1,
			StopReason: "end_turn",
		}, true

	case "agent_end":
		return types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: 0,
			StopReason: "agent_end",
		}, true

	case "response":
		// Command response — skip
		return types.Event{}, false
	}

	return types.Event{}, false
}
