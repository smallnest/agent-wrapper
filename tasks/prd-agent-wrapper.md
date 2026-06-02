# PRD: Agent Wrapper — Go 多 Agent 统一运行时

## 1. 概述

**Agent Wrapper** 是一个 Go 语言实现的 agent 统一适配与编排层。

市面上有多个优秀的 coding agent 实现——Claude Code、Codex CLI、Pi Agent、OpenCode 等——但它们各自有不同的调用方式、协议、认证机制和生命周期。当业务系统需要灵活切换或同时使用多个 agent 时，必须逐个适配。

Agent Wrapper 解决这个问题：**一个统一的 `Agent` 接口，一份 Go 代码即可驱动任意后端 agent，附带 session 管理、turn 循环编排、工具审批和预算控制。**

它既是一个可 import 的 Go library，也是一个独立的 CLI 工具。

### 1.1 为什么不用 Agent Client Protocol (ACP) / MCP？

业界已有 MCP（Model Context Protocol）和 Google A2A（Agent-to-Agent）等 agent 间通信协议，以及正在孵化中的 ACP（Agent Client Protocol）。一个自然的问题是：为什么不基于它们来做，而要自己包装？

**1. ACP/MCP 解决的是不同问题**

MCP 的核心抽象是 "Tool/Resource/Prompt 的暴露与发现"——一个 MCP Server 告诉 Client "我能执行这些工具"。ACP 则关注 agent 与 IDE/编辑器的集成。它们的目标是**工具和资源的标准化暴露**，而非**agent 生命周期的统一驱动**。

我们要做的事不同：启动一个 Claude Code 进程，喂入消息，从 stdout 解析流式事件（文本增量、工具调用、turn 结束），控制审批决策的回传，管理子进程的生命周期。这是进程编排和协议适配，不是工具注册。

**2. 现有的 coding agent CLI 不实现 ACP/MCP**

今天你 `claude`、`codex`、`pi`、`opencode` 这些 CLI，没有一个把 ACP/MCP 作为自己的 stdin/stdout 协议。它们各有各的格式：

- Claude Code：JSON-RPC 2.0 over stdio（`claude agent` 模式）
- Codex CLI：OpenAI 兼容的 Chat Completions 流
- Pi Agent：自定义包协议
- OpenCode：自定义 CLI 输出

在它们之上套一层 ACP 适配器，等于先理解 A 协议，翻译成 B 协议（ACP），再让下游从 B 协议翻译回来——无谓增加一层抽象，多了延迟、复杂度和协议阻抗失配。Agent Wrapper 的策略是**直接适配各 CLI 的原生协议**，不做中间翻译。

**3. Agent 不是 Tool**

MCP 的模型里，模型是 client，工具是 server。但我们要包装的 agent 自己就能规划、调用工具、多轮纠错——它是一个完整的 agent 进程。强行把它挤进 "tool" 的语义，`Run()` 变成一次 MCP `tools/call`，就丢掉了流式事件、审批中断、token 预算这些 agent 特有的控制面。这些东西在 MCP 里根本没有对应的语义。

**4. 我们更接近 iii 的 worker 原语，而非协议标准**

ACP/MCP 的思路是：**定义一种所有 agent 都该说的语言**。这很理想，但历史上每个协议标准都面临同样的困境——要么覆盖面不够（很多 agent 特有的能力表达不了），要么过于臃肿（规格无限膨胀）。

Agent Wrapper 的思路更接近 iii 文章中的理念：**不做"通用语言"，只做"统一接口"**。一个 Go `Agent` interface，`Run()` 返回 `<-chan Event`。协议差异被封装在每个实现里，调用方完全看不见。这个接口足够薄，薄到可以适配四个完全不同实现；也足够厚，厚到能承载流式输出、工具审批、预算控制这些 agent 运行时的全部需求。

