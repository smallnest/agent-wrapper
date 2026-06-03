# agent-wrapper

Go 语言统一 coding agent 包装库。提供一致的接口驱动 Claude Code、Cursor Agent、Kimi Code、Antigravity、Codex、Pi Agent、OpenCode 等 coding agent CLI。

## 为什么

每个 coding agent CLI 有各自的协议、认证和生命周期。agent-wrapper 提供统一的 `Agent` 接口，换 agent 就像换一个参数。

## 快速开始

```go
package main

import (
    "context"
    "fmt"

    agentwrapper "github.com/smallnest/agent-wrapper"
    "github.com/smallnest/agent-wrapper/provider/claude"
    "github.com/smallnest/agent-wrapper/types"
)

func main() {
    registry := agentwrapper.NewRegistry()
    claude.RegisterIn(registry)

    agent, _ := registry.Get("claude-code", nil)
    orch := agentwrapper.NewOrchestrator(agent)

    // Stream events.
    events, _ := orch.Run(context.Background(), types.RunInput{
        Prompt: "你好",
    })
    for evt := range events {
        if evt.Type == types.EventTextDelta {
            fmt.Print(evt.TextDelta)
        }
    }

    // Or use sync API.
    result, _ := orch.RunSync(context.Background(), types.RunInput{
        Prompt:    "say hello",
        SessionID: "resume-this-session-uuid", // optional
    })
    fmt.Println(result.Text)
    fmt.Println(result.SessionID) // agent runtime session
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
│  │    8 pro─┼──┤  +hooks       │  │  (memory) │ │
│  │   viders │  │               │  │           │ │
│  └──────────┘  └───────┬───────┘  └───────────┘ │
│                        │ Agent.Run()              │
│  ┌─────────────────────▼────────────────────────┐ │
│  │    Provider Implementations  (subprocess)    │ │
│  │  claude  cursor  kimi-code  agy  codex       │ │
│  │  pi      opencode  acp                       │ │
│  │  (all in provider/ directory)                │ │
│  └──────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
           │
     ┌─────▼──────────────────────┐
     │  Agent CLI 进程             │
     │  claude / cursor / kimi /   │
     │  agy / codex / pi / acpx   │
     └────────────────────────────┘
```

## 核心概念

- **Agent** — 统一接口，通过子进程驱动各 agent CLI
- **Orchestrator** — 多 turn 对话编排，含审批、预算控制、上下文压缩重试
- **Registry** — Provider 注册表，按名称创建 Agent 实例
- **Event** — 统一事件流（TextDelta / ToolCall / ToolResult / TurnEnd / Error）
- **RunSync** — 同步 API，收集所有事件返回聚合 `RunResult`

## Provider 支持

| Provider | 协议 | 流式 | 工具调用 | Token 用量 | Session |
|----------|------|------|---------|-----------|---------|
| Claude Code | CLI stream-json (NDJSON) | ✅ | ✅ | ✅ | ✅ |
| Cursor Agent | CLI --print stream-json | ✅ | ✅ | ✅ | ✅ |
| Kimi Code | CLI --prompt stream-json | ✅ | ✅ | ✅ | ✅ |
| Antigravity | CLI --print (plain text) | ❌ | ❌ | ❌ | ✅ |
| Codex | CLI exec --json | ✅ | ✅ | ✅ | ✅ |
| Pi Agent | CLI --mode json | ✅ | ✅ | ✅ | ✅ |
| OpenCode | CLI run --format json | ✅ | ✅ | ✅ | ✅ |
| ACP | ACP JSON-RPC over stdio | ✅ | ✅ | ✅ | ✅ |

## CLI

```bash
go build ./cmd/agent-wrapper

# 运行 agent（默认流式输出）
./agent-wrapper run --provider claude-code "解释这段代码"

# JSON 聚合输出（适合脚本/CI）
./agent-wrapper run --provider codex "修复 bug" --json

# 恢复已有 session
./agent-wrapper run --provider claude-code --session-id abc123 "继续刚才的对话"

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
| [async](examples/async/) | 流式异步消费 orch.Run |
| [multi-turn](examples/multi-turn/) | 多 turn 上下文累积 |
| [session](examples/session/) | Session resume 跨调用恢复会话 |
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
| [#45](https://github.com/smallnest/agent-wrapper/issues/45) ACP provider | ✅ |
| [#47](https://github.com/smallnest/agent-wrapper/issues/47) + [#48](https://github.com/smallnest/agent-wrapper/issues/48) + [#49](https://github.com/smallnest/agent-wrapper/issues/49) + [#50](https://github.com/smallnest/agent-wrapper/issues/50) + [#51](https://github.com/smallnest/agent-wrapper/issues/51) Multi-agent expansion (agy, cursor, kimi-code) | ✅ |

## 与 ACP（Agent Client Protocol）对比

[ACP](https://agentclientprotocol.com/get-started/introduction) 是编辑器与 agent 之间的通信协议标准，定位类似 LSP——定义 JSON-RPC 消息格式让任何编辑器对接任何 agent。

agent-wrapper 不采用 ACP 协议，而是直接驱动 agent CLI 子进程。ACP 在编辑器场景下有意义，在工程集成场景下存在几个根本性问题：

1. **功能不对齐**。Agent CLI 提供完整功能（`--output-format`、`--max-turns`、`--approve-all`、`--session-id` 等），ACP server 是重新实现的一层，通常只覆盖子集，很多 CLI flag 和输出格式在 ACP 层被丢弃或降级。

2. **间接层伤害**。ACP 增加一重 JSON-RPC 包装——CLI 输出先被 ACP server 解析，再序列化为 ACP 事件，acpx 再消费。每多一层序列化/反序列化就慢一分，且中间层的 bug（事件丢失、字段静默丢弃、类型错误）对调用方不可见。

3. **不适合嵌入项目**。ACP server 通常绑定编辑器生命周期，不是为嵌入到其他 Go 程序中调用的库。agent-wrapper 是一个 Go package，`go get` 后直接 `orch.Run`，零额外进程、零协议开销。

agent-wrapper 的策略是 **"不协议化，直接包装"**——agent 的 CLI 就是 API。每个 agent 的子进程 stdout 直接打通到 `<-chan types.Event`，没有中间人。

如果仍想使用 ACP 协议，推荐 openclaw（Peter Steinberger）的 [acpx](https://acpx.sh/)——一个 ACP 命令行客户端，`npm install -g acpx` 即装即用，支持 Codex、Claude、Pi、Gemini 等 ACP bot。

## License

MIT
