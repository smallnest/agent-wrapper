package agentwrapper

import (
	"context"
	"fmt"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

// Orchestrator drives multi-turn agent conversations with approval,
// budget control, session context accumulation, and context-length
// retry with compression.
type Orchestrator struct {
	agent      Agent
	store      SessionStore
	approval   ApprovalHandler
	budget     BudgetHandler
	compressor ContextCompressor
	maxRetries int
}

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithApprovalHandler sets the tool approval handler.
// If not set, all tool calls are allowed by default.
func WithApprovalHandler(h ApprovalHandler) OrchestratorOption {
	return func(o *Orchestrator) { o.approval = h }
}

// WithBudgetHandler sets the budget handler called after each turn.
func WithBudgetHandler(h BudgetHandler) OrchestratorOption {
	return func(o *Orchestrator) { o.budget = h }
}

// WithContextCompressor sets the compressor used when retrying after
// a context-length error. If not set, defaults to a chained compressor:
// SlidingWindowCompressor(20) → SummaryCompressor(20, nil).
func WithContextCompressor(c ContextCompressor) OrchestratorOption {
	return func(o *Orchestrator) { o.compressor = c }
}

// WithMaxRetries sets the maximum number of retry attempts after a
// context-length error. If not set, defaults to 3. When 0, no retry
// is attempted even when the error matches.
func WithMaxRetries(n int) OrchestratorOption {
	if n < 0 {
		n = 0
	}
	return func(o *Orchestrator) { o.maxRetries = n }
}

// NewOrchestrator creates an Orchestrator for the given agent and session store.
func NewOrchestrator(agent Agent, store SessionStore, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		agent:      agent,
		store:      store,
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

// Run executes the orchestrator loop: appends NewMessage to the session,
// calls Agent.Run with context-length retry, processes the event stream
// with approval/budget/writeback, and returns a downstream event channel.
func (o *Orchestrator) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	session := input.Session
	if session == nil {
		return nil, fmt.Errorf("orchestrator: session is required")
	}

	// Append NewMessage to session.
	newMessage := input.NewMessage
	if newMessage != nil {
		session.Messages = append(session.Messages, *newMessage)
		session.UpdatedAt = time.Now()
	}

	// Build agentInput (without NewMessage — already appended).
	agentInput := types.RunInput{
		Session:      session,
		NewMessage:   nil,
		SystemPrompt: input.SystemPrompt,
		WorkingDir:   input.WorkingDir,
		MaxTurns:     input.MaxTurns,
		AllowedTools: input.AllowedTools,
		Extra:        input.Extra,
	}

	// Retry loop: on ContextLengthExceededError, compress and retry.
	eventCh, err := o.runAgentWithRetry(ctx, agentInput, session, newMessage)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: agent run: %w", err)
	}

	out := make(chan types.Event, 64)

	go func() {
		defer close(out)

		var (
			turnNumber    int
			assistantText string
			turnMessages  []types.Message
		)

		forward := func(evt types.Event) {
			select {
			case out <- evt:
			case <-ctx.Done():
			}
		}

		writeback := func() {
			if len(turnMessages) == 0 && assistantText == "" {
				return
			}
			if assistantText != "" {
				session.Messages = append(session.Messages, types.NewAssistantMessage(assistantText))
			}
			session.Messages = append(session.Messages, turnMessages...)
			session.UpdatedAt = time.Now()

			assistantText = ""
			turnMessages = nil

			if o.store != nil {
				_ = o.store.Save(session)
			}
		}

		for evt := range eventCh {
			switch evt.Type {
			case types.EventTextDelta:
				assistantText += evt.TextDelta
				forward(evt)

			case types.EventToolCall:
				decision := o.decideApproval(ctx, evt)
				switch decision.Action {
				case ActionDeny:
					reason := decision.Reason
					if reason == "" {
						reason = "denied"
					}
					synthetic := types.NewToolResultMessage(evt.ToolCallID, "DENIED: "+reason, true)
					turnMessages = append(turnMessages,
						types.NewToolUseMessage(evt.ToolCallID, evt.ToolName, evt.ToolInput),
						synthetic,
					)
					forward(evt)
					forward(types.Event{
						Type:             types.EventToolResult,
						ToolResultID:     evt.ToolCallID,
						ToolResultOutput: synthetic.Content,
						ToolResultError:  true,
					})

				case ActionAbort:
					writeback()
					forward(types.Event{
						Type:       types.EventTurnEnd,
						TurnNumber: turnNumber,
						StopReason: "aborted",
					})
					return

				default: // ActionAllow
					turnMessages = append(turnMessages,
						types.NewToolUseMessage(evt.ToolCallID, evt.ToolName, evt.ToolInput),
					)
					forward(evt)
				}

			case types.EventToolResult:
				turnMessages = append(turnMessages, types.NewToolResultMessage(
					evt.ToolResultID, evt.ToolResultOutput, evt.ToolResultError,
				))
				forward(evt)

			case types.EventTurnEnd:
				turnNumber++
				if evt.TurnNumber > 0 {
					turnNumber = evt.TurnNumber
				}

				writeback()

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
				writeback()
				forward(evt)
				return
			}

			select {
			case <-ctx.Done():
				writeback()
				forward(types.Event{Type: types.EventError, Error: ctx.Err()})
				return
			default:
			}
		}
	}()

	return out, nil
}

// runAgentWithRetry calls agent.Run with retry on context-length errors.
func (o *Orchestrator) runAgentWithRetry(
	ctx context.Context,
	agentInput types.RunInput,
	session *types.Session,
	newMessage *types.Message,
) (<-chan types.Event, error) {
	for attempt := 0; ; attempt++ {
		eventCh, err := o.agent.Run(ctx, agentInput)
		if err == nil {
			return eventCh, nil
		}

		if !IsContextLengthExceeded(err) || attempt >= o.maxRetries {
			return nil, err
		}

		// Compress and re-append the current turn's user message.
		session.Messages = o.compressor.Compress(session.Messages)
		if newMessage != nil {
			session.Messages = append(session.Messages, *newMessage)
		}
		agentInput.NewMessage = nil
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