**然而 session 是一个例外。** ACP 定义 session 的方式值得借鉴——一个 session 是一个带有唯一 ID 的、跨 turn 保持的消息上下文容器，由 client 端维护消息历史。这正是 Agent Wrapper 需要的：wrapper 自己管理 session 中的消息累积，每次 `Run()` 携带完整历史，而不是依赖底层 CLI 的内部会话机制。详见下文 Session 设计。

**总结：** 不排斥未来某个 agent CLI 原生支持 ACP 时，加一个 `ACPAgent` 实现。但今天的四个目标 provider 都不说 ACP，而我们应该适配现实，不是适配标准文档。同时，session 机制从 ACP 中借鉴，在 wrapper 层实现。

## 2. 目标

- 提供统一 `Agent` 接口，隐藏 Claude Code / Codex / Pi Agent / OpenCode 的协议差异
- **实现 Session 管理**：每个 session 维护完整的消息历史（user/assistant/tool 消息），同一 session 内多次 `Run()` 共享上下文
- 内置 turn 循环编排（发消息 → 流式接收 → 工具调用 → 继续），业务方无需自己写循环
- 提供可插拔的审批回调机制（`SetApprovalHandler`），工具调用在执行前可暂停等待人工决策
- 提供可插拔的预算追踪回调，每次模型调用后上报 token 用量，超出上限自动终止
- 留出扩展点，允许用户注册自定义 provider
- 以 Go library 形式发布（`go get`），同时提供 CLI 工具（`agent-wrapper run`）用于快速试用
- 完整的测试套件、中文文档和使用示例

## 3. 用户故事

### US-001: 定义 Agent 接口与核心类型
**描述:** 作为开发者，我需要一个统一的 `Agent` 接口、`Session` 类型和事件模型，以便所有后端可以互换使用，同一 session 内上下文保持一致。

**验收标准:**
- [ ] 定义 `Agent` 接口，包含 `Name()`、`Provider()`、`Run()`、`Close()`
- [ ] 定义 `Session` 结构体，包含 `ID`、`Messages`（`[]Message`，类型覆盖 user/assistant/tool/tool_result）、`CreatedAt`、`UpdatedAt`、`Metadata`
- [ ] 定义 `Message` 类型，表达 user / assistant / tool_use / tool_result 四种角色
- [ ] 定义 `RunInput` 结构体，包含 `Session`（必填）、`SystemPrompt`、`WorkingDir`、`MaxTurns`、`AllowedTools`、`Extra`
- [ ] 定义 `Event` 结构体，支持 TextDelta、ToolCall、ToolResult、TurnEnd、Error 五种事件类型
- [ ] 定义 `Provider` 枚举：ClaudeCode、Codex、PiAgent、OpenCode
- [ ] 定义 `ApprovalHandler` 函数类型：`func(ctx context.Context, call ToolCall) (Decision, error)`
- [ ] 定义 `BudgetHandler` 函数类型：`func(ctx context.Context, usage TokenUsage) error`
- [ ] 定义 `SessionStore` 接口：`Create()`、`Get(id)`、`Save(session)`、`Delete(id)`、`List()`
- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告

### US-002: 实现内存 Session 存储
**描述:** 作为开发者，我需要一个内存实现的 SessionStore，管理 session 的创建、查找、更新和删除，因为 session 是 wrapper 的一等公民，同一 session 内的消息必须在多次 `Run()` 之间累积。

**验收标准:**
- [ ] 实现 `MemorySessionStore`，满足 `SessionStore` 接口
- [ ] `Create()` 生成唯一 session ID（UUID v4），初始化空的 `Messages` 列表，记录 `CreatedAt`
- [ ] `Get(id)` 返回 session 副本，并发安全
- [ ] `Save(session)` 原子更新 session，自动更新 `UpdatedAt`，追加的 messages 不会丢失
- [ ] `Delete(id)` 移除 session
- [ ] `List()` 返回所有 session 摘要（ID、消息数、创建/更新时间）
- [ ] 支持并发读写：两个 goroutine 同时对同一 session 调用 `Run()` 时不会丢消息
- [ ] 单元测试：创建/获取/保存追加消息/删除/并发安全
- [ ] `go build ./...` 通过

