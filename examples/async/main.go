// async 演示 orch.Run 流式异步消费。
//
// 和 RunSync 不同，Run 返回事件通道，调用方可边输出边处理，
// 适合需要实时流式展示的交互式场景。
//
// 使用方法:
//
//	go run main.go
package main

import (
	"context"
	"fmt"
	"os"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/agents/claude"
	"github.com/smallnest/agent-wrapper/types"
)

func main() {
	registry := agentwrapper.NewRegistry()
	_ = claude.RegisterIn(registry)

	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get agent: %v\n", err)
		os.Exit(1)
	}

	orch := agentwrapper.NewOrchestrator(agent)

	// orch.Run returns a channel — consume events as they arrive.
	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt:   "list files and explain the most important one",
		MaxTurns: 3,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	var text string
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
			text += evt.TextDelta
		case types.EventToolCall:
			fmt.Printf("\n[tool] %s\n", evt.ToolName)
		case types.EventToolResult:
			fmt.Printf("[result] %s\n", truncStr(evt.ToolResultOutput, 80))
		case types.EventTurnEnd:
			if evt.TokenUsage != nil {
				fmt.Printf("[tokens: in=%d out=%d total=%d]\n",
					evt.TokenUsage.InputTokens,
					evt.TokenUsage.OutputTokens,
					evt.TokenUsage.TotalTokens,
				)
			}
			if evt.SessionID != "" {
				fmt.Printf("[session: %s]\n", evt.SessionID)
			}
		case types.EventError:
			fmt.Fprintf(os.Stderr, "[error] %v\n", evt.Error)
		}
	}
	fmt.Println()
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
