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
- **Orchestrator** — 多 turn 对话编排，含审批、预算控制、上下文压缩重试
- **Registry** — Provider 注册表，按名称创建 Agent 实例
- **Event** — 统一事件流（TextDelta / ToolCall / ToolResult / TurnEnd / Error）
- **RunSync** — 同步 API，收集所有事件返回聚合 `RunResult`

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

## 与 ACP（Agent Client Protocol）对比

[ACP](https://agentclientprotocol.com/get-started/introduction) 是编辑器与 agent 之间的通信协议标准，定位类似 LSP——定义 JSON-RPC 消息格式让任何编辑器对接任何 agent。

agent-wrapper 与 ACP 是互补关系，非竞争关系：

| 维度 | ACP | agent-wrapper |
|------|-----|---------------|
| **定位** | 协议标准——定义 agent 和 editor 之间怎么通信 | 运行时库——封装 agent CLI 进程并提供持久化、审批、重试 |
| **解决的问题** | "我写的 agent 怎么对接多个编辑器" | "我已经有多个 agent CLI，怎么统一调用、编排、容错" |
| **抽象层** | 传输层（JSON-RPC over stdio/HTTP） | 进程控制层（子进程生命周期 + event 流 + turn 循环） |
| **Agent 实现方** | agent 方必须实现 ACP 协议 | agent 只需有 CLI，wrap 即可——无需修改 agent 自身 |
| **Session 管理** | 不定义 session 持久化 | 透传 agent runtime session + `RunResult.SessionID` 可存储恢复 |
| **错误恢复** | 无内置重试/压缩 | 内置上下文超限检测 → 滑动窗口 → 摘要压缩 → 重试，最多 N 次 |
| **审批/预算** | 不涉及 | Orchestrator 内置审批 handler + token 预算控制 |
| **多 provider** | 需每个 agent 实现协议 | Registry 注册即可，4 个内置 provider + 自定义 |

**关键区别：ACP 是一份协议，agent-wrapper 是一份实现。** ACP 说"你应该用这个 JSON 格式对话"，agent-wrapper 说"给我任意 agent CLI，我来跑、来容错、来治理"。两者可以组合使用——agent-wrapper 作为 ACP agent 的宿主运行时。

### 相关项目

[acpx](https://acpx.sh/) 是 openclaw（Peter Steinberger）开发的 ACP 命令行客户端——通过 `npm install -g acpx` 安装后，可以在命令行直接与支持 ACP 协议的 agent 交互。acpx 是 ACP 的**消费者**，agent-wrapper 是 agent CLI 的**包装器**。两者解决不同层次的问题：acpx 让你"用 ACP 协议调 agent"，agent-wrapper 让你"把任意 agent CLI 封成可治理的运行时"。

## License

MIT
