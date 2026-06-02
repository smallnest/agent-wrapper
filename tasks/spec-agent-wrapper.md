# SPEC: Agent Wrapper

> Technical specification derived from: [tasks/prd-agent-wrapper.md](tasks/prd-agent-wrapper.md)
> Generated: 2026-06-02 | Target: greenfield | Go 1.22+

## 1. Summary

### 1.1 What This SPEC Covers

Agent Wrapper 的技术实现规范。定义：Go 接口/类型签名、Session 存储并发模型、四个 provider 的协议适配细节（wire format 映射表）、Orchestrator 状态机、CLI 命令结构、错误类型体系、测试策略。

### 1.2 PRD Reference

- Source: `tasks/prd-agent-wrapper.md`
- User Stories covered: US-001 到 US-011（全部）
- Functional Requirements covered: FR-1 到 FR-16（全部）

### 1.3 Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| 子进程启动策略 | 每次 `Run()` 启动新进程，`Close()` 或 context cancel 时终止 | session 可能存活数小时，期间 agent CLI 进程不应一直驻留 |
| 消息角色映射 | Wrapper 定义泛化 `Message`，各 provider 内部转换为原生格式 | 调用方面对统一模型；协议差异封装在 provider 内 |
| Session 存储并发模型 | `sync.RWMutex` 保护整个 store，Save 时深拷贝 Messages | 简单正确；一期无高并发需求 |
| UUID 生成 | `crypto/rand` + RFC 9562 UUID v4，无外部依赖 | 零依赖原则 |
| 流式协议解析 | 行分隔 JSON（JSON-RPC/NDJSON）+ SSE（`text/event-stream`），手写 scanner | 零依赖；两种模式覆盖全部四个 provider |
| `ApprovalHandler` 必填 vs 可选 | 可选。未设置时默认 allow 所有工具调用 | CLI `--approve-all` 场景和简单调用不需要审批 |

---

## 2. Architecture

### 2.1 System Context

```
┌──────────────────────────────────────────────────┐
│ 调用方 (Go code / CLI)                            │
│   import "github.com/smallnest/agent-wrapper"     │
└──────────┬───────────────────────────────────────┘
           │ Go function calls
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
│  │  │        agentProcess                   │   │ │
│  │  │  os/exec.Cmd + scanner/parser         │   │ │
│  │  └────────────┬─────────────────────────┘   │ │
│  └───────────────┼─────────────────────────────┘ │
└──────────────────┼──────────────────────────────┘
                   │ stdin/stdout (JSON-RPC / SSE)
     ┌─────────────▼──────────────┐
     │  Claude Code / Codex CLI   │
     │  Pi Agent / OpenCode CLI   │
     └────────────────────────────┘
```

### 2.2 Component Design

| Component | Responsibility | Package Path |
|-----------|---------------|-------------|
| `agent` | `Agent` interface + `Provider` enum + `RunInput` | `.` (root) |
| `session` | `Session` + `Message` types + `SessionStore` interface | `.` (root) |
| `session/memory` | `MemorySessionStore` | `./sessionstore/memory/` |
| `event` | `Event` type + `EventType` enum | `.` (root) |
| `process` | 子进程生命周期 + JSON-RPC/SSE scanner | `./process/` |
| `orchestrator` | Turn loop + hook dispatch | `.` (root) |
| `registry` | Provider 注册/查找 | `.` (root) |
| `claude` | Claude Code agent | `./claude/` |
| `codex` | Codex agent | `./codex/` |
| `pi` | Pi Agent | `./pi/` |
| `opencode` | OpenCode agent | `./opencode/` |
| `cmd/agent-wrapper` | CLI | `./cmd/agent-wrapper/` |

### 2.3 Module Interactions

```
Orchestrator.Run() 调用序列：

1. 从 SessionStore 加载 Session（或接收调用方传入的）
2. 将 NewMessage 追加到 Session.Messages
3. 构造 RunInput{Session: sessionWithNewMsg}
4. 调用 Agent.Run(ctx, input)
5. 事件循环：
   ┌─────────────────────────────────────────────────┐
   │ for evt := range eventCh {                       │
   │   switch evt.Type {                              │
   │   case TextDelta:    accumulate -> assistantText │
   │   case ToolCall:                                 │
   │     consult ApprovalHandler                      │
   │     if allow: 继续等待 ToolResult                 │
   │     if deny:   注入 synthetic tool_result 拒绝    │
   │     if abort:  跳出循环                           │
   │   case ToolResult: append to session, send back   │
   │   case TurnEnd:                                  │
   │     append all new msgs to session               │
   │     call BudgetHandler(tokenUsage)                │
   │     Save session to store                        │
   │     if maxTurns || stop: break                   │
   │   }                                              │
   │ }                                                │
   └─────────────────────────────────────────────────┘
6. 返回事件 channel（调用方同时消费）
```

