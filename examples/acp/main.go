// acp 演示通过 ACP 协议连接 coding agent。
//
// 使用 acpx 或任意 ACP 兼容二进制，通过 ACP JSON-RPC 通信。
// 前置条件: npm install -g acpx 或安装其他 ACP agent。
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
	"github.com/smallnest/agent-wrapper/agents/acp"
	"github.com/smallnest/agent-wrapper/types"
)

func main() {
	registry := agentwrapper.NewRegistry()
	_ = acp.RegisterIn(registry)

	agent, err := registry.Get("acp", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get agent: %v\n", err)
		os.Exit(1)
	}

	orch := agentwrapper.NewOrchestrator(agent)

	// Stream events via Run.
	fmt.Println("=== Streaming (Run) ===")
	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "say hello in one sentence",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	var sid string
	for evt := range events {
		if evt.SessionID != "" {
			sid = evt.SessionID
		}
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventToolCall:
			fmt.Printf("\n[tool] %s\n", evt.ToolName)
		case types.EventTurnEnd:
			fmt.Println()
			if evt.TokenUsage != nil {
				fmt.Printf("[tokens: in=%d out=%d total=%d]\n",
					evt.TokenUsage.InputTokens,
					evt.TokenUsage.OutputTokens,
					evt.TokenUsage.TotalTokens,
				)
			}
		case types.EventError:
			fmt.Fprintf(os.Stderr, "[error] %v\n", evt.Error)
		}
	}

	if sid != "" {
		fmt.Printf("session: %s\n", sid)

		// Resume the same session.
		fmt.Println("\n=== Resume (RunSync) ===")
		result, err := orch.RunSync(context.Background(), types.RunInput{
			Prompt:    "what did I just ask?",
			SessionID: sid,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "resume: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Resumed: %s\n", result.Text)
	}
}
