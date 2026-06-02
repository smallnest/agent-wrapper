// custom-provider 演示如何编写和注册自定义 provider。
//
// 使用方法:
//   go run main.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/sessionstore/memory"
	"github.com/smallnest/agent-wrapper/types"
)

// EchoAgent 是一个最简的 Agent 实现，将用户消息原样返回。
type EchoAgent struct{}

func NewEchoAgent() *EchoAgent { return &EchoAgent{} }

func (a *EchoAgent) Name() string         { return "Echo" }
func (a *EchoAgent) Provider() types.Provider { return "echo" }
func (a *EchoAgent) Close() error         { return nil }

func (a *EchoAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	ch := make(chan types.Event, 4)

	go func() {
		defer close(ch)

		prompt := ""
		if input.NewMessage != nil {
			prompt = input.NewMessage.Content
		} else if len(input.Session.Messages) > 0 {
			for i := len(input.Session.Messages) - 1; i >= 0; i-- {
				if input.Session.Messages[i].Role == types.RoleUser {
					prompt = input.Session.Messages[i].Content
					break
				}
			}
		}

		// 模拟流式输出：逐字发送
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
	// 创建 Registry 并注册自定义 provider
	registry := agentwrapper.NewRegistry()
	err := registry.Register("echo", func(opts map[string]any) (agentwrapper.Agent, error) {
		return NewEchoAgent(), nil
	}, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "register: %v\n", err)
		os.Exit(1)
	}

	// 通过 Registry 创建 agent
	agent, err := registry.Get("echo", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get agent: %v\n", err)
		os.Exit(1)
	}

	// 创建 session 和 orchestrator
	store := memory.New()
	session, err := store.Create()
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		os.Exit(1)
	}

	orch := agentwrapper.NewOrchestrator(agent, store)
	events, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("hello world"); return &m }(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("响应: ")
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventTurnEnd:
			fmt.Printf("\n[turn %d 结束, reason=%s]\n", evt.TurnNumber, evt.StopReason)
		}
	}

	fmt.Printf("\nsession ID: %s\n", session.ID)
	fmt.Printf("session 消息数: %d\n", len(session.Messages))
}