### 2.4 File Structure

```
agent-wrapper/
├── agent.go                    # Agent interface + RunInput
├── provider.go                 # Provider enum
├── message.go                  # Message + Role types
├── session.go                  # Session type + SessionStore interface
├── session_memory.go           # MemorySessionStore (并发安全内存实现)
├── event.go                    # Event + EventType
├── orchestrator.go             # Orchestrator
├── registry.go                 # Registry
├── errors.go                   # 所有自定义 error 类型
├── approval.go                 # ApprovalHandler + Decision 类型
├── budget.go                   # BudgetHandler + TokenUsage 类型
├── process/
│   ├── process.go              # agentProcess: exec.Cmd wrapper
│   ├── scanner.go              # LineScanner interface
│   ├── jsonrpc.go              # JSON-RPC 2.0 frame scanner
│   └── sse.go                  # SSE frame scanner
├── claude/
│   ├── agent.go                # ClaudeCodeAgent
│   ├── agent_test.go
│   └── convert.go              # Message <-> Claude content blocks
├── codex/
│   ├── agent.go                # CodexAgent
│   ├── agent_test.go
│   └── convert.go              # Message <-> OpenAI chat format
├── pi/
│   ├── agent.go                # PiAgent
│   ├── agent_test.go
│   └── convert.go
├── opencode/
│   ├── agent.go                # OpenCodeAgent
│   ├── agent_test.go
│   └── convert.go
├── cmd/
│   └── agent-wrapper/
│       └── main.go
├── docs/                       # 中文文档
├── examples/                   # 可运行示例
├── go.mod
├── go.sum
├── README.md
└── tasks/
    ├── prd-agent-wrapper.md
    └── spec-agent-wrapper.md   # this file
```

---

## 3. Data Model

### 3.1 Core Types

```go
// agent.go

// Provider 标识底层 agent 实现。
type Provider string

const (
    ProviderClaudeCode Provider = "claude-code"
    ProviderCodex      Provider = "codex"
    ProviderPiAgent    Provider = "pi-agent"
    ProviderOpenCode   Provider = "opencode"
)

// Agent 是所有 coding agent 后端的统一接口。
// 每个实现通过子进程调用对应的 CLI。
type Agent interface {
    // Name 返回 agent 可读名称，如 "Claude Code 4.7"。
    Name() string

    // Provider 返回 provider 标识。
    Provider() Provider

    // Run 启动 agent 并返回事件流。
    // input.Session 包含完整历史；agent 负责将历史转换为原生格式发给 CLI。
    // 返回的 channel 在 agent 进程退出或 ctx 取消时关闭。
    // Run 可以并发调用，每个调用独立启动一个子进程。
    Run(ctx context.Context, input RunInput) (<-chan Event, error)

    // Close 释放 agent 持有的资源（如缓存的二进制路径检测结果）。
    Close() error
}

// RunInput 是一次 agent 调用的全部输入。
type RunInput struct {
    Session      *Session        // 必填：包含完整消息历史
    NewMessage   *Message        // 可选：本次新增的用户消息
    SystemPrompt string          // 可选：系统提示词
    WorkingDir   string          // 可选：工作目录
    MaxTurns     int             // 0 = 不限制（仅 Orchestrator 使用）
    AllowedTools []string        // 可选：限制可用的工具列表
    Extra        map[string]any  // provider 特有参数透传
}
```

```go
// message.go

// Role 表示消息的发送者角色。
type Role string

const (
    RoleUser       Role = "user"
    RoleAssistant  Role = "assistant"
    RoleToolUse    Role = "tool_use"
    RoleToolResult Role = "tool_result"
)

// Message 是会话中的单条消息。
//
// 角色语义：
//   user        - 用户发送的文本消息
//   assistant   - agent 产生的文本回复（由多次 TextDelta 累积）
//   tool_use    - agent 请求调用工具（包含工具名和参数）
//   tool_result - 工具执行结果（由 Orchestrator 在审批 allow 后追加）
type Message struct {
    Role    Role   `json:"role"`
    Content string `json:"content"`           // user / assistant / tool_result 的文本内容

    // 以下字段仅 tool_use 角色使用
    ToolCallID string          `json:"tool_call_id,omitempty"`
    ToolName   string          `json:"tool_name,omitempty"`
    ToolInput  json.RawMessage `json:"tool_input,omitempty"`  // tool_use: 工具参数 JSON

    // 以下字段仅 tool_result 角色使用
    ToolCallResultID string `json:"tool_call_result_id,omitempty"` // 关联的 tool_use.call_id
    IsError          bool   `json:"is_error,omitempty"`            // 工具执行是否出错
}
```

