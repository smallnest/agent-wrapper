# Agent Wrapper — Issues

> Derived from: [PRD](prd-agent-wrapper.md) | [SPEC](spec-agent-wrapper.md)
> Generated: 2026-06-02

---

## #1: Core Types — Message, Session, Event, Errors, Provider, Approval, Budget
**Type:** AFK | **Blocked by:** None

### What to build

定义 agent-wrapper 的全部纯类型，构成后续所有 issue 的类型基础。

- `Provider` 枚举：`claude-code`、`codex`、`pi-agent`、`opencode`
- `Message` 结构体 + `Role` 枚举（`user` / `assistant` / `tool_use` / `tool_result`）
- `Session` 结构体（ID UUID v4、Messages 切片、CreatedAt、UpdatedAt、Metadata map），调用方可读写 Messages
- `SessionStore` 接口：`Create()`、`Get(id)`、`Save(session)`、`Delete(id)`、`List()`
- `SessionSummary` 结构体（不含完整消息体）
- `Event` 结构体 + `EventType` 枚举（`text_delta` / `tool_call` / `tool_result` / `turn_end` / `error`），各事件字段按 SPEC 定义
- `Agent` 接口：`Name()`、`Provider()`、`Run(ctx, input) (<-chan Event, error)`、`Close()`
- `RunInput` 结构体：`Session`、`NewMessage`、`SystemPrompt`、`WorkingDir`、`MaxTurns`、`AllowedTools`、`Extra`
- `ApprovalHandler` 函数类型：`func(ctx context.Context, call ToolCall) (Decision, error)`，含 `ToolCall` 和 `Decision` 结构体
- `BudgetHandler` 函数类型，`TokenUsage` 结构体
- 所有自定义 Error 类型：`ExitError`、`ProtocolError`、`BudgetExceededError`、`SessionNotFoundError`、`TimeoutError`

包路径：文件放在 repo 根目录（`package agentwrapper`）。

### Acceptance criteria

- [ ] 所有类型 `go build ./...` 通过
- [ ] `go vet ./...` 零警告
- [ ] `Message` JSON 序列化/反序列化正确（tag 定义准确）
- [ ] `Provider` 枚举的常量值不与标准库冲突
- [ ] `SessionStore` 接口方法签名可在无实现的情况下编译

### Blocked by

None — can start immediately.

---

## #2: MemorySessionStore
**Type:** AFK | **Blocked by:** #1

### What to build

实现 `SessionStore` 接口的内存版本，保证并发安全。

- `MemorySessionStore` 结构体，内部 `sync.RWMutex` 保护 `map[string]*storedSession`
- `storedSession` 持有 `Session` + per-session `sync.RWMutex`，Save 时在单 session 锁下深拷贝 Messages 并更新 UpdatedAt
- `Create()` 用 `crypto/rand` 生成 RFC 9562 UUID v4
- `Get()` 返回 session 副本（Messages 是新 slice）
- `Save()` 原子更新 session，追加的 messages 不丢失
- `Delete()` 移除 session
- `List()` 返回 `[]SessionSummary`
- `Get()` 不存在的 ID 返回 `*SessionNotFoundError`

并发安全测试：10 个 goroutine 同时 Save 同一 session，验证消息不丢失。

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] `go test -race ./...` 零数据竞争
- [ ] Create → Get → append msg → Save → Get：消息完整保留
- [ ] 并发测试：10 goroutine × 100 append，最终消息数正确
- [ ] `List()` 返回正确的 session ID 和消息计数
- [ ] `Delete()` 后 `Get()` 返回 `*SessionNotFoundError`

### Blocked by

- #1 Core Types

---

## #3: Registry
**Type:** AFK | **Blocked by:** #1

### What to build

Provider 注册表，按名称查找 Agent 实现。

- `Registry` 结构体，`Register(name string, factory Factory)` + `Get(name string) (Agent, error)` + `List() []string` + `Unregister(name string)`
- `Factory` 函数类型：`func(opts map[string]any) (Agent, error)`
- 内置 4 个预留条目（`claude-code`、`codex`、`pi-agent`、`opencode`），初始工厂函数返回错误 "not yet implemented"（由后续 issue 注入真正实现）
- `Get()` 不存在的 name 返回明确错误
- `Register()` 重复 name 返回错误（overwrite=false）或覆盖（overwrite=true）

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] Register → Get：返回的 Agent 正确
- [ ] Get 未注册 name：返回错误
- [ ] Register 重复 name overwrite=false：返回错误
- [ ] List 返回所有已注册 name
- [ ] Unregister → Get：返回错误
- [ ] `go test -race ./...` 通过

### Blocked by

- #1 Core Types

---

## #4: Process Manager — 子进程 + Scanner
**Type:** AFK | **Blocked by:** None（仅依赖标准库）

