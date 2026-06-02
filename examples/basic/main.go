// basic 演示最简单的 agent-wrapper 调用。
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
	// 注册 provider
	registry := agentwrapper.NewRegistry()
	if err := claude.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "register: %v\n", err)
		os.Exit(1)
	}

	// 创建 agent
	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get agent: %v\n", err)
		os.Exit(1)
	}

	// 创建 session
	store := memory.New()
	session, err := store.Create()
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		os.Exit(1)
	}

	// 创建 orchestrator 并运行
	orch := agentwrapper.NewOrchestrator(agent, store)
	events, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("说你好"); return &m }(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	// 读取事件流
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventTurnEnd:
			fmt.Println("\n--- turn end ---")
		case types.EventError:
			fmt.Fprintf(os.Stderr, "error: %v\n", evt.Error)
		}
	}

	fmt.Printf("session: %s\n", session.ID)
}
