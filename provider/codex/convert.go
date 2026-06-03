package codex

import (
	"encoding/json"

	"github.com/smallnest/agent-wrapper/types"
)

// messagesToOpenAI converts []Message to OpenAI Chat Completions messages.
func messagesToOpenAI(msgs []types.Message) []map[string]any {
	result := make([]map[string]any, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case types.RoleUser:
			result = append(result, map[string]any{
				"role":    "user",
				"content": msg.Content,
			})
		case types.RoleAssistant:
			m := map[string]any{
				"role":    "assistant",
				"content": msg.Content,
			}
			if msg.ToolCallID != "" {
				m["tool_calls"] = []any{
					map[string]any{
						"id":   msg.ToolCallID,
						"type": "function",
						"function": map[string]any{
							"name":      msg.ToolName,
							"arguments": string(msg.ToolInput),
						},
					},
				}
			}
			result = append(result, m)
		case types.RoleToolResult:
			result = append(result, map[string]any{
				"role":         "tool",
				"tool_call_id": msg.ToolCallResultID,
				"content":      msg.Content,
			})
		case types.RoleToolUse:
			result = append(result, map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{
					map[string]any{
						"id":   msg.ToolCallID,
						"type": "function",
						"function": map[string]any{
							"name":      msg.ToolName,
							"arguments": string(msg.ToolInput),
						},
					},
				},
			})
		}
	}
	return result
}

// openAIToMessages converts OpenAI Chat Completions messages back to []Message.
func openAIToMessages(msgs []map[string]any) []types.Message {
	result := make([]types.Message, 0, len(msgs))
	for _, msg := range msgs {
		role, _ := msg["role"].(string)
		switch role {
		case "system", "user":
			content, _ := msg["content"].(string)
			if role == "user" {
				result = append(result, types.NewUserMessage(content))
			}
			// system messages are skipped (handled separately)
		case "assistant":
			content, _ := msg["content"].(string)
			if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				for _, tc := range toolCalls {
					tcMap, _ := tc.(map[string]any)
					id, _ := tcMap["id"].(string)
					fn, _ := tcMap["function"].(map[string]any)
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					result = append(result, types.NewToolUseMessage(id, name, json.RawMessage(args)))
				}
			} else if content != "" {
				result = append(result, types.NewAssistantMessage(content))
			}
		case "tool":
			content, _ := msg["content"].(string)
			toolCallID, _ := msg["tool_call_id"].(string)
			result = append(result, types.NewToolResultMessage(toolCallID, content, false))
		}
	}
	return result
}
