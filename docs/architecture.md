# 架构设计

## 整体架构

```
┌──────────────────────────────────────────────────┐
│ 调用方 (Go code / CLI)                            │
│   import "github.com/smallnest/agent-wrapper"     │
└──────────┬───────────────────────────────────────┘
           │ Go 函数调用
┌──────────▼───────────────────────────────────────┐
│              Agent Wrapper                        │
│                                                   │
│  ┌──────────┐  ┌───────────────┐  ┌───────────┐ │
│  │ Registry │  │ Orchestrator  │  │  Session  │ │
│  │          │  │  ┌─────────┐  │  │  Store    │ │
│  │ claude───┼──┼──► turn    │  │  │  (memory) │ │
│  │ codex    │  │  │  loop   │  │  │           │ │
│  │ pi       │  │  │  +hooks │  │  │           │ │
│  │ opencode │  │  └─────────┘  │  │           │ │
│  └──────────┘  └───────┬───────┘  └───────────┘ │
│                        │ Agent.Run()              │
│  ┌─────────────────────▼────────────────────────┐ │
│  │         Agent Implementations                │ │
│  │  ClaudeCodeAgent  CodexAgent                 │ │
│  │  PiAgent          OpenCodeAgent              │ │
│  │                                              │ │
│  │  ┌──────────────────────────────────────┐   │ │
│  │  │        Process Manager                │   │ │
│  │  │  os/exec.Cmd + scanner/parser         │   │ │
│  │  └────────────┬─────────────────────────┘   │ │
│  └───────────────┼─────────────────────────────┘ │
└──────────────────┼──────────────────────────────┘
                   │ stdin/stdout
     ┌─────────────▼──────────────┐
     │  Claude Code / Codex CLI   │
     │  Pi Agent / OpenCode CLI   │
     └────────────────────────────┘
```

## 核心接口

### Agent

所有 coding agent 后端的统一接口。每个实现通过子进程调用对应的 CLI。

```go
type Agent interface {
    Name() string
    Provider() types.Provider
    Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error)
    Close() error
}
```

- `Name()` — 返回可读名称，如 "Claude Code"
- `Provider()` — 返回 provider 标识
- `Run()` — 启动 agent 并返回事件流，channel 在进程退出或 ctx 取消时关闭
- `Close()` — 释放资源

### Orchestrator

多 turn 对话编排器。包装 `Agent` 并驱动事件循环，处理审批、预算和 session 回写。

```go
orch := agentwrapper.NewOrchestrator(agent, store,
    agentwrapper.WithApprovalHandler(myHandler),
    agentwrapper.WithBudgetHandler(myBudget),
)
events, _ := orch.Run(ctx, input)
```

### Registry

Provider 注册表。管理 `Factory` 函数，按名称创建 `Agent` 实例。

```go
registry := agentwrapper.NewRegistry()
claude.RegisterIn(registry)
agent, _ := registry.Get("claude-code", nil)
```

### SessionStore

Session 持久化接口。内置 `MemorySessionStore` 实现。

```go
type SessionStore interface {
    Create() (*types.Session, error)
    Get(id string) (*types.Session, error)
    Save(session *types.Session) error
    Delete(id string) error
    List() []*types.SessionSummary
}
```

## 事件模型

`Agent.Run()` 返回 `<-chan Event`，事件类型：

| 类型 | 含义 | 关键字段 |
|------|------|---------|
| `EventTextDelta` | 流式文本增量 | `TextDelta` |
| `EventToolCall` | agent 请求调用工具 | `ToolCallID`, `ToolName`, `ToolInput` |
| `EventToolResult` | 工具执行完成 | `ToolResultID`, `ToolResultOutput`, `ToolResultError` |
| `EventTurnEnd` | 一个 turn 结束 | `TurnNumber`, `StopReason`, `TokenUsage` |
| `EventError` | 运行中发生错误 | `Error` |

## 进程管理

每个 provider 通过 `process.AgentProcess` 管理子进程生命周期：

- 启动：`process.StartProcess(ctx, cfg)` 创建 `os/exec.Cmd` 子进程
- 终止：context cancel → SIGTERM → 等待 5s → SIGKILL
- 协议解析：`JSONRPCScanner`（Claude Code, Pi）或 `SSEScanner`（Codex）

## 包结构

```
agent-wrapper/
├── agent.go           # Agent 接口
├── orchestrator.go    # Orchestrator 编排器
├── registry.go        # Registry 注册表
├── session.go         # SessionStore 接口
├── approval.go        # 审批类型
├── budget.go          # 预算类型
├── types/             # 核心类型 (Message, Event, Session 等)
├── process/           # 子进程管理 + 协议解析器
├── sessionstore/      # Session 存储实现
│   └── memory/        # 内存存储
├── claude/            # Claude Code agent
├── codex/             # Codex agent
├── pi/                # Pi agent
├── opencode/          # OpenCode agent
├── cmd/               # CLI 入口
│   └── agent-wrapper/
├── docs/              # 中文文档
└── examples/          # 可运行示例
```
