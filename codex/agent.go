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
	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures a CodexAgent.
type Options struct {
	BinaryPath string         // path to codex binary; empty = auto-detect
	Model      string         // model name (default: "codex-mini-latest")
	Extra      map[string]any // provider-specific parameters
}

// CodexAgent drives the OpenAI Codex CLI via SSE (OpenAI Chat Completions).
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

// chatRequest is an OpenAI Chat Completions request.
type chatRequest struct {
	Model    string         `json:"model"`
	Messages []map[string]any `json:"messages"`
	Stream   bool           `json:"stream"`
}

// sseChunk is a minimal SSE chunk from OpenAI Chat Completions streaming.
type sseChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// RegisterIn replaces the codex stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("codex", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts a codex chat subprocess and returns an event channel.
func (a *CodexAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	model := a.opts.Model
	if model == "" {
		model = "codex-mini-latest"
	}

	args := []string{"chat", "--model", model}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start codex process: %w", err)
	}

	messages := input.Session.Messages
	if input.NewMessage != nil {
		messages = append(messages, *input.NewMessage)
	}

	openAIMsgs := messagesToOpenAI(messages)

	// Prepend system prompt if provided.
	if input.SystemPrompt != "" {
		openAIMsgs = append([]map[string]any{
			{"role": "system", "content": input.SystemPrompt},
		}, openAIMsgs...)
	}

	req := chatRequest{
		Model:    model,
		Messages: openAIMsgs,
		Stream:   true,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		proc.Close()
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	if _, err := proc.Stdin().Write(reqBytes); err != nil {
		proc.Close()
		return nil, fmt.Errorf("write chat request: %w", err)
	}

	if closer, ok := proc.Stdin().(interface{ Close() error }); ok {
		closer.Close()
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer proc.Close()

		scanner := process.NewSSEScanner(proc.Stdout())
		turnNumber := 0
		var totalUsage types.TokenUsage

		for scanner.Scan() {
			frame := scanner.Frame()

			// [DONE] signal
			if frame.Data == nil {
				turnNumber++
				evt := types.Event{
					Type:       types.EventTurnEnd,
					TurnNumber: turnNumber,
					StopReason: "end_turn",
				}
				if totalUsage.TotalTokens > 0 {
					evt.TokenUsage = &types.TokenUsage{
						InputTokens:  totalUsage.InputTokens,
						OutputTokens: totalUsage.OutputTokens,
						TotalTokens:  totalUsage.TotalTokens,
					}
				}
				select {
				case events <- evt:
				case <-ctx.Done():
				}
				continue
			}

			var chunk sseChunk
			if err := json.Unmarshal(frame.Data, &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				totalUsage.InputTokens = chunk.Usage.PromptTokens
				totalUsage.OutputTokens = chunk.Usage.CompletionTokens
				totalUsage.TotalTokens = chunk.Usage.TotalTokens
			}

			for _, choice := range chunk.Choices {
				// Text delta
				if choice.Delta.Content != "" {
					evt := types.Event{
						Type:      types.EventTextDelta,
						TextDelta: choice.Delta.Content,
					}
					select {
					case events <- evt:
					case <-ctx.Done():
						return
					}
				}

				// Tool calls
				for _, tc := range choice.Delta.ToolCalls {
					if tc.ID != "" && tc.Function.Name != "" {
						evt := types.Event{
							Type:       types.EventToolCall,
							ToolCallID: tc.ID,
							ToolName:   tc.Function.Name,
							ToolInput:  json.RawMessage(tc.Function.Arguments),
						}
						select {
						case events <- evt:
						case <-ctx.Done():
							return
						}
					}
				}

				// Finish reason — emit TurnEnd
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					turnNumber++
					evt := types.Event{
						Type:       types.EventTurnEnd,
						TurnNumber: turnNumber,
						StopReason: *choice.FinishReason,
					}
					if totalUsage.TotalTokens > 0 {
						evt.TokenUsage = &types.TokenUsage{
							InputTokens:  totalUsage.InputTokens,
							OutputTokens: totalUsage.OutputTokens,
							TotalTokens:  totalUsage.TotalTokens,
						}
					}
					select {
					case events <- evt:
					case <-ctx.Done():
						return
					}
				}
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