### What to build

子进程生命周期管理和协议帧扫描器，零外部依赖。

**agentProcess（`process/process.go`）：**
- 封装 `os/exec.Cmd`，支持设置 workdir、env vars
- `Start()` 启动进程并返回 stdin writer + stdout reader
- 基于 context 的优雅关闭：ctx cancel → 发送 SIGTERM → 等待 5s → SIGKILL
- `Close()` 等待进程退出并返回状态
- 并发安全：write stdin / read stdout / close 可在不同 goroutine 调用

**FrameScanner（`process/scanner.go`）：**
- `FrameScanner` 接口：`Scan() bool`、`Frame() Frame`、`Err() error`
- `Frame` 结构体：`Data []byte`（JSON body）+ `Raw []byte`（原始行）

**JSONRPCScanner（`process/jsonrpc.go`）：**
- 行分隔 JSON 扫描器，每行一个完整 JSON 对象
- 空行跳过，非 JSON 行报 `*ProtocolError`

**SSEScanner（`process/sse.go`）：**
- 解析 `text/event-stream` 格式
- `data:` 前缀的行累积为帧，空行触发 frame 完成
- 支持 `data:` 后的前导空格
- 非 data 行（event/id/retry）忽略，`[DONE]` 信号返回空帧

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] JSONRPCScanner：3 行有效 JSON → 3 个 frame
- [ ] JSONRPCScanner：混合空白行和 JSON 行 → 正确跳过空白
- [ ] JSONRPCScanner：非法 JSON 行 → `*ProtocolError`
- [ ] SSEScanner：`data: {"a":1}\n\n` → 1 个 `{"a":1}` frame
- [ ] SSEScanner：`data: [DONE]\n\n` → 空 Data frame
- [ ] SSEScanner：多行 `data:` 累积为一个 frame
- [ ] agentProcess：`echo hello` 子进程 → stdout 读到 "hello"，正常退出
- [ ] agentProcess：ctx cancel → 进程被终止 → `context.Canceled`
- [ ] agentProcess：子进程写满 stderr → stderr 被正确捕获不阻塞 stdout

### Blocked by

None — can start immediately.

---

## #5: ClaudeCodeAgent
**Type:** AFK | **Blocked by:** #1, #4

### What to build

第一个完整的 Agent 实现，通过 `claude agent` 子进程 + JSON-RPC 2.0 驱动 Claude Code。

**`claude/agent.go`：**
- `ClaudeCodeAgent` 实现 `Agent` 接口
- `Options` 结构体：`BinaryPath`（空=自动检测 PATH+常见路径）、`Model`、`Extra`
- `Run()` 启动 `claude agent --model ... --max-turns ...`，stdin 写入 JSON-RPC `initialize` + `run`
- 从 stdout 读取 JSON-RPC 响应/通知，转换为 `Event` channel 输出
- 自动检测：`notify/text_delta` → `EventTextDelta`；`notify/tool_use` → `EventToolCall`；`notify/turn_end` → `EventTurnEnd`
- Context 取消时优雅关闭子进程

**`claude/convert.go`：**
- `messagesToContentBlocks([]Message) []map[string]any`：按 SPEC 3.2 映射表转换
- `contentBlockToMessage(block map[string]any) Message`：反向转换

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] Mock 子进程输出：JSON-RPC `notify/text_delta` → 生成 `Event{Type: TextDelta, TextDelta: "hi"}`
- [ ] Mock 子进程输出：JSON-RPC `notify/tool_use` → 生成 `Event{Type: ToolCall, ToolName: "read", ToolInput: ...}`
- [ ] Mock 子进程输出：JSON-RPC `notify/turn_end` → 生成 `Event{Type: TurnEnd, StopReason: "end_turn"}`
- [ ] `[]Message` 包含 user + assistant + tool_use + tool_result → content blocks 顺序和结构正确
- [ ] Binary auto-detect：优先 `PATH`，其次 `~/.local/bin`，其次 `~/.npm-global/bin`
- [ ] Binary not found → `Run()` 返回错误，消息包含安装指令
- [ ] `go test -race ./claude/...` 通过

### Blocked by

- #1 Core Types
- #4 Process Manager

---

## #6: CodexAgent
**Type:** AFK | **Blocked by:** #1, #4

### What to build

通过 Codex CLI (`codex chat`) + SSE 驱动 OpenAI Codex。

**`codex/agent.go`：**
- `CodexAgent` 实现 `Agent` 接口
- `Options`：同 Claude 模式
- `Run()` 启动 `codex chat --model ...`，stdin 写入 OpenAI Chat Completions 请求
- stdout 用 `SSEScanner` 解析，转换为 `Event` channel