### US-003: 实现子进程管理器
**描述:** 作为开发者，我需要一个通用的子进程管理模块，负责 agent CLI 进程的启动、通信和回收，因为所有四个 provider 都通过子进程调用。

**验收标准:**
- [ ] 实现 `agentProcess` 封装 `os/exec.Cmd`，支持 stdin 写入 / stdout-stderr 分离读取
- [ ] 实现基于 context 的超时和优雅关闭（先 SIGTERM，超时后 SIGKILL）
- [ ] 实现 JSON-RPC 流解析器 — 从 stdout 管道中逐帧解析 JSON 消息
- [ ] 实现 SSE 流解析器 — 处理 `text/event-stream` 格式输出
- [ ] 支持设置环境变量、工作目录
- [ ] 并发安全：Write/Close/Read 可被不同 goroutine 调用
- [ ] 单元测试覆盖正常退出、超时 kill、解析错误场景
- [ ] `go build ./...` 通过

### US-004: 实现 ClaudeCode Agent
**描述:** 作为开发者，我需要通过 `ClaudeCodeAgent` 调用 Claude Code CLI，使用 JSON-RPC 协议交互，以便使用 Claude 进行编码任务。

**验收标准:**
- [ ] 实现 `ClaudeCodeAgent`，满足 `Agent` 接口
- [ ] `Run()` 接收 `RunInput`，将其 `Session.Messages` 转换为 Claude Code 的 JSON-RPC 请求格式
- [ ] 自动检测 `claude` 二进制路径（优先 `PATH`，其次 `~/.local/bin/claude` 等常见路径）
- [ ] 解析 JSON-RPC 响应流，映射为统一的 `Event` 类型
- [ ] 支持 Claude Code 的 `--model`、`--max-turns`、`--allowedTools` 参数透传
- [ ] 支持 Claude Code 的 thinking/extended thinking 模式
- [ ] 支持 tool_use 事件 → ToolCall 事件的转换
- [ ] 单元测试覆盖 JSON-RPC 协议解析（mock 子进程输出）
- [ ] `go build ./...` 通过

### US-005: 实现 Codex Agent
**描述:** 作为开发者，我需要通过 `CodexAgent` 调用 Codex CLI，以便使用 OpenAI Codex 进行编码任务。

**验收标准:**
- [ ] 实现 `CodexAgent`，满足 `Agent` 接口
- [ ] `Run()` 将 `Session.Messages` 转换为 Codex CLI 的协议格式
- [ ] 自动检测 `codex` 二进制路径
- [ ] 解析 Codex 的响应流，映射为统一的 `Event` 类型
- [ ] 支持 Codex 特有的参数透传（sandbox 模式等）
- [ ] 单元测试覆盖协议解析
- [ ] `go build ./...` 通过

### US-006: 实现 Pi Agent
**描述:** 作为开发者，我需要通过 `PiAgent` 调用 Pi Agent（`@earendil-works/pi`），以便使用 Pi 进行编码任务。

**验收标准:**
- [ ] 实现 `PiAgent`，满足 `Agent` 接口
- [ ] `Run()` 将 `Session.Messages` 转换为 Pi Agent 的协议格式
- [ ] 通过 `npx` 或本地安装调用 `@earendil-works/pi`
- [ ] 解析 Pi Agent 的响应流，映射为统一的 `Event` 类型
- [ ] 支持 Pi 特有的参数透传
- [ ] 单元测试覆盖协议解析
- [ ] `go build ./...` 通过

### US-007: 实现 OpenCode Agent
**描述:** 作为开发者，我需要通过 `OpenCodeAgent` 调用 OpenCode CLI，以便使用 OpenCode 进行编码任务。

**验收标准:**
- [ ] 实现 `OpenCodeAgent`，满足 `Agent` 接口
- [ ] `Run()` 将 `Session.Messages` 转换为 OpenCode 的协议格式
- [ ] 自动检测 `opencode` 二进制路径
- [ ] 解析 OpenCode 的响应流，映射为统一的 `Event` 类型
- [ ] 支持 OpenCode 特有的参数透传
- [ ] 单元测试覆盖协议解析
- [ ] `go build ./...` 通过

