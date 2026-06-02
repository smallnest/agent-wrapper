package agentwrapper

import (
	"context"
	"fmt"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

// Orchestrator drives multi-turn agent conversations with approval,
// budget control, and session context accumulation.
type Orchestrator struct {
	agent    Agent
	store    SessionStore
	approval ApprovalHandler
	budget   BudgetHandler
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

// NewOrchestrator creates an Orchestrator for the given agent and session store.
func NewOrchestrator(agent Agent, store SessionStore, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{agent: agent, store: store}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Agent returns the underlying agent.
func (o *Orchestrator) Agent() Agent { return o.agent }

// Run executes the orchestrator loop: appends NewMessage to the session,
// calls Agent.Run, processes the event stream with approval/budget/writeback,
// and returns a downstream event channel for the caller.
func (o *Orchestrator) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	session := input.Session
	if session == nil {
		return nil, fmt.Errorf("orchestrator: session is required")
	}

	// Append NewMessage to session.
	if input.NewMessage != nil {
		session.Messages = append(session.Messages, *input.NewMessage)
		session.UpdatedAt = time.Now()
	}

	// Build RunInput for the agent.
	agentInput := types.RunInput{
		Session:      session,
		NewMessage:   nil, // already appended
		SystemPrompt: input.SystemPrompt,
		WorkingDir:   input.WorkingDir,
		MaxTurns:     input.MaxTurns,
		AllowedTools: input.AllowedTools,
		Extra:        input.Extra,
	}

	eventCh, err := o.agent.Run(ctx, agentInput)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: agent run: %w", err)
	}

	out := make(chan types.Event, 64)

	go func() {
		defer close(out)

		var (
			turnNumber   int
			assistantText string
			turnMessages []types.Message
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
				o.store.Save(session)
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