```go
// session.go

// Session 代表一个跨 turn 保持的会话上下文。
// 调用方可以安全地读取 Messages，但应通过 SessionStore 进行写入。
type Session struct {
    ID        string    `json:"id"`        // UUID v4
    Messages  []Message `json:"messages"`  // 按时间顺序排列的完整消息历史
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Metadata  map[string]string `json:"metadata,omitempty"` // 调用方自定义标签
}

// SessionStore 管理 Session 的持久化。
// 一期仅提供 MemorySessionStore 实现。
type SessionStore interface {
    Create() (*Session, error)              // 创建新 session，分配 UUID
    Get(id string) (*Session, error)        // 按 ID 查找；不存在返回 *SessionNotFoundError
    Save(session *Session) error            // 原子更新 session，自动更新 UpdatedAt
    Delete(id string) error                 // 移除 session
    List() []*SessionSummary                // 返回所有 session 摘要（不含完整消息）
}

type SessionSummary struct {
    ID           string    `json:"id"`
    MessageCount int       `json:"message_count"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

```go
// event.go

type EventType string

const (
    EventTextDelta  EventType = "text_delta"
    EventToolCall   EventType = "tool_call"
    EventToolResult EventType = "tool_result"
    EventTurnEnd    EventType = "turn_end"
    EventError      EventType = "error"
)

type Event struct {
    Type EventType `json:"type"`

    // TextDelta
    TextDelta string `json:"text_delta,omitempty"`

    // ToolCall
    ToolCallID string          `json:"tool_call_id,omitempty"`
    ToolName   string          `json:"tool_name,omitempty"`
    ToolInput  json.RawMessage `json:"tool_input,omitempty"`

    // ToolResult（当审批返回 allow 后工具执行完成时）
    ToolResultID     string `json:"tool_result_id,omitempty"`
    ToolResultOutput string `json:"tool_result_output,omitempty"`
    ToolResultError  bool   `json:"tool_result_error,omitempty"`

    // TurnEnd
    TurnNumber       int         `json:"turn_number,omitempty"`
    StopReason       string      `json:"stop_reason,omitempty"` // "end_turn" | "stop" | "max_turns" | "budget_exceeded"
    TokenUsage       *TokenUsage `json:"token_usage,omitempty"`

    // Error
    Error error `json:"error,omitempty"` // 序列化时使用 error.Error()
}
```

### 3.2 Message Role Mapping Table

各 provider 的原生格式不同。以下定义泛化 Message 到各原生格式的映射：

| Wrapper Role | Claude Code (content block type) | Codex (OpenAI role) | Pi Agent | OpenCode |
|---|---|---|---|---|
| `user` | `{type:"user", text:"..."}` | `{role:"user", content:"..."}` | TBD | TBD |
| `assistant` | `{type:"assistant", text:"..."}` | `{role:"assistant", content:"..."}` | TBD | TBD |
| `tool_use` | `{type:"tool_use", id, name, input}` | `tool_calls[]` on assistant msg | TBD | TBD |
| `tool_result` | `{type:"tool_result", tool_use_id, content}` | `{role:"tool", tool_call_id, content}` | TBD | TBD |

各 provider 的 `convert.go` 负责实现此映射。Pi 和 OpenCode 的具体映射在 US-005/US-006 实现时确定。

### 3.3 MemorySessionStore Concurrency

```go
// session_memory.go 内部结构（伪代码）

type MemorySessionStore struct {
    mu       sync.RWMutex
    sessions map[string]*storedSession // key: session ID
}

type storedSession struct {
    session Session
    mu      sync.RWMutex // per-session 锁，允许 Get + 并发 Save 不丢数据
}

// Save 实现：
// 1. store.mu.RLock  → 获取 storedSession
// 2. s.mu.Lock       → 锁定单 session
// 3. 深拷贝 Messages  → 新 slice，避免调用方修改影响内部状态
// 4. 更新 UpdatedAt
// 5. s.mu.Unlock
// 6. store.mu.RUnlock
```

`Get` 返回 session 的浅拷贝（Messages 是新 slice 但 Message struct 是值类型，天然隔离）。

### 3.4 Session Workflow

```
// 新建 session
session, _ := store.Create()

// 第一次 Run：session 有 0 条消息
session.Messages = append(session.Messages, Message{Role: "user", Content: "重构 main.go"})
events, _ := agent.Run(ctx, RunInput{Session: session})
// agent 内部：
//   1. 发现 session.Messages 有 1 条 user msg
//   2. 将消息列表转为原生格式发给 CLI
//   3. 返回的事件流中包含 TextDelta, ToolCall, TurnEnd 等
// 消费事件后：
//   session.Messages 新追加：assistant msg + tool_use msg + tool_result msg + assistant msg
//   = 总共 5 条消息

store.Save(session)

// 第二次 Run：同一个 session
session, _ = store.Get(session.ID) // 加载，包含上述 5 条消息
session.Messages = append(session.Messages, Message{Role: "user", Content: "加上重试逻辑"})
events2, _ := agent.Run(ctx, RunInput{Session: session})
// agent 看到的是 5+1=6 条消息的完整历史
```

