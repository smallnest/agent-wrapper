package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures a ClaudeCodeAgent.
type Options struct {
	BinaryPath string         // path to claude binary; empty = auto-detect
	Model      string         // model name passed to --model
	Extra      map[string]any // provider-specific parameters
}

// ClaudeCodeAgent drives the Claude Code CLI via JSON-RPC 2.0.
type ClaudeCodeAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

const (
	methodTextDelta  = "notify/text_delta"
	methodToolUse    = "notify/tool_use"
	methodToolResult = "notify/tool_result"
	methodTurnEnd    = "notify/turn_end"
)

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

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rawNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Run starts a claude agent subprocess and returns an event channel.
func (a *ClaudeCodeAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	args := []string{"agent"}
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

	messages := input.Session.Messages
	if input.NewMessage != nil {
		messages = append(messages, *input.NewMessage)
	}

	contentBlocks := messagesToContentBlocks(messages)

	initReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"systemPrompt": input.SystemPrompt,
			"messages":     contentBlocks,
		},
	}
	initBytes, err := json.Marshal(initReq)
	if err != nil {
		proc.Close()
		return nil, fmt.Errorf("marshal initialize request: %w", err)
	}
	if _, err := fmt.Fprintf(proc.Stdin(), "%s\n", initBytes); err != nil {
		proc.Close()
		return nil, fmt.Errorf("write initialize: %w", err)
	}

	runReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "run",
		Params:  map[string]any{},
	}
	runBytes, err := json.Marshal(runReq)
	if err != nil {
		proc.Close()
		return nil, fmt.Errorf("marshal run request: %w", err)
	}
	if _, err := fmt.Fprintf(proc.Stdin(), "%s\n", runBytes); err != nil {
		proc.Close()
		return nil, fmt.Errorf("write run request: %w", err)
	}

	if closer, ok := proc.Stdin().(interface{ Close() error }); ok {
		closer.Close()
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer proc.Close()

		scanner := process.NewJSONRPCScanner(proc.Stdout())
		for scanner.Scan() {
			frame := scanner.Frame()
			evt, ok := parseNotification(frame.Data)
			if !ok {
				continue
			}
			select {
			case events <- evt:
			case <-ctx.Done():
				events <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
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

func parseNotification(data []byte) (types.Event, bool) {
	var raw rawNotification
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}
	if raw.Method == "" {
		return types.Event{}, false
	}

	switch raw.Method {
	case methodTextDelta:
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw.Params, &p); err != nil {
			return types.Event{Type: types.EventError, Error: fmt.Errorf("parse text_delta: %w", err)}, true
		}
		return types.Event{Type: types.EventTextDelta, TextDelta: p.Text}, true

	case methodToolUse:
		var p struct {
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw.Params, &p); err != nil {
			return types.Event{Type: types.EventError, Error: fmt.Errorf("parse tool_use: %w", err)}, true
		}
		return types.Event{
			Type: types.EventToolCall, ToolCallID: p.ID, ToolName: p.Name, ToolInput: p.Input,
		}, true

	case methodToolResult:
		var p struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			IsError bool   `json:"is_error"`
		}
		if err := json.Unmarshal(raw.Params, &p); err != nil {
			return types.Event{Type: types.EventError, Error: fmt.Errorf("parse tool_result: %w", err)}, true
		}
		return types.Event{
			Type: types.EventToolResult, ToolResultID: p.ID,
			ToolResultOutput: p.Content, ToolResultError: p.IsError,
		}, true

	case methodTurnEnd:
		var p struct {
			StopReason string `json:"stopReason"`
			TurnNumber int    `json:"turnNumber"`
			Usage      struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw.Params, &p); err != nil {
			return types.Event{Type: types.EventError, Error: fmt.Errorf("parse turn_end: %w", err)}, true
		}
		return types.Event{
			Type: types.EventTurnEnd, TurnNumber: p.TurnNumber, StopReason: p.StopReason,
			TokenUsage: &types.TokenUsage{
				InputTokens: p.Usage.InputTokens, OutputTokens: p.Usage.OutputTokens, TotalTokens: p.Usage.TotalTokens,
			},
		}, true
	}

	return types.Event{}, false
}
