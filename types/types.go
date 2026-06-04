package types

import (
	"encoding/json"
)

// Provider identifies the underlying agent implementation.
type Provider string

const (
	ProviderClaudeCode Provider = "claude-code"
	ProviderCodex      Provider = "codex"
	ProviderPiAgent    Provider = "pi-agent"
	ProviderOpenCode   Provider = "opencode"
	ProviderAgy        Provider = "agy"
	ProviderCursor     Provider = "cursor"
	ProviderKimiCode   Provider = "kimi-code"
)

// String returns the provider string identifier.
func (p Provider) String() string { return string(p) }

// AllProviders returns a list of all built-in providers.
func AllProviders() []Provider {
	return []Provider{
		ProviderClaudeCode,
		ProviderCodex,
		ProviderPiAgent,
		ProviderOpenCode,
		ProviderAgy,
		ProviderCursor,
		ProviderKimiCode,
	}
}

// Role represents the sender role of a message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolUse    Role = "tool_use"
	RoleToolResult Role = "tool_result"
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`

	// Fields for tool_use role only
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// Fields for tool_result role only
	ToolCallResultID string `json:"tool_call_result_id,omitempty"`
	IsToolError      bool   `json:"is_tool_error,omitempty"`
}

// NewUserMessage creates a user text message.
func NewUserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// NewAssistantMessage creates an assistant text message.
func NewAssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// NewToolUseMessage creates a tool_use message.
func NewToolUseMessage(callID, name string, input json.RawMessage) Message {
	return Message{
		Role:       RoleToolUse,
		ToolCallID: callID,
		ToolName:   name,
		ToolInput:  input,
	}
}

// NewToolResultMessage creates a tool_result message.
func NewToolResultMessage(callID, content string, isError bool) Message {
	return Message{
		Role:             RoleToolResult,
		Content:          content,
		ToolCallResultID: callID,
		IsToolError:      isError,
	}
}

// EventType represents the type of events produced by an agent.
type EventType string

const (
	EventTextDelta  EventType = "text_delta"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventTurnEnd    EventType = "turn_end"
	EventError      EventType = "error"
)

// Event is produced by an agent during execution.
type Event struct {
	Type EventType `json:"type"`

	// TextDelta: streaming text increment
	TextDelta string `json:"text_delta,omitempty"`

	// ToolCall: agent requests a tool invocation
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// ToolResult: tool execution completed
	ToolResultID     string `json:"tool_result_id,omitempty"`
	ToolResultOutput string `json:"tool_result_output,omitempty"`
	ToolResultError  bool   `json:"tool_result_error,omitempty"`

	// TurnEnd: a turn has ended
	TurnNumber int         `json:"turn_number,omitempty"`
	StopReason string      `json:"stop_reason,omitempty"`
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`

	// Error: error occurred during execution
	Error error `json:"error,omitempty"`

	// SessionID: agent runtime session ID (persistable for later resume)
	SessionID string `json:"session_id,omitempty"`
}

// TokenUsage represents token consumption of one LLM call.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// OutputFormat controls how the orchestrator or CLI emits results.
type OutputFormat string

const (
	OutputStream     OutputFormat = "stream"
	OutputJSON       OutputFormat = "json"
	OutputStreamJSON OutputFormat = "stream-json"
)

// RunResult is the aggregated result of an orchestrated agent run.
type RunResult struct {
	Text      string      `json:"text"`
	Usage     *TokenUsage `json:"usage"`
	SessionID string      `json:"session_id"`
}

// RunInput is the complete input for a single agent invocation.
type RunInput struct {
	Prompt       string
	SessionID    string
	SystemPrompt string
	WorkingDir   string
	MaxTurns     int
	AllowedTools []string
	Extra        map[string]any
	OutputFormat OutputFormat
}