---

## 4. API Design

### 4.1 Go SDK API

#### Agent Interface

```go
// 创建 agent
agent := claude.New(claude.Options{
    BinaryPath: "",        // 空 = 自动检测 PATH
    Model:      "sonnet",  // 透传给 claude --model
    Extra: map[string]any{
        "thinking_budget": 16000,   // Claude 特有：extended thinking tokens
    },
})

// 直接使用 Agent（无编排）
events, err := agent.Run(ctx, RunInput{
    Session:      session,
    NewMessage:   &Message{Role: "user", Content: "重构 main.go"},
    SystemPrompt: "You are a Go expert.",
    WorkingDir:   "/path/to/project",
    MaxTurns:     10,
    AllowedTools: []string{"read", "write", "bash"},
})
```

#### Orchestrator

```go
store := memory.NewStore() // MemorySessionStore
agent := claude.New(claude.Options{})

orch := agentwrapper.NewOrchestrator(agent, store)

// 设置审批回调（可选）
orch.SetApprovalHandler(func(ctx context.Context, call ToolCall) (Decision, error) {
    fmt.Printf("Allow tool: %s(%s)? [y/n/a] ", call.Name, call.Input)
    var answer string
    fmt.Scanln(&answer)
    switch answer {
    case "y": return Decision{Action: ActionAllow}, nil
    case "n": return Decision{Action: ActionDeny, Reason: "user denied"}, nil
    default:  return Decision{Action: ActionAbort}, nil
    }
})

// 设置预算回调（可选）
orch.SetBudgetHandler(func(ctx context.Context, usage TokenUsage) error {
    totalTokens += usage.TotalTokens
    if totalTokens > budgetLimit {
        return &BudgetExceededError{Used: totalTokens, Limit: budgetLimit}
    }
    return nil
})

events, err := orch.Run(ctx, RunInput{
    Session:      session,
    NewMessage:   &Message{Role: "user", Content: "重构 main.go"},
    SystemPrompt: "You are a Go expert.",
    WorkingDir:   "/path/to/project",
    MaxTurns:     10,
})

// 消费事件
for evt := range events {
    switch evt.Type {
    case EventTextDelta:
        fmt.Print(evt.TextDelta)
    case EventToolCall:
        fmt.Printf("\n[TOOL] %s(%s)\n", evt.ToolName, evt.ToolInput)
    case EventToolResult:
        status := "OK"
        if evt.ToolResultError { status = "ERROR" }
        fmt.Printf("[RESULT %s] %s\n", status, truncate(evt.ToolResultOutput, 200))
    case EventTurnEnd:
        fmt.Printf("\n--- Turn %d end (%s) ---\n", evt.TurnNumber, evt.StopReason)
        fmt.Printf("Tokens: %d in / %d out\n", evt.TokenUsage.InputTokens, evt.TokenUsage.OutputTokens)
    case EventError:
        fmt.Printf("\n[ERROR] %v\n", evt.Error)
    }
}
```

#### Registry

```go
// 使用内置 provider
reg := agentwrapper.NewRegistry()
agent, err := reg.Get("claude-code") // 返回 ClaudeCodeAgent

// 注册自定义 provider
reg.Register("my-agent", func(opts map[string]any) (Agent, error) {
    return &MyCustomAgent{}, nil
})

// 遍历所有已注册 provider
for _, name := range reg.List() {
    a, _ := reg.Get(name)
    fmt.Printf("%s: %s\n", name, a.Name())
}
```

### 4.2 CLI API

```
agent-wrapper [global-flags] <command> [args]

Commands:
  run       启动 agent turn
  list      列出已注册的 provider
  sessions  列出当前所有 session
  version   显示版本信息

Global flags:
  --store-dir PATH   Session 存储目录（默认：$HOME/.agent-wrapper/sessions）
                     （一期仅内存模式，此 flag 预留二期文件存储）

Run flags:
  --provider NAME            Provider 名称：claude-code|codex|pi-agent|opencode（必填）
  --model NAME               模型名称（透传给 provider）
  --max-turns N              最大 turn 数（默认：10）
  --working-dir PATH         工作目录
  --system-prompt-file PATH  从文件读取系统提示词
  --approve-all              自动允许所有工具调用（跳过审批）
  --budget-tokens N          Token 预算上限
  --session-id UUID          恢复已有 session
  --binary-path PATH         覆盖 agent CLI 二进制路径

Run arguments:
  MESSAGE                    用户消息（必需，位置参数）
```

