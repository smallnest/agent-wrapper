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

	// First turn.
	result1, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt:   "list the files in the current directory",
		MaxTurns: 2,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run 1: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Turn 1: %s\n", result1.Text)
	fmt.Printf("Session: %s\n", result1.SessionID)

	// Second turn resumes the same agent session.
	result2, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt:    "explain the most important file",
		SessionID: result1.SessionID,
		MaxTurns:  2,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run 2: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Turn 2: %s\n", result2.Text)
}