### US-008: 实现 Turn 循环编排器与 Session 上下文累积
**描述:** 作为开发者，我需要编排器在每次 `Run()` 时：(1) 从 session 中读取完整消息历史发给 agent，(2) 将 agent 产生的所有消息（assistant 文本、tool_use、tool_result）自动写回 session，(3) 驱动多 turn 对话直到完成——这样调用方在同一 session 上多次 `Run()` 自然保持上下文一致。

**验收标准:**
- [ ] 实现 `Orchestrator`，包装任意 `Agent` 并驱动多 turn 对话
- [ ] `Orchestrator.Run(ctx, input)` 内部流程：
  1. 从 `input.Session` 读取现有 `Messages` 作为对话历史
  2. 将 `input` 中的新 user 消息追加到 session（由调用方在传入前完成，或编排器内部通过 `NewMessage` 参数处理）
  3. 调用底层 `Agent.Run()`，传入完整消息列表
  4. 接收 `Event` 流：`TextDelta` → 累积为 assistant 文本消息；`ToolCall` → 追加为 tool_use 消息；`ToolResult`（来自审批回调） → 追加为 tool_result 消息
  5. Turn 结束时，将所有新消息持久化到 `SessionStore.Save(session)`
  6. 判断继续/停止/达到 MaxTurns
- [ ] 自动检测 `Event.ToolCall`，调用 `ApprovalHandler` 获取决策
- [ ] `allow` → 将决策传递回 agent 继续
- [ ] `deny` → 生成 tool_result（内容为拒绝说明），追加到 session 并传回 agent
- [ ] `aborted` → 终止当前 turn
- [ ] 自动检测 `Event.TurnEnd`，判断是否继续或达到 `MaxTurns`
- [ ] 每次 turn 结束后调用 `BudgetHandler` 上报 token 用量
- [ ] 超出预算上限（`BudgetHandler` 返回 error）时停止循环并返回预算耗尽事件
- [ ] 通过 channel 将事件透传给调用方
- [ ] 验证：同一 session ID 调用两次 `Orchestrator.Run()`，第二次的 agent 请求中包含第一次对话的全部历史
- [ ] 单元测试：模拟 3 turn 对话、模拟审批 deny/allow/abort、模拟预算耗尽、模拟跨 Run 的上下文保持
- [ ] `go build ./...` 通过

### US-009: 实现 Provider 注册表与扩展点
**描述:** 作为开发者，我需要一个 provider 注册表，可以按名称查找已有 provider，也可以注册自定义 provider 实现。

**验收标准:**
- [ ] 实现 `Registry`，支持 `Register(name string, factory Factory)` 和 `Get(name string) (Agent, error)`
- [ ] 内置四个 provider 自动注册（`claude-code`、`codex`、`pi-agent`、`opencode`）
- [ ] 用户可通过 `registry.Register("my-agent", myFactory)` 注册自定义实现
- [ ] 自定义 provider 只需满足 `Agent` 接口，无需修改任何内部代码
- [ ] 单元测试：注册/获取/覆盖/未找到
- [ ] `go build ./...` 通过

### US-010: 实现 CLI 工具
**描述:** 作为开发者，我需要一个 CLI 工具快速试用 agent-wrapper，无需写 Go 代码即可驱动不同 agent。

**验收标准:**
- [ ] `agent-wrapper run --provider claude-code "帮我重构 main.go"` 可用
- [ ] `--provider` 支持所有四个内置 provider
- [ ] `--max-turns` 控制最大轮次
- [ ] `--working-dir` 指定工作目录
- [ ] `--model` 透传模型选择
- [ ] `--system-prompt-file` 从文件读取系统提示词
- [ ] `--approve-all` 自动批准所有工具调用（跳过审批）
- [ ] `--budget-tokens` 设置 token 预算上限
- [ ] `--session-id` 恢复已有会话，继续对话
- [ ] 输出流式打印到终端
- [ ] `agent-wrapper list` 列出已注册的 provider
- [ ] `agent-wrapper sessions` 列出当前所有 session
- [ ] `agent-wrapper version` 显示版本信息
- [ ] 集成测试：端到端执行一次简单任务 + 恢复 session 继续对话
- [ ] `go build ./...` 通过