### 4.3 Error Types

```go
// errors.go

// ExitError 表示 agent CLI 进程异常退出。
type ExitError struct {
    ExitCode int
    Stderr   string
    Command  string
}
func (e *ExitError) Error() string

// ProtocolError 表示无法解析 agent CLI 的输出。
type ProtocolError struct {
    Provider    Provider
    RawBytes    []byte
    Description string
}
func (e *ProtocolError) Error() string

// BudgetExceededError 表示 token 预算耗尽。
type BudgetExceededError struct {
    Used  int
    Limit int
}
func (e *BudgetExceededError) Error() string

// SessionNotFoundError 表示 session ID 不存在。
type SessionNotFoundError struct {
    ID string
}
func (e *SessionNotFoundError) Error() string

// TimeoutError 表示 agent 运行超时。
type TimeoutError struct {
    Duration time.Duration
}
func (e *TimeoutError) Error() string
```

---

## 5. Business Logic

### 5.1 Orchestrator State Machine

```
                    ┌─────────────┐
                    │    START    │
                    └──────┬──────┘
                           │
                           ▼
                  ┌────────────────┐
                  │   SENDING      │
                  │ 追加 NewMessage │
                  │ 构造 RunInput  │
                  └───────┬────────┘
                          │ Agent.Run()
                          ▼
                 ┌──────────────────┐
                 │  RECEIVING       │◄──────────────────┐
                 │ 消费 Event 流    │                   │
                 └──┬───────┬───────┴───┬───────────┘  │
                    │       │           │               │
         TextDelta  │  ToolCall    TurnEnd         (continue)
                    │       │           │               │
                    ▼       ▼           ▼               │
               accumulate  │      ┌──────────┐         │
               to output   │      │ WRITEBACK │         │
                           │      │ 追加 msgs │         │
                           │      │ 调 Budget │         │
                           │      │ 保存 sess │         │
                           │      └────┬─────┘         │
                           │           │               │
                           ▼      ┌────▼────┐          │
                    ┌───────────┐ │ STOP?   │          │
                    │ CONSULT   │ │ maxTurns│          │
                    │ Approval  │ │ or stop │          │
                    │ Handler   │ └────┬────┘          │
                    └──┬───┬────┘      │               │
                       │   │       yes │               │
              allow/deny│   │ abort     ▼               │
                       │   │      ┌────────┐  budget    │
                       ▼   │      │  END   │  exceeded  │
                  继续等待  │      └────────┘            │
                  ToolResult│                            │
                            └────────────────────────────┘
```

### 5.2 Tool Approval Flow

```
1. Orchestrator 收到 EventToolCall
2. 如果 ApprovalHandler == nil → 默认 allow
3. 调用 ApprovalHandler(ctx, ToolCall{ID, Name, Input})
4. 等待返回：
   allow → 继续等待对应 call_id 的 ToolResult
   deny  → 构造 synthetic ToolResult{content: "DENIED: <reason>", isError: true}
           → 作为 Message{Role: "tool_result"} 追加到 session
           → 发送回 agent（继续 turn 循环）
   abort → 发送 EventTurnEnd{StopReason: "aborted"}
         → 跳出事件循环，writeback session，关闭 agent 进程
```

### 5.3 Session Writeback Rules

在一个 turn 结束后，以下消息被追加到 session：

| 事件 | 操作 |
|------|------|
| `TextDelta`（累积） | 如果累积的文本非空 → 追加 `Message{Role: "assistant", Content: accumulated}` |
| `ToolCall` | 追加 `Message{Role: "tool_use", ToolCallID, ToolName, ToolInput}` |
| `ToolResult` | 追加 `Message{Role: "tool_result", ToolCallResultID = ToolCallID, Content: result}` |

**写入时机：** 每个 `TurnEnd` 事件时，一次性追加该 turn 中累积的所有新消息。

### 5.4 Context Cancellation

```
Orchestrator.Run() 接收 ctx
     │
     ├─ ctx.Done() 触发 → cancel subprocess context
     │                   → agent.Run 内部的 agentProcess 收到 cancel
     │                   → SIGTERM → 等待 5s → SIGKILL
     │                   → event channel 关闭（返回最后一条 EventError{context.Canceled}）
     │
     └─ 调用方也可以直接 cancel ctx 来中止运行
```

---

## 6. Agent Protocol Adapters

### 6.1 Claude Code (JSON-RPC 2.0)

**启动命令：**
```
claude agent --model <model> --max-turns <N> --allowedTools <list>
```

