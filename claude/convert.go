package claude

import (
	"encoding/json"

	"github.com/smallnest/agent-wrapper/types"
)

// messagesToContentBlocks converts []Message to Anthropic content blocks.
func messagesToContentBlocks(msgs []types.Message) []map[string]any {
	blocks := make([]map[string]any, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case types.RoleUser:
			blocks = append(blocks, map[string]any{
				"type": "user",
				"text": msg.Content,
			})
		case types.RoleAssistant:
			blocks = append(blocks, map[string]any{
				"type": "assistant",
				"text": msg.Content,
			})
		case types.RoleToolUse:
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    msg.ToolCallID,
				"name":  msg.ToolName,
				"input": msg.ToolInput,
			})
		case types.RoleToolResult:
			blocks = append(blocks, map[string]any{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallResultID,
				"content":     msg.Content,
			})
		}
	}
	return blocks
}

// contentBlocksToMessages converts Anthropic content blocks back to []Message.
func contentBlocksToMessages(blocks []map[string]any) []types.Message {
	msgs := make([]types.Message, 0, len(blocks))
	for _, block := range blocks {
		typ, _ := block["type"].(string)
		switch typ {
		case "user":
			text, _ := block["text"].(string)
			msgs = append(msgs, types.NewUserMessage(text))
		case "assistant":
			text, _ := block["text"].(string)
			msgs = append(msgs, types.NewAssistantMessage(text))
		case "tool_use":
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			var input json.RawMessage
			if raw, ok := block["input"]; ok {
				input, _ = json.Marshal(raw)
			}
			msgs = append(msgs, types.NewToolUseMessage(id, name, input))
		case "tool_result":
			toolUseID, _ := block["tool_use_id"].(string)
			content, _ := block["content"].(string)
			isError := false
			if v, ok := block["is_error"].(bool); ok {
				isError = v
			}
			msgs = append(msgs, types.NewToolResultMessage(toolUseID, content, isError))
		}
	}
	return msgs
}