### US-011: 编写文档与示例
**描述:** 作为开发者，我需要中文文档和可运行的示例代码，以便快速上手集成 agent-wrapper。

**验收标准:**
- [ ] `README.md`：项目介绍、快速开始、架构图（ASCII art）
- [ ] `docs/` 目录包含以下中文文档：
  - [ ] `docs/quickstart.md` — 5 分钟快速开始
  - [ ] `docs/architecture.md` — 架构设计与接口说明
  - [ ] `docs/session.md` — Session 机制详解：创建、消息累积、跨 Run 上下文保持
  - [ ] `docs/providers.md` — 各 provider 的特性对比与配置说明
  - [ ] `docs/approval.md` — 审批流程详解
  - [ ] `docs/custom-provider.md` — 如何编写自定义 provider
- [ ] `examples/` 目录包含可运行示例：
  - [ ] `examples/basic/` — 最简调用（创建 session → 发消息 → 收事件）
  - [ ] `examples/multi-turn/` — 同一 session 多次 Run，展示上下文累积
  - [ ] `examples/approval/` — 带审批的交互式 agent
  - [ ] `examples/budget/` — 带预算限制
  - [ ] `examples/custom-provider/` — 自定义 provider
- [ ] 每个示例包含 `main.go` 和注释说明

## 4. 功能需求

- **FR-1:** 系统必须提供 `Agent` 接口，定义统一的 `Run(ctx, input) (<-chan Event, error)`、`Close()` 等方法签名
- **FR-2:** 系统必须实现 `ClaudeCodeAgent`、`CodexAgent`、`PiAgent`、`OpenCodeAgent` 四种内置实现
- **FR-3:** 系统必须通过子进程方式调用 agent CLI，使用 `os/exec` 包并正确处理 stdin/stdout/stderr
- **FR-4:** 系统必须将各 agent 的协议输出（JSON-RPC、SSE、自定义流）统一映射为 `Event` 流
- **FR-5:** 系统必须提供 `Session` 类型和 `SessionStore` 接口，管理会话的创建与消息持久化
- **FR-6:** 系统必须实现 `MemorySessionStore`（内存实现），支持 create/get/save/delete/list 操作
- **FR-7:** 系统必须提供 `Orchestrator`，自动驱动多 turn 对话，并在每个 turn 结束后将新消息写回 Session
- **FR-8:** 同一 session 的多次 `Run()` 调用必须共享完整的消息上下文——第二次 `Run()` 时 agent 能看到第一次的全部对话历史
- **FR-9:** 系统必须提供 `ApprovalHandler` 回调接口，允许调用方在工具执行前决策 allow/deny/abort
- **FR-10:** 系统必须提供 `BudgetHandler` 回调接口，在每 turn 结束后上报 token 用量，返回 error 时终止运行
- **FR-11:** 系统必须提供 `Registry`，支持按名称注册和查找 Agent 实现
- **FR-12:** 系统必须提供 CLI 工具，支持 `agent-wrapper run`、`list`、`sessions`、`version` 子命令
- **FR-13:** 系统必须支持通过 context 取消正在运行的 agent 调用
- **FR-14:** 系统必须为每个 provider 实现提供 `Extra map[string]any` 字段，允许透传 provider 特有的参数
- **FR-15:** `Session` 中的消息历史必须包含完整的角色信息（user / assistant / tool_use / tool_result），以便正确还原为各 agent 的原生消息格式
- **FR-16:** 所有公开 API 必须有 Go doc 注释，关键结构体有使用示例

