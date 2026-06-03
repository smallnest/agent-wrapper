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

	orch := agentwrapper.NewOrchestrator(agent,
		agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
			fmt.Printf("\n[tool] %s(%s)\n", call.Name, string(call.Input))
			switch call.Name {
			case "read", "ls", "grep", "glob", "view":
				return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
			default:
				return &agentwrapper.Decision{Action: agentwrapper.ActionDeny, Reason: "write tools blocked"}, nil
			}
		}),
	)

	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "create a hello.go file",
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
