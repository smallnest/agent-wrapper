package agentwrapper

import (
	"context"
	"encoding/json"
)

// Action 表示审批决策的动作。
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
	ActionAbort Action = "abort"
)

// Decision 是审批回调的返回结果。
type Decision struct {
	Action Action `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// ToolCall 表示 agent 请求的一个工具调用。
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ApprovalHandler 在工具执行前被调用，决定是否允许执行。
type ApprovalHandler func(ctx context.Context, call ToolCall) (*Decision, error)