## 5. 非目标

- **不做**：工具的实际执行——Agent Wrapper 只负责工具调用的审批和路由，业务方自己执行工具并通过回调返回结果
- **不做**：多 agent 协作/编排——一期只做单 agent 的 turn 循环，不涉及 agent 之间的通信
- **不做**：持久化到磁盘/数据库的 SessionStore——一期只做内存实现。Session 在进程重启后丢失。二期可扩展 FileStore / SQLiteStore
- **不做**：OpenTelemetry 追踪——二期考虑
- **不做**：HTTP/API 服务——二期考虑
- **不做**：策略引擎（OPA/Cedar 集成）——审批逻辑由回调处理，不做声明式策略
- **不做**：WebSocket 协议——不做像 iii 那样的 worker 总线，保持 Go SDK 方式调用
- **不做**：基于 provider CLI 内部的 session 透传（如 `--resume`）——wrapper 层自己管理 session，不依赖底层 CLI 的会话机制

## 6. 设计考量

### 6.1 Session 是核心抽象

```
┌──────────────────────────────────────────────────┐
│ Session                                           │
│  ID: "uuid-1234"                                  │
│  Messages: [                                      │
│    {role: user,      content: "重构 main.go"}     │
│    {role: assistant, content: "我来分析..."}       │
│    {role: tool_use,  name: "read",  input: {...}} │
│    {role: tool_result, call_id: "...", result:..} │
│    {role: assistant, content: "重构完成"}          │
│  ]                                                │
│  CreatedAt / UpdatedAt                            │
└──────────────────────────────────────────────────┘
```

Session 不透明——调用方能读写 `Messages`。这允许在调用 `Run()` 前插入/修改消息（如注入系统指令、编辑历史等）。

### 6.2 接口设计原则

```
// 最简：创建 session → 发消息 → 收事件
store := wrapper.NewMemorySessionStore()
session, _ := store.Create()
session.Messages = append(session.Messages, wrapper.Message{
    Role: "user", Content: "重构 main.go",
})

agent := wrapper.NewClaudeCodeAgent(opts)
events, _ := agent.Run(ctx, RunInput{
    Session:     session,
    WorkingDir:  "/path/to/project",
    MaxTurns:    10,
    SystemPrompt: "...",
})
for evt := range events {
    // 处理事件
    // 每次 TurnEnd 后，session.Messages 已自动追加了新消息
}
store.Save(session) // 持久化到 store

// 继续对话：同一个 session，自动携带全部历史
session, _ = store.Get(session.ID)
session.Messages = append(session.Messages, wrapper.Message{
    Role: "user", Content: "给 handleError 加上重试逻辑",
})
events2, _ := agent.Run(ctx, RunInput{Session: session, ...})
// agent 看到的是：重构 main.go 的完整历史 + 新消息
```

```
需要编排的调用方（带审批和预算）：

orch := wrapper.NewOrchestrator(agent, store)
orch.SetApprovalHandler(myApproval)
orch.SetBudgetHandler(myBudget)
events, _ := orch.Run(ctx, RunInput{
    Session:    session,
    NewMessage: wrapper.Message{Role: "user", Content: "重构 main.go"},
    MaxTurns:   10,
})
// Orchestrator 自动：
//   1. 将 NewMessage 追加到 session
//   2. 发送完整历史给 agent
//   3. 每个 TurnEnd 后 writeback 到 store + 调用 BudgetHandler
//   4. ToolCall 时暂停等待审批
//   5. 审批结果写入 session
```

裸 `Agent` 只管协议适配和单次调用。`Orchestrator` 加 turn 循环、审批、预算、以及自动的 session 上下文累积。

### 6.3 目录结构

