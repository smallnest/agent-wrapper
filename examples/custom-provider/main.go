package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

// EchoAgent returns the prompt as-is.
type EchoAgent struct{}

func NewEchoAgent() *EchoAgent { return &EchoAgent{} }

func (a *EchoAgent) Name() string             { return "Echo" }
func (a *EchoAgent) Provider() types.Provider { return "echo" }
func (a *EchoAgent) Close() error             { return nil }

func (a *EchoAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	ch := make(chan types.Event, 4)

	go func() {
		defer close(ch)

		prompt := input.Prompt
		response := "Echo: " + strings.ToUpper(prompt)
		for _, r := range response {
			select {
			case ch <- types.Event{Type: types.EventTextDelta, TextDelta: string(r)}:
			case <-ctx.Done():
				ch <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
			}
		}

		ch <- types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: 1,
			StopReason: "end_turn",
		}
	}()

	return ch, nil
}

func main() {
	registry := agentwrapper.NewRegistry()
	err := registry.Register("echo", func(opts map[string]any) (agentwrapper.Agent, error) {
		return NewEchoAgent(), nil
	}, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "register: %v\n", err)
		os.Exit(1)
	}

	agent, err := registry.Get("echo", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get agent: %v\n", err)
		os.Exit(1)
	}

	orch := agentwrapper.NewOrchestrator(agent)
	events, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "hello world",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("Response: ")
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventTurnEnd:
			fmt.Printf("\n[turn %d end, reason=%s]\n", evt.TurnNumber, evt.StopReason)
		}
	}
}
