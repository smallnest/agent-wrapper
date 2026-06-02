// budget 演示 token 预算限制。
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

	budgetLimit := 5000
	var totalUsed int

	// 配置预算 handler：超过限制时终止
	orch := agentwrapper.NewOrchestrator(agent, store,
		agentwrapper.WithBudgetHandler(func(ctx context.Context, usage types.TokenUsage) error {
			totalUsed = usage.TotalTokens
			fmt.Fprintf(os.Stderr, "[预算] 已使用 %d / %d tokens\n", usage.TotalTokens, budgetLimit)
			if usage.TotalTokens > budgetLimit {
				return fmt.Errorf("预算耗尽: 已用 %d tokens，上限 %d", usage.TotalTokens, budgetLimit)
			}
			return nil
		}),
	)

	events, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("详细解释 Go 语言的并发模型"); return &m }(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventTurnEnd:
			fmt.Fprintln(os.Stderr)
		}
	}

	fmt.Fprintf(os.Stderr, "\n最终 token 用量: %d / %d\n", totalUsed, budgetLimit)
}