**stdin 协议（每行一个 JSON-RPC request）：**
```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"systemPrompt":"...","messages":[...]}}
{"jsonrpc":"2.0","id":2,"method":"run","params":{}}
```

**stdout 协议（每行一个 JSON-RPC response/notification）：**
```json
{"jsonrpc":"2.0","id":1,"result":{"status":"initialized"}}
{"jsonrpc":"2.0","method":"notify/text_delta","params":{"text":"Hello"}}
{"jsonrpc":"2.0","method":"notify/tool_use","params":{"id":"call_1","name":"read","input":{...}}}
{"jsonrpc":"2.0","method":"notify/turn_end","params":{"stopReason":"end_turn","usage":{...}}}
```

**Message 转换（convert.go）：**
```go
// []Message → Anthropic content blocks
func messagesToContentBlocks(msgs []Message) []map[string]any {
    var blocks []map[string]any
    for _, msg := range msgs {
        switch msg.Role {
        case RoleUser:
            blocks = append(blocks, map[string]any{"type": "user", "text": msg.Content})
        case RoleAssistant:
            blocks = append(blocks, map[string]any{"type": "assistant", "text": msg.Content})
        case RoleToolUse:
            blocks = append(blocks, map[string]any{
                "type":  "tool_use",
                "id":    msg.ToolCallID,
                "name":  msg.ToolName,
                "input": msg.ToolInput,
            })
        case RoleToolResult:
            blocks = append(blocks, map[string]any{
                "type":        "tool_result",
                "tool_use_id": msg.ToolCallResultID,
                "content":     msg.Content,
            })
        }
    }
    return blocks
}
```

### 6.2 Codex (OpenAI-compatible Chat Completions)

**启动命令：**
```
codex chat --model <model>
```

**stdin 协议：** OpenAI Chat Completions 请求（单次 JSON 后关闭 stdin）
```json
{
  "model": "gpt-5",
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "重构 main.go"}
  ],
  "stream": true,
  "tools": [...]
}
```

**stdout 协议：** SSE（`data: {json}\n\n`）
```
data: {"id":"chatcmpl-xxx","choices":[{"delta":{"role":"assistant"},"index":0}]}

data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":"Hello"},"index":0}]}

data: {"id":"chatcmpl-xxx","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"path\":\"main.go\"}"}}]},"index":0}]}

data: [DONE]
```

**Message 转换：**
```go
func messagesToOpenAI(msgs []Message) []map[string]any {
    var result []map[string]any
    for _, msg := range msgs {
        switch msg.Role {
        case RoleUser:
            result = append(result, map[string]any{"role": "user", "content": msg.Content})
        case RoleAssistant:
            m := map[string]any{"role": "assistant", "content": msg.Content}
            if msg.IsToolCall {
                m["tool_calls"] = []map[string]any{{
                    "id":   msg.ToolCallID,
                    "type": "function",
                    "function": map[string]any{
                        "name":      msg.ToolName,
                        "arguments": string(msg.ToolInput),
                    },
                }}
            }
            result = append(result, m)
        case RoleToolResult:
            result = append(result, map[string]any{
                "role":         "tool",
                "tool_call_id": msg.ToolCallResultID,
                "content":      msg.Content,
            })
        }
    }
    return result
}
```

### 6.3 Pi Agent & OpenCode

Pi Agent 和 OpenCode 的具体协议格式在实现 US-005/US-006 时调研确定。适配模式与上述一致：
- 子目录下 `agent.go` 实现 `Agent` 接口
- `convert.go` 处理 `[]Message` → 原生格式的转换
- 复用 `process/` 包中的 scanner（优先 JSON-RPC 或 SSE 格式；若为自定义格式则新增 scanner）

### 6.4 Scanner Interface

```go
// process/scanner.go

// Frame 是扫描器产生的一个协议帧。
type Frame struct {
    Data   []byte          // 原始帧数据（JSON）
    Raw    []byte          // 原始行文本（用于调试）
}

// FrameScanner 从 io.Reader 中逐帧扫描。
type FrameScanner interface {
    Scan() bool       // 前进到下一帧
    Frame() Frame     // 获取当前帧
    Err() error       // 扫描过程中遇到的错误
}

// NewJSONRPCScanner 创建 JSON-RPC 2.0 行分隔扫描器。
// 每行一个 JSON 对象。
func NewJSONRPCScanner(r io.Reader) FrameScanner { ... }

// NewSSEScanner 创建 SSE (text/event-stream) 扫描器。
// 解析 "data: <json>\n\n" 帧。
func NewSSEScanner(r io.Reader) FrameScanner { ... }
```

---

## 7. Error Handling

### 7.1 Error Taxonomy

