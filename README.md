# agent-wrapper

Go 多 Agent 统一运行时 —— 一个接口驱动 Claude Code、Codex、Pi Agent、OpenCode。

## 为什么

每个 coding agent CLI 有各自的协议、认证和生命周期。agent-wrapper 提供统一的 `Agent` 接口，换 agent 就像换一个参数。

## 包结构

```
agentwrapper/          # package agentwrapper — 接口 + 回调类型
├── agent.go           # Agent interface, RunInput
├── session.go         # SessionStore interface
├── approval.go        # ApprovalHandler, Decision, ToolCall
├── budget.go          # BudgetHandler
└── types/             # package types — 纯数据模型，零依赖
    ├── types.go       # Provider, Role, Message, Session, Event, TokenUsage
    └── errors.go      # ExitError, ProtocolError, BudgetExceededError, ...
```

## 快速开始

```go
package main

import (
	"context"
	"fmt"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

func main() {
	// 用 types 包创建 session 和消息
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("帮我重构 main.go"))

	var agent agentwrapper.Agent // 后续 issue 实现具体 provider

	events, err := agent.Run(context.Background(), agentwrapper.RunInput{
		Session:      session,
		SystemPrompt: "You are a Go expert.",
		MaxTurns:     10,
	})
	if err != nil {
		panic(err)
	}
	for evt := range events {
		switch evt.Type {
		case types.EventTextDelta:
			fmt.Print(evt.TextDelta)
		case types.EventToolCall:
			fmt.Printf("\n[TOOL] %s\n", evt.ToolName)
		case types.EventTurnEnd:
			fmt.Printf("\n--- Turn %d end ---\n", evt.TurnNumber)
		}
	}
}
```

## 状态

| Issue | 状态 |
|-------|------|
| [#1](https://github.com/smallnest/agent-wrapper/issues/1) Core Types | ✅ 完成 |
| [#2](https://github.com/smallnest/agent-wrapper/issues/3) MemorySessionStore | 待开始 |
| [#3](https://github.com/smallnest/agent-wrapper/issues/4) Registry | 待开始 |
| [#4](https://github.com/smallnest/agent-wrapper/issues/2) Process Manager | 待开始 |
| [#5](https://github.com/smallnest/agent-wrapper/issues/5) ClaudeCodeAgent | 待开始 |
| [#6](https://github.com/smallnest/agent-wrapper/issues/6) CodexAgent | 待开始 |
| [#7](https://github.com/smallnest/agent-wrapper/issues/7) PiAgent | 待开始 |
| [#8](https://github.com/smallnest/agent-wrapper/issues/8) OpenCodeAgent | 待开始 |
| [#9](https://github.com/smallnest/agent-wrapper/issues/9) Orchestrator | 待开始 |
| [#10](https://github.com/smallnest/agent-wrapper/issues/10) CLI | 待开始 |
| [#11](https://github.com/smallnest/agent-wrapper/issues/11) Docs + Examples | 待开始 |

## 文档

- [PRD](tasks/prd-agent-wrapper.md)
- [SPEC](tasks/spec-agent-wrapper.md)

## License

MIT
