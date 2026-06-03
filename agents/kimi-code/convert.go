package kimicode

import (
	"encoding/json"
	"fmt"

	"github.com/smallnest/agent-wrapper/types"
)

func parseKimiEvent(data []byte) (types.Event, bool) {
	var raw kimiEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return types.Event{}, false
	}

	switch raw.Type {
	case "system":
		return types.Event{SessionID: raw.SessionID}, true

	case "assistant":
		if raw.Message == nil {
			return types.Event{}, false
		}
		for _, c := range raw.Message.Content {
			switch c.Type {
			case "text":
				return types.Event{Type: types.EventTextDelta, TextDelta: c.Text}, true
			case "tool_use":
				return types.Event{
					Type:       types.EventToolCall,
					ToolCallID: c.ID,
					ToolName:   c.Name,
					ToolInput:  c.Input,
				}, true
			case "tool_result":
				return types.Event{
					Type:             types.EventToolResult,
					ToolResultID:     c.ToolUseID,
					ToolResultOutput: c.Text,
					ToolResultError:  c.IsError,
				}, true
			}
		}

	case "result":
		evt := types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: 1,
			StopReason: raw.StopReason,
		}
		if raw.StopReason == "" {
			evt.StopReason = "end_turn"
		}
		if raw.Usage != nil {
			evt.TokenUsage = &types.TokenUsage{
				InputTokens:  raw.Usage.InputTokens,
				OutputTokens: raw.Usage.OutputTokens,
				TotalTokens:  raw.Usage.InputTokens + raw.Usage.OutputTokens,
			}
		}
		return evt, true

	case "error":
		return types.Event{
			Type:  types.EventError,
			Error: fmt.Errorf("kimi-code: %s", raw.Result),
		}, true
	}

	return types.Event{}, false
}