| Error Type | Trigger | User-Visible Message |
|------------|---------|---------------------|
| `*ExitError` | CLI 进程非零退出 | `agent 'claude-code' exited with code 1: <stderr>` |
| `*ProtocolError` | stdout 解析失败 | `failed to parse agent output: <description>` |
| `*BudgetExceededError` | 预算耗尽 | `token budget exceeded: used 12000 of 10000` |
| `*SessionNotFoundError` | `Get(nonexistent)` | `session 'xxx' not found` |
| `*TimeoutError` | context 超时 | `agent run timed out after 30s` |

### 7.2 Retry Strategy

- Agent 协议解析错误 (`*ProtocolError`)：不自动重试。上报给调用方决定。
- CLI 进程异常退出 (`*ExitError`)：不自动重试。调用方可重新调用 `Run()`。
- 任何重试逻辑由调用方实现，wrapper 只做忠实上报。

### 7.3 Graceful Degradation

| Dependency Failure | Behavior |
|-------------------|----------|
| CLI binary not found | `Run()` 返回错误："claude not found in PATH. Install with: npm install -g @anthropic-ai/claude-code" |
| CLI binary launches then crashes | 读取 stderr，返回 `*ExitError{Stderr: stderrOutput}` |
| Agent hangs (no output) | ctx timeout → SIGTERM → SIGKILL → `*TimeoutError` |
| Session store full (OOM) | Go 标准库 OOM 行为；二期引入 LRU eviction |

---

## 8. Performance

### 8.1 Expected Load

- 一期：单机单进程，1-10 个并发 session
- 每个 agent 子进程占用 200MB-1GB（取决于模型和上下文大小）
- 无外部依赖，延迟仅受 CLI 进程启动 + LLM 推理延迟影响

### 8.2 Optimization Strategy

- 子进程延迟启动：`Run()` 时才起进程，非构造时
- stdout scanner 使用 `bufio.Scanner`，缓冲区 64KB
- `MemorySessionStore` 使用读写锁而非互斥锁，读多写少场景友好
- JSON 解析使用 `json.NewDecoder` 流式解码，不预先读全部输出

### 8.3 No Database

一期零外部依赖。无数据库、无缓存中间件、无消息队列。所有数据在内存中。

---

## 9. Testing Strategy

### 9.1 Unit Tests (per package)

| Package | What to Test | Mock Strategy |
|---------|-------------|---------------|
| `session_memory` | CRUD + 并发安全 | 直接用 MemorySessionStore |
| `process` | JSON-RPC scanner, SSE scanner | `strings.NewReader` 模拟 stdout |
| `claude` | Message → content blocks 转换, 事件解析 | mock 子进程输出 |
| `codex` | Message → OpenAI messages 转换, 事件解析 | mock 子进程输出 |
| `pi` | 同上 | mock 子进程输出 |
| `opencode` | 同上 | mock 子进程输出 |
| `orchestrator` | turn 循环, 审批流程, budget, session writeback | mock Agent（返回预定义 event channel） |
| `registry` | Register/Get/List/覆盖/未找到 | 直接用 Registry |

### 9.2 Integration Tests

- `agent_test.go` (root): 用 mock 子进程验证 end-to-end 事件流
- 需要在 CI 中安装真实 CLI 的测试用 build tag `//go:build integration` 隔离

### 9.3 Concurrency Tests

```go
func TestMemorySessionStore_ConcurrentSave(t *testing.T) {
    store := memory.NewStore()
    s, _ := store.Create()

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            s, _ := store.Get(s.ID)
            s.Messages = append(s.Messages, Message{Role: "user", Content: fmt.Sprintf("msg-%d", i)})
            store.Save(s)
        }(i)
    }
    wg.Wait()

    // 验证：10 条消息都应追加，无丢失
    s, _ = store.Get(s.ID)
    // 计数可能 < 10 如果竞争覆盖，或 = 10 如果正确追加
    // SPEC 要求：Save 在 per-session 锁下原子操作，确保不丢消息
}
```

### 9.4 Acceptance Criteria Mapping

| PRD US | SPEC Sections | Test Type | Key Assertion |
|--------|--------------|-----------|---------------|
| US-001 | 3.1, 4.1 | unit | Agent 接口所有方法签名可编译 |
| US-002 | 3.3, 9.3 | unit + race | 10 goroutine 并发 Save 不丢消息 |
| US-003 | 6.4, 6.1 | unit | JSON-RPC/SSE scanner 正确解析 |
| US-004 | 6.1 | unit | Message → Claude content blocks 映射正确 |
| US-005 | 6.2 | unit | Message → OpenAI messages 映射正确 |
| US-006/US-007 | 6.3 | TBD | 协议调研后确定 |
| US-008 | 5.1, 5.2, 5.3 | unit | 3-turn mock: 消息正确累积到 session |
| US-008 | 5.3 | unit | 同一 session 两次 Run: 第二次包含第一次全部消息 |
| US-009 | 4.1 | unit | Registry 注册/获取/覆盖 |
| US-010 | 4.2 | integration | `agent-wrapper run` CLI 端到端 |
| US-011 | docs/ | manual | 文档覆盖所有公开 API |