**`codex/convert.go`：**
- `messagesToOpenAI([]Message) []map[string]any`：按 SPEC 3.2 映射表转换
- `sseDeltaToEvent(delta) Event`：SSE delta 到统一 Event 的映射

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] Mock SSE `choices[0].delta.content` → `Event{Type: TextDelta, TextDelta: "hi"}`
- [ ] Mock SSE `choices[0].delta.tool_calls[]` → `Event{Type: ToolCall, ...}`
- [ ] Mock SSE `[DONE]` + `usage` → `Event{Type: TurnEnd, TokenUsage: ...}`
- [ ] `[]Message` 包含 user + assistant + tool → OpenAI messages 格式正确
- [ ] Binary auto-detect 同 Claude 模式
- [ ] `go test -race ./codex/...` 通过

### Blocked by

- #1 Core Types
- #4 Process Manager

---

## #7: PiAgent
**Type:** HITL — 需先调研 Pi Agent CLI 协议 | **Blocked by:** #1, #4

### What to build

通过 `npx @earendil-works/pi` 或本地安装驱动 Pi Agent。

**先决条件（本 issue 的第一步）：**
调研 Pi Agent CLI 的 stdin/stdout 协议格式：
1. 是否有 JSON 模式或机器可读输出？
2. 消息历史如何传入？（参数？stdin？文件？）
3. 流式输出格式？（JSON-RPC？SSE？纯文本？）
4. 工具调用和结果的表达方式？

**调研结论确认后实现：**
- `PiAgent` 实现 `Agent` 接口
- `pi/convert.go`：`[]Message` ↔ Pi 原生格式
- 如果无机器可读模式，降级方案：PTY + 正则解析（在 SPEC 中标注限制）
- Mock 子进程输出测试协议解析

### Acceptance criteria

- [ ] 协议调研结论文档化（写在 `pi/README.md` 或 issue comment）
- [ ] `go build ./...` 通过
- [ ] `[]Message` → Pi 原生格式转换正确
- [ ] Pi 输出 → `Event` 流映射正确（mock 测试）
- [ ] Binary 检测：优先 `PATH`，其次 `npx @earendil-works/pi`
- [ ] `go test -race ./pi/...` 通过

### Blocked by

- #1 Core Types
- #4 Process Manager

---

## #8: OpenCodeAgent
**Type:** HITL — 需先调研 OpenCode CLI 协议 | **Blocked by:** #1, #4

### What to build

通过 `opencode` CLI 驱动 OpenCode。

**先决条件（本 issue 的第一步）：**
调研 OpenCode CLI 的 stdin/stdout 协议格式：
1. 是否有 JSON 模式或机器可读输出？
2. 消息历史如何传入？
3. 流式输出格式？
4. 工具调用和结果的表达方式？

**调研结论确认后实现：**
- `OpenCodeAgent` 实现 `Agent` 接口
- `opencode/convert.go`：`[]Message` ↔ OpenCode 原生格式
- Mock 子进程输出测试协议解析

### Acceptance criteria

- [ ] 协议调研结论文档化
- [ ] `go build ./...` 通过
- [ ] `[]Message` → OpenCode 原生格式转换正确
- [ ] OpenCode 输出 → `Event` 流映射正确（mock 测试）
- [ ] Binary auto-detect
- [ ] `go test -race ./opencode/...` 通过

### Blocked by

- #1 Core Types
- #4 Process Manager

---

## #9: Orchestrator — Turn 循环编排器
**Type:** AFK | **Blocked by:** #1, #2, #3

### What to build

实现 `Orchestrator`，包装任意 `Agent` 并驱动多 turn 对话，含审批、预算和 session 写入。

**核心逻辑（按 SPEC 5.1 状态机实现）：**
1. 从 `SessionStore` 加载或接收 Session
2. 将 NewMessage 追加到 Session.Messages
3. 构造 `RunInput` 调用 `Agent.Run()`
4. 事件循环：
   - `TextDelta`: 累积为 assistant 文本
   - `ToolCall`: 调用 `ApprovalHandler`（nil 则默认 allow）；allow → 等待 ToolResult；deny → 注入合成拒绝 ToolResult；abort → 跳出循环
   - `TurnEnd`: 追加所有本 turn 新消息到 Session，调用 `BudgetHandler`，`Save()` session；判断 stop/max_turns
   - `Error`: 停止循环，返回错误
5. 返回事件 channel（调用方同时消费）
6. Context 取消时清理

**审批回调（可选）：** 未设置时默认 allow 所有工具调用
**预算回调（可选）：** 每 TurnEnd 调用，返回 error 时终止循环

### Acceptance criteria

