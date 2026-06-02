// multi-turn 演示多 turn 对话中的上下文累积。
//
// 使用方法:
//   go run main.go
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
	claude.RegisterIn(registry)

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

	// 第一次对话
	orch := agentwrapper.NewOrchestrator(agent, store)
	fmt.Println("=== 第一轮对话 ===")
	runTurn(orch, session, "我叫小明")

	// 第二次对话 — 使用同一个 session，agent 能记住之前的上下文
	fmt.Println("\n=== 第二轮对话 ===")
	runTurn(orch, session, "我叫什么名字？")

	// 打印 session 中的所有消息
	fmt.Println("\n=== Session 消息历史 ===")
	for i, msg := range session.Messages {
		fmt.Printf("[%d] %s: %s\n", i, msg.Role, truncate(msg.Content, 80))
	}
}

func runTurn(orch *agentwrapper.Orchestrator, session *types.Session, prompt string) {
	events, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage(prompt); return &m }(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		return
	}

	for evt := range events {
		if evt.Type == types.EventTextDelta {
			fmt.Print(evt.TextDelta)
		}
	}
	fmt.Println()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