---

## 10. Implementation Plan

### 10.1 Phase Order

```
Phase 1: 核心抽象（无外部依赖）
├── message.go + session.go + session_memory.go
├── event.go + errors.go + provider.go + agent.go
├── approval.go + budget.go
└── registry.go

Phase 2: 子进程基础设施
├── process/process.go (exec.Cmd wrapper)
├── process/scanner.go (FrameScanner interface)
├── process/jsonrpc.go
└── process/sse.go

Phase 3: Provider 实现（可并行，按协议调研完成度排序）
├── claude/ (优先：协议已知，JSON-RPC)
├── codex/ (次优：协议已知，SSE)
├── pi/    (需要协议调研)
└── opencode/ (需要协议调研)

Phase 4: 编排层
└── orchestrator.go

Phase 5: CLI + 文档 + 示例
├── cmd/agent-wrapper/main.go
├── docs/*.md
└── examples/*/main.go
```

### 10.2 Dependency Graph

```
Phase 1  ──►  Phase 2  ──►  Phase 3 (parallel)  ──►  Phase 4  ──►  Phase 5
                                                          │
   claude ─┐                                               │
   codex  ─┤──  depends on phase 2 (process) ─────────────┘
   pi     ─┤
   opencode┘
```

### 10.3 Issue Mapping

| Issue | PRD Stories | SPEC Sections | Phase | Depends On |
|-------|------------|--------------|-------|------------|
| #1 Core Types | US-001, US-002 | 3.1, 3.2, 3.3, 3.4 | 1 | — |
| #2 Memory Session Store | US-002 | 3.3, 9.3 | 1 | #1 |
| #3 Event & Error types | US-001 | 3.1 (event), 4.3 | 1 | — |
| #4 Approval & Budget types | US-001 | 4.1 | 1 | — |
| #5 Registry | US-009 | 4.1 | 1 | #1 |
| #6 Process Manager | US-003 | 6.4 | 2 | — |
| #7 Claude Agent | US-004 | 6.1 | 3 | #6 |
| #8 Codex Agent | US-005 | 6.2 | 3 | #6 |
| #9 Pi Agent | US-006 | 6.3 | 3 | #6 |
| #10 OpenCode Agent | US-007 | 6.3 | 3 | #6 |
| #11 Orchestrator | US-008 | 5.1, 5.2, 5.3, 5.4 | 4 | #1-#10 |
| #12 CLI | US-010 | 4.2 | 5 | #11 |
| #13 Docs & Examples | US-011 | — | 5 | #11 |

---

## 11. Open Questions & Risks

### 11.1 Unresolved Questions

- **[Pi Agent]** CLI 的 stdin/stdout 协议格式是什么？是否有 JSON 模式？
- **[OpenCode]** CLI 的 stdin/stdout 协议格式是什么？是否有 JSON 模式？
- **[Windows]** `os/exec` 在 Windows 下的 SIGTERM 不支持，优雅关闭用 `taskkill`？是否需要一期支持？
- **[Go module path]** 确认 `github.com/smallnest/agent-wrapper` 吗？

### 11.2 Technical Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Pi/OpenCode CLI 无机器可读输出模式 | 无法解析事件，provider 不可用 | 调研阶段先读源码；若无则用 PTY + 正则解析（降级方案），PRD 中记录限制 |
| Claude Code JSON-RPC API 不稳定变更 | ClaudeCodeAgent 协议解析失败 | 版本检测 + 协议版本选择逻辑；或要求固定 CLI 版本 |
| 子进程 stdout 阻塞（缓冲区满） | agent 卡死 | `Run()` 内部持续消费 stdout，使用 goroutine + channel 解耦 |
| Message 映射语义损失 | 某些 provider 原生能力无法通过泛化 Message 表达 | `Extra` 字段透传；或特定 provider 支持 `ExtendedMessage` 接口（二期） |

### 11.3 Assumptions

- 每个 agent CLI 可通过 `os/exec` 子进程调用，未 sandbox 化
- Claude Code 的 `claude agent` 子命令存在且稳定
- Codex CLI 的 `codex chat` 子命令输出标准 OpenAI Chat Completions SSE
- 一期不涉及 Windows 兼容性
- 一期不涉及多进程/分布式/集群
- 调用方负责安装对应的 agent CLI；Agent Wrapper 不做自动安装