```
agent-wrapper/
├── agent.go                  # Agent 接口 + Provider 枚举
├── session.go                # Session + Message 类型 + SessionStore 接口
├── session_memory.go         # MemorySessionStore 实现
├── event.go                  # Event 类型定义
├── orchestrator.go           # Turn 循环编排器
├── registry.go               # Provider 注册表
├── process.go                # 子进程管理器
├── claude/
│   └── agent.go              # ClaudeCodeAgent 实现
├── codex/
│   └── agent.go              # CodexAgent 实现
├── pi/
│   └── agent.go              # PiAgent 实现
├── opencode/
│   └── agent.go              # OpenCodeAgent 实现
├── cmd/
│   └── agent-wrapper/
│       └── main.go           # CLI 入口
├── docs/
│   ├── quickstart.md
│   ├── architecture.md
│   ├── session.md
│   ├── providers.md
│   ├── approval.md
│   └── custom-provider.md
├── examples/
│   ├── basic/
│   ├── multi-turn/
│   ├── approval/
│   ├── budget/
│   └── custom-provider/
├── go.mod
├── go.sum
├── README.md
└── tasks/
    └── prd-agent-wrapper.md
```

### 6.4 命名

Go 包名：`agentwrapper`（import 路径 `github.com/smallnest/agent-wrapper`）。CLI 二进制名：`agent-wrapper`。

## 7. 技术考量

### 7.1 Go 版本
Go 1.22+。

### 7.2 依赖
- 标准库优先：`os/exec`、`context`、`bufio`、`encoding/json`、`sync`、`net`（UUID 生成）
- 零外部依赖。所有协议解析手写，不引入第三方 JSON-RPC 或 SSE 库。

### 7.3 并发安全
- `MemorySessionStore` 内部使用 `sync.RWMutex`，支持多 goroutine 并发读写
- `Session` 的 `Messages` 追加操作在 store 层面原子保护
- 子进程 stdout 读取和 stdin 写入在不同 goroutine 中进行
- `Orchestrator` 内部用 channel 串联事件

### 7.4 错误处理
- 子进程异常退出 → 返回 `*ExitError`，包含退出码和 stderr
- 协议解析错误 → 返回 `*ProtocolError`，包含原始字节便于调试
- 超时 → 返回 `context.DeadlineExceeded`
- 预算耗尽 → 返回 `*BudgetExceededError`，包含已用/上限 token 数
- 会话不存在 → 返回 `*SessionNotFoundError`

### 7.5 测试策略
- 单元测试：每个 provider 的协议解析用 mock 子进程输出（`strings.NewReader` 模拟 stdout）
- 集成测试：需要安装对应 CLI 时用 `t.Skip()` 跳过，CI 中安装真实 CLI 再跑
- 每个 provider 子目录下测试覆盖率 > 80%

## 8. 成功指标

- 四个 provider 实现全部通过接口一致性测试（同一套 test suite 跑在所有 provider 上）
- 同一 session 连续 5 次 `Run()` 后，第 6 次 agent 请求包含全部前 5 次的消息历史
- 覆盖率达到 80%+ (`go test -cover ./...`)
- `go vet ./...` 零警告
- `go test -race ./...` 零数据竞争
- 文档覆盖所有公开 API（`go doc -all` 可读）
- CLI 端到端测试通过
- 外部开发者能在 5 分钟内通过 `examples/basic/main.go` 跑通第一个 agent 调用

## 9. 未决问题

- **Pi Agent 的协议格式？** Pi Agent CLI 是否支持 JSON 模式或机器可读输出？需要实际调研后确认。
- **OpenCode 的协议格式？** OpenCode 是否有 JSON/NDJSON 输出模式？
- **Windows 兼容性？** 一期是否支持 Windows？`os/exec` 在 Windows 下有差异（如 SIGTERM 不可用）。
- **Go module path？** 确认 `github.com/smallnest/agent-wrapper` 吗？
- **Provider 特有的工具注册语法差异如何处理？** 各 agent 的工具定义格式不同，`RunInput.AllowedTools` 如何映射到各 provider 的参数？
- **消息角色映射表？** `Message{Role: "tool_result"}` 在不同 agent 的 native 格式中如何表达？（Claude Code: `tool_result` content block；OpenAI/Codex: `role: "tool"` message）
