# agent-wrapper

Go 语言统一 coding agent 包装库。提供一致的接口驱动 Claude Code、Codex、Pi Agent、OpenCode 等 coding agent CLI。

## 为什么

每个 coding agent CLI 有各自的协议、认证和生命周期。agent-wrapper 提供统一的 `Agent` 接口，换 agent 就像换一个参数。

## 快速开始

```go
package main

import (
    "context"
    "fmt"

    agentwrapper "github.com/smallnest/agent-wrapper"
    "github.com/smallnest/agent-wrapper/claude"
    "github.com/smallnest/agent-wrapper/sessionstore/memory"
    "github.com/smallnest/agent-wrapper/types"
)

func main() {
    registry := agentwrapper.NewRegistry()
    claude.RegisterIn(registry)

    agent, _ := registry.Get("claude-code", nil)
    store := memory.New()
    session, _ := store.Create()

    orch := agentwrapper.NewOrchestrator(agent, store)
    events, _ := orch.Run(context.Background(), types.RunInput{
        Session:    session,
        NewMessage: func() *types.Message { m := types.NewUserMessage("你好"); return &m }(),
    })

    for evt := range events {
        if evt.Type == types.EventTextDelta {
            fmt.Print(evt.TextDelta)
        }
    }
}
```

## 架构

```
┌──────────────────────────────────────────────────┐
│ 调用方 (Go code / CLI)                            │
└──────────┬───────────────────────────────────────┘
           │
┌──────────▼───────────────────────────────────────┐
│              Agent Wrapper                        │
│                                                   │
│  ┌──────────┐  ┌───────────────┐  ┌───────────┐ │
│  │ Registry │  │ Orchestrator  │  │  Session  │ │
│  │          │  │  turn loop    │  │  Store    │ │
│  │ claude───┼──┤  +hooks       │  │  (memory) │ │
│  │ codex    │  │               │  │           │ │
│  │ pi       │  │               │  │           │ │
│  │ opencode │  │               │  │           │ │
│  └──────────┘  └───────┬───────┘  └───────────┘ │
│                        │                          │
│  ┌─────────────────────▼────────────────────────┐ │
│  │    Agent Implementations  (subprocess)       │ │
│  │  ClaudeCodeAgent  CodexAgent  PiAgent        │ │
│  │  OpenCodeAgent    Process Manager            │ │
│  └──────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
           │
     ┌─────▼──────────────┐
     │  Agent CLI 进程     │
     │  claude / codex /   │
     │  pi / opencode      │
     └────────────────────┘
```

## 核心概念

- **Agent** — 统一接口，通过子进程驱动各 agent CLI
- **Orchestrator** — 多 turn 对话编排，含审批、预算控制、session 回写
- **Registry** — Provider 注册表，按名称创建 Agent 实例
- **SessionStore** — Session 持久化（内置内存实现）
- **Event** — 统一事件流（TextDelta / ToolCall / ToolResult / TurnEnd / Error）

## Provider 支持

| Provider | 协议 | 流式 | 工具调用 | Token 用量 |
|----------|------|------|---------|-----------|
| Claude Code | JSON-RPC 2.0 | ✅ | ✅ | ✅ |
| Codex | SSE (OpenAI) | ✅ | ✅ | ✅ |
| Pi Agent | JSONL (RPC) | ✅ | ✅ | ❌ |
| OpenCode | 非交互模式 | ❌ | ❌ | ❌ |

## CLI

```bash
go build ./cmd/agent-wrapper

# 运行 agent（默认流式输出）
./agent-wrapper run --provider claude-code "解释这段代码"

# JSON 聚合输出（适合脚本/CI）
./agent-wrapper run --provider codex "修复 bug" --json

# NDJSON 流式输出（适合管道处理）
./agent-wrapper run --provider claude-code "hello" --json --stream | jq .

# 自动审批 + 预算限制
./agent-wrapper run --provider codex "修复 bug" --approve-all --budget-tokens 5000

# 列出 provider
./agent-wrapper list

# 查看版本
./agent-wrapper version
```

### 输出格式

| Flags | 模式 | 输出 |
|-------|------|------|
| (默认) | stream | 文本→stdout，元数据→stderr |
| `--json` | aggregated JSON | `{"text":"...","usage":{...},"session_id":"..."}` |
| `--json --stream` | stream-json (NDJSON) | 每行一个 Event JSON |

## 示例

| 示例 | 说明 |
|------|------|
| [basic](examples/basic/) | 最简调用 |
| [multi-turn](examples/multi-turn/) | 多 turn 上下文累积 |
| [approval](examples/approval/) | 交互式审批 |
| [budget](examples/budget/) | 预算限制 |
| [custom-provider](examples/custom-provider/) | 自定义 provider |

## 文档

| 文档 | 说明 |
|------|------|
| [快速开始](docs/quickstart.md) | 5 分钟上手 |
| [架构设计](docs/architecture.md) | 架构与接口说明 |
| [Session 机制](docs/session.md) | Session 详解 |
| [Provider 对比](docs/providers.md) | 各 provider 特性 |
| [审批流程](docs/approval.md) | 审批详解 |
| [自定义 Provider](docs/custom-provider.md) | 编写自定义 provider |

## 状态

| Issue | 状态 |
|-------|------|
| [#1](https://github.com/smallnest/agent-wrapper/issues/1) Core Types | ✅ |
| [#2](https://github.com/smallnest/agent-wrapper/issues/2) Process Manager | ✅ |
| [#3](https://github.com/smallnest/agent-wrapper/issues/3) MemorySessionStore | ✅ |
| [#4](https://github.com/smallnest/agent-wrapper/issues/4) Registry | ✅ |
| [#5](https://github.com/smallnest/agent-wrapper/issues/5) ClaudeCodeAgent | ✅ |
| [#6](https://github.com/smallnest/agent-wrapper/issues/6) CodexAgent | ✅ |
| [#7](https://github.com/smallnest/agent-wrapper/issues/7) PiAgent | ✅ |
| [#8](https://github.com/smallnest/agent-wrapper/issues/8) OpenCodeAgent | ✅ |
| [#9](https://github.com/smallnest/agent-wrapper/issues/9) Orchestrator | ✅ |
| [#10](https://github.com/smallnest/agent-wrapper/issues/10) CLI | ✅ |
| [#11](https://github.com/smallnest/agent-wrapper/issues/11) Docs + Examples | ✅ |

## License

MIT
