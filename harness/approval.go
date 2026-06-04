package harness

import (
	"context"
	"encoding/json"
)

// Action represents an approval decision action.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
	ActionAbort Action = "abort"
)

// Decision is the result of an approval callback.
type Decision struct {
	Action Action `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ToolCall represents a tool invocation requested by the agent.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ApprovalHandler is called before a tool executes to decide whether to allow it.
type ApprovalHandler func(ctx context.Context, call ToolCall) (*Decision, error)
