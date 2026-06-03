// multi-turn 演示 orchestrator 多 turn 对话编排。
//
// Orchestrator 自动处理 agent 的 tool_use/tool_result 循环，
// 不必手动管理 session。
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
	"github.com/smallnest/agent-wrapper/claude"
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

	// Single prompt triggers multiple turns if agent needs tools.
	// Orchestrator handles the tool_use / tool_result loop internally.
	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt:   "list files in current directory and explain the most important one",
		MaxTurns: 3,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventToolCall:
			fmt.Printf("\n[tool] %s\n", evt.ToolName)
		case types.EventToolResult:
			fmt.Printf("[result] %s\n", truncStr(evt.ToolResultOutput, 100))
		case types.EventTurnEnd:
			fmt.Println("\n--- turn end ---")
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
