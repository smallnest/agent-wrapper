package types

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

// Provider 标识底层 agent 实现。
type Provider string

const (
	ProviderClaudeCode Provider = "claude-code"
	ProviderCodex      Provider = "codex"
	ProviderPiAgent    Provider = "pi-agent"
	ProviderOpenCode   Provider = "opencode"
)

// String 返回 provider 的字符串标识。
func (p Provider) String() string { return string(p) }

// AllProviders 返回所有内置 provider 的列表。
func AllProviders() []Provider {
	return []Provider{
		ProviderClaudeCode,
		ProviderCodex,
		ProviderPiAgent,
		ProviderOpenCode,
	}
}

// Role 表示消息的发送者角色。
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolUse    Role = "tool_use"
	RoleToolResult Role = "tool_result"
)

// Message 是会话中的单条消息。
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`

	// 以下字段仅 tool_use 角色使用
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// 以下字段仅 tool_result 角色使用
	ToolCallResultID string `json:"tool_call_result_id,omitempty"`
	IsToolError      bool   `json:"is_tool_error,omitempty"`
}

// NewUserMessage 创建一个用户文本消息。
func NewUserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// NewAssistantMessage 创建一个 assistant 文本消息。
func NewAssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// NewToolUseMessage 创建一个 tool_use 消息。
func NewToolUseMessage(callID, name string, input json.RawMessage) Message {
	return Message{
		Role:       RoleToolUse,
		ToolCallID: callID,
		ToolName:   name,
		ToolInput:  input,
	}
}

// NewToolResultMessage 创建一个 tool_result 消息。
func NewToolResultMessage(callID, content string, isError bool) Message {
	return Message{
		Role:             RoleToolResult,
		Content:          content,
		ToolCallResultID: callID,
		IsToolError:      isError,
	}
}

// Session 代表一个跨 turn 保持的会话上下文。
type Session struct {
	ID        string            `json:"id"`
	Messages  []Message         `json:"messages"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// SessionSummary 是 session 的元数据摘要，不含完整消息体。
type SessionSummary struct {
	ID           string    `json:"id"`
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewSession 创建一个空的 Session。
func NewSession() *Session {
	now := time.Now()
	return &Session{
		ID:        newUUID(),
		Messages:  make([]Message, 0),
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  make(map[string]string),
	}
}

// newUUID 生成 RFC 9562 UUID v4。
func newUUID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic(fmt.Sprintf("agentwrapper: crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// EventType 表示 agent 产生的事件的类型。
type EventType string

const (
	EventTextDelta  EventType = "text_delta"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventTurnEnd    EventType = "turn_end"
	EventError      EventType = "error"
)

// Event 是 agent 在运行过程中产生的事件。
type Event struct {
	Type EventType `json:"type"`

	// TextDelta: 流式文本增量
	TextDelta string `json:"text_delta,omitempty"`

	// ToolCall: agent 请求调用工具
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	// ToolResult: 工具执行完成
	ToolResultID     string `json:"tool_result_id,omitempty"`
	ToolResultOutput string `json:"tool_result_output,omitempty"`
	ToolResultError  bool   `json:"tool_result_error,omitempty"`

	// TurnEnd: 一个 turn 结束
	TurnNumber int         `json:"turn_number,omitempty"`
	StopReason string      `json:"stop_reason,omitempty"`
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`

	// Error: 运行中发生错误
	Error error `json:"error,omitempty"`
}

// TokenUsage 表示一次 LLM 调用的 token 用量。
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

// RunInput 是一次 agent 调用的全部输入。
type RunInput struct {
	Session      *Session
	NewMessage   *Message
	SystemPrompt string
	WorkingDir   string
	MaxTurns     int
	AllowedTools []string
	Extra        map[string]any
	OutputFormat OutputFormat
}
