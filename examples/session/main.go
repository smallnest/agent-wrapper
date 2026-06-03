// session 演示 session resume 流程。
//
// 第一次 run 获取 agent runtime 的 session ID，
// 第二次 run 用 session ID 恢复上下文。
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
	"github.com/smallnest/agent-wrapper/provider/claude"
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

	// Turn 1: one-shot run.
	// Agent runtime generates a session ID, returned in RunResult.
	fmt.Println("=== Turn 1 ===")
	result1, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt: "list files in current directory",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run 1: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Response: %s\n", result1.Text)
	fmt.Printf("SessionID: %s\n\n", result1.SessionID)

	// Turn 2: resume the same agent session.
	// Pass SessionID to resume the agent's internal conversation history.
	fmt.Println("=== Turn 2 (resumed) ===")
	result2, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt:    "explain the most important file",
		SessionID: result1.SessionID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run 2: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Response: %s\n", result2.Text)
	fmt.Printf("SessionID: %s\n", result2.SessionID)
}
