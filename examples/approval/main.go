// approval 演示交互式审批流程。
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
	"github.com/smallnest/agent-wrapper/sessionstore/memory"
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

	store := memory.New()
	session, err := store.Create()
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		os.Exit(1)
	}

	// 配置审批 handler：只允许只读工具，拒绝写操作
	orch := agentwrapper.NewOrchestrator(agent, store,
		agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
			fmt.Printf("\n🔧 Agent 请求执行工具: %s\n", call.Name)
			fmt.Printf("   参数: %s\n", string(call.Input))

			switch call.Name {
			case "read", "ls", "grep", "glob", "view":
				fmt.Println("   ✅ 只读工具 → 允许")
				return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
			default:
				fmt.Println("   ❌ 写操作工具 → 拒绝")
				return &agentwrapper.Decision{
					Action: agentwrapper.ActionDeny,
					Reason: "只允许只读工具，写入操作被禁止",
				}, nil
			}
		}),
	)

	events, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("帮我创建一个 hello.go 文件"); return &m }(),
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
			fmt.Printf("\n[tool_call] %s\n", evt.ToolName)
		case types.EventToolResult:
			if evt.ToolResultError {
				fmt.Printf("[tool_denied] %s\n", evt.ToolResultOutput)
			} else {
				fmt.Printf("[tool_result] %s\n", truncate(evt.ToolResultOutput, 100))
			}
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
