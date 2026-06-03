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

	orch := agentwrapper.NewOrchestrator(agent,
		agentwrapper.WithBudgetHandler(func(ctx context.Context, usage types.TokenUsage) error {
			limit := 5000
			if usage.TotalTokens > limit {
				return fmt.Errorf("budget exceeded: %d/%d", usage.TotalTokens, limit)
			}
			fmt.Fprintf(os.Stderr, "[usage] %d total tokens\n", usage.TotalTokens)
			return nil
		}),
	)

	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "explain this code briefly",
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
			fmt.Println()
		}
	}
}