- [ ] `go build ./...` 通过
- [ ] 3-turn mock：消息按 turn 正确累积到 Session
- [ ] 同一 session 两次 `Run()`：第二次的 `Agent.Run()` 收到包含第一次全部消息的 `RunInput`
- [ ] 审批 allow：等待 ToolResult 并回传
- [ ] 审批 deny：注入合成拒绝，agent 继续
- [ ] 审批 abort：立即终止并返回停止事件
- [ ] 预算耗尽：`BudgetHandler` 返回 `BudgetExceededError` → 停止循环
- [ ] MaxTurns 达到上限：停止循环
- [ ] Context 取消：agent 进程被终止
- [ ] `go test -race ./...` 通过

### Blocked by

- #1 Core Types
- #2 MemorySessionStore
- #3 Registry

---

## #10: CLI 工具
**Type:** AFK | **Blocked by:** #5, #9（至少一个真实 agent + Orchestrator）

### What to build

`cmd/agent-wrapper/main.go` — CLI 入口。

**子命令：**
- `run` — 启动 agent turn。参数：`--provider`（必填）、`--model`、`--max-turns`、`--working-dir`、`--system-prompt-file`、`--approve-all`、`--budget-tokens`、`--session-id`、`--binary-path`。位置参数：消息文本
- `list` — 列出所有已注册 provider（名称 + 类型）
- `sessions` — 列出当前所有 session（ID + 消息数 + 时间）
- `version` — 版本号 + Go 版本

**`run` 命令行为：**
- 流式打印 TextDelta 到 stdout
- ToolCall 时：如 `--approve-all` 自动允许；否则终端交互提示 allow/deny/abort
- ToolResult 打印摘要（截断 200 字符）
- TurnEnd 打印 turn 编号 + token 用量
- Session 自动保存在内存 store 中
- 结束后打印 session ID 供后续 `--session-id` 恢复

### Acceptance criteria

- [ ] `agent-wrapper run --provider claude-code "echo hello"` 端到端成功
- [ ] `--provider` 支持全部 4 个名称
- [ ] `--session-id` 恢复已有 session 继续对话
- [ ] `--approve-all` 跳过审批
- [ ] `--budget-tokens 100` 超预算后终止
- [ ] `agent-wrapper list` 输出所有 provider
- [ ] `agent-wrapper sessions` 输出 session 列表
- [ ] `agent-wrapper version` 输出版本信息
- [ ] `go build ./cmd/agent-wrapper/...` 成功
- [ ] `go test -race ./...` 通过

### Blocked by

- #5 ClaudeCodeAgent（至少一个可用的真实 agent）
- #9 Orchestrator

---

## #11: Docs + Examples
**Type:** AFK | **Blocked by:** #10

### What to build

完整的中文文档和可运行示例。

**文档（`docs/`）：**
- `quickstart.md` — 5 分钟快速开始（安装 → 调用 Claude Code → 收事件）
- `architecture.md` — 架构设计与接口说明（ASCII 架构图 + 组件关系）
- `session.md` — Session 机制详解（创建、消息累积、跨 Run 上下文保持、并发安全）
- `providers.md` — 各 provider 特性对比与配置说明
- `approval.md` — 审批流程详解（Orchestrator + ApprovalHandler 交互）
- `custom-provider.md` — 如何编写自定义 provider

**示例（`examples/`）：**
- `basic/main.go` — 最简：创建 session → 发消息 → `agent.Run()` → 打印事件
- `multi-turn/main.go` — 同一 session 3 次 Run，展示上下文累积
- `approval/main.go` — 设置 ApprovalHandler，交互式审批
- `budget/main.go` — 设置 BudgetHandler，超预算自动终止
- `custom-provider/main.go` — 注册并使用自定义 Agent

**README：**
- 项目介绍、快速开始、架构图（ASCII art）、示例索引、文档链接、LICENSE

### Acceptance criteria

- [ ] 所有 5 个示例 `go run ./examples/<name>/main.go` 可运行且不报错（至少 basic 和 multi-turn 需真实验证）
- [ ] 6 篇文档覆盖所有公开 API
- [ ] README 包含 ASCII 架构图
- [ ] 所有 Go doc 注释可通过 `go doc -all` 完整阅读

### Blocked by

- #10 CLI

---

## Issue Dependency Graph

```
#1 Core Types ──┬── #2 MemorySessionStore ──────┐
                ├── #3 Registry ────────────────┤
                │                               │
#4 Process ────┬── #5 ClaudeCodeAgent ──────┐   │
               ├── #6 CodexAgent            │   │
               ├── #7 PiAgent (HITL)        │   │
               └── #8 OpenCodeAgent (HITL)  │   │
                                            │   │
                                     #9 Orchestrator ◄──┘
                                            │
                                     #10 CLI ◄── #5 + #9
                                            │
                                     #11 Docs ◄── #10
```
