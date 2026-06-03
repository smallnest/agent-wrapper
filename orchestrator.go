package agentwrapper

import (
	"context"
	"fmt"

	"github.com/smallnest/agent-wrapper/types"
)

// Orchestrator drives multi-turn agent conversations with approval,
// budget control, and context-length retry with compression.
type Orchestrator struct {
	agent      Agent
	approval   ApprovalHandler
	budget     BudgetHandler
	compressor ContextCompressor
	maxRetries int
}

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithApprovalHandler sets the tool approval handler.
func WithApprovalHandler(h ApprovalHandler) OrchestratorOption {
	return func(o *Orchestrator) { o.approval = h }
}

// WithBudgetHandler sets the budget handler called after each turn.
func WithBudgetHandler(h BudgetHandler) OrchestratorOption {
	return func(o *Orchestrator) { o.budget = h }
}

// WithContextCompressor sets the compressor used when retrying after
// a context-length error.
func WithContextCompressor(c ContextCompressor) OrchestratorOption {
	return func(o *Orchestrator) { o.compressor = c }
}

// WithMaxRetries sets the maximum number of retry attempts after a
// context-length error. Defaults to 3. Set to 0 to disable retry.
func WithMaxRetries(n int) OrchestratorOption {
	if n < 0 {
		n = 0
	}
	return func(o *Orchestrator) { o.maxRetries = n }
}

// NewOrchestrator creates an Orchestrator for the given agent.
func NewOrchestrator(agent Agent, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		agent:      agent,
		compressor: NewChainedCompressor(NewSlidingWindowCompressor(20), NewSummaryCompressor(20, nil)),
		maxRetries: 3,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Agent returns the underlying agent.
func (o *Orchestrator) Agent() Agent { return o.agent }

// Run executes the orchestrator loop: passes Prompt to Agent.Run with
// context-length retry, processes the event stream with approval/budget,
// and returns a downstream event channel.
func (o *Orchestrator) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	if input.Prompt == "" {
		return nil, fmt.Errorf("orchestrator: prompt is required")
	}

	eventCh, err := o.runAgentWithRetry(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: agent run: %w", err)
	}

	out := make(chan types.Event, 64)

	go func() {
		defer close(out)
		var turnNumber int

		forward := func(evt types.Event) {
			select {
			case out <- evt:
			case <-ctx.Done():
			}
		}

		for evt := range eventCh {
			switch evt.Type {
			case types.EventTextDelta:
				forward(evt)

			case types.EventToolCall:
				decision := o.decideApproval(ctx, evt)
				switch decision.Action {
				case ActionDeny:
					reason := decision.Reason
					if reason == "" {
						reason = "denied"
					}
					forward(evt)
					forward(types.Event{
						Type:             types.EventToolResult,
						ToolResultID:     evt.ToolCallID,
						ToolResultOutput: "DENIED: " + reason,
						ToolResultError:  true,
					})

				case ActionAbort:
					forward(types.Event{
						Type:       types.EventTurnEnd,
						TurnNumber: turnNumber,
						StopReason: "aborted",
					})
					return

				default:
					forward(evt)
				}

			case types.EventToolResult:
				forward(evt)

			case types.EventTurnEnd:
				turnNumber++
				if evt.TurnNumber > 0 {
					turnNumber = evt.TurnNumber
				}

				if o.budget != nil && evt.TokenUsage != nil {
					if err := o.budget(ctx, *evt.TokenUsage); err != nil {
						forward(evt)
						return
					}
				}

				forward(evt)

				if evt.StopReason == "end_turn" || evt.StopReason == "stop" {
					return
				}
				if input.MaxTurns > 0 && turnNumber >= input.MaxTurns {
					return
				}

			case types.EventError:
				forward(evt)
				return
			}

			select {
			case <-ctx.Done():
				forward(types.Event{Type: types.EventError, Error: ctx.Err()})
				return
			default:
			}
		}
	}()

	return out, nil
}

// RunSync calls Run and drains the event channel, returning an aggregated
// RunResult. On error or context cancellation, *RunResult is nil.
func (o *Orchestrator) RunSync(ctx context.Context, input types.RunInput) (*types.RunResult, error) {
	eventCh, err := o.Run(ctx, input)
	if err != nil {
		return nil, err
	}

	var text string
	var usage *types.TokenUsage
	var sessionID string

	for evt := range eventCh {
		if evt.SessionID != "" {
			sessionID = evt.SessionID
		}
		switch evt.Type {
		case types.EventTextDelta:
			text += evt.TextDelta
		case types.EventTurnEnd:
			if evt.TokenUsage != nil {
				usage = evt.TokenUsage
			}
		case types.EventError:
			if evt.Error != nil {
				return nil, evt.Error
			}
		}
	}
	if sessionID == "" {
		sessionID = input.SessionID
	}

	return &types.RunResult{
		Text:      text,
		Usage:     usage,
		SessionID: sessionID,
	}, nil
}

// runAgentWithRetry calls agent.Run with retry on context-length errors.
func (o *Orchestrator) runAgentWithRetry(
	ctx context.Context,
	input types.RunInput,
) (<-chan types.Event, error) {
	for attempt := 0; ; attempt++ {
		eventCh, err := o.agent.Run(ctx, input)
		if err == nil {
			return eventCh, nil
		}

		if !IsContextLengthExceeded(err) || attempt >= o.maxRetries {
			return nil, err
		}

		compressed := o.compressor.Compress([]types.Message{types.NewUserMessage(input.Prompt)})
		if len(compressed) > 0 && compressed[0].Role == types.RoleUser {
			input.Prompt = compressed[0].Content
		}
	}
}

func (o *Orchestrator) decideApproval(ctx context.Context, evt types.Event) *Decision {
	if o.approval == nil {
		return &Decision{Action: ActionAllow}
	}
	dec, err := o.approval(ctx, ToolCall{
		ID:    evt.ToolCallID,
		Name:  evt.ToolName,
		Input: evt.ToolInput,
	})
	if err != nil {
		return &Decision{Action: ActionDeny, Reason: err.Error()}
	}
	if dec == nil {
		return &Decision{Action: ActionAllow}
	}
	return dec
}
