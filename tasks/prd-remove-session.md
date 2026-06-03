# PRD: Remove Wrapper Session, Pass-Through Runtime Session

## Introduction

当前 agent-wrapper 自己维护了一套 `Session`/`SessionStore`/`memory` 包，但四个 agent backend **从不读取 `Session.Messages` 的历史消息** — 每个 agent 只取最后一条 user message 作为 `-p` 参数传给子进程。

真正的对话历史由 agent CLI 自身的运行时管理（claude `--continue`/`--resume`/`--session-id`，codex `resume`，opencode `--continue`/`--session`）。

本 PRD 做两件事：
1. **删除** wrapper 层的 Session/SessionStore/memory — 它们是不被消费的假抽象
2. **透传** agent 运行时的 session 控制 — `RunInput.SessionID` → agent 原生 flag

## Goals

- 删除 `types.Session`、`types.SessionSummary`、`types.NewSession`、`SessionStore` 接口、`sessionstore/memory` 包
- `RunInput` 改为 `Prompt string` + `SessionID string`
- `SessionID` 为空时：one-shot 模式，agent 子进程不传任何 session 相关 flag
- `SessionID` 非空时：通过 agent 原生 flag 恢复/继续会话
- `RunResult.SessionID` 保留，agent 解析子进程输出中的 session_id 并填充
- CLI 不再暴露 session 相关概念（已精简）

## User Stories

### US-001: Remove Session types and SessionStore interface
**Description:** As a developer, I want dead code removed so the codebase is smaller and easier to understand.

**Acceptance Criteria:**
- [ ] Delete `types.Session`, `types.SessionSummary`, `types.NewSession`
- [ ] Delete `types.NewUserMessage`, `types.NewAssistantMessage`, `types.NewToolUseMessage`, `types.NewToolResultMessage`
- [ ] Delete `SessionStore` interface (`session.go`)
- [ ] Delete `sessionstore/memory` package entirely
- [ ] Remove all `session.go`/`sessionstore` references from `go.mod` (no sub-packages left)
- [ ] CI lint passes
- [ ] CI tests pass

### US-002: Refactor RunInput to Prompt + SessionID
**Description:** As a developer, I want RunInput to directly accept a prompt string and optional runtime session ID, without the indirection of wrapper Session.

**Acceptance Criteria:**
- [ ] `RunInput` struct: `Prompt string` replaces `Session *Session` + `NewMessage *Message`
- [ ] `RunInput.SessionID string` added — forwarded to agent runtime
- [ ] `Orchestrator.Run` adapts: creates ephemeral session internally for message tracking (writeback to nil store)
- [ ] `Orchestrator.RunSync` adapts: passes Prompt + SessionID to Run
- [ ] `RunResult.SessionID` kept, filled from event stream
- [ ] CI lint passes
- [ ] CI tests pass

### US-003: Map SessionID to native agent flags
**Description:** As a user, when I set `SessionID`, I want the agent runtime to resume my conversation using its native mechanism.

**Acceptance Criteria:**
- [ ] claude: `SessionID != ""` → `--session-id <id>`; else one-shot（当前行为）
- [ ] codex: `SessionID != ""` → `--session <id>`; else one-shot
- [ ] pi: `SessionID != ""` → remove `--no-session`, pass `--session-id <id>`; else keep `--no-session`
- [ ] opencode: `SessionID != ""` → `--session <id>`; else one-shot
- [ ] CI lint passes
- [ ] CI tests pass

### US-004: Extract session_id from agent output into RunResult
**Description:** As a developer, I want the runtime session ID from the agent subprocess to be captured in RunResult, so callers can store it for later resume.

**Acceptance Criteria:**
- [ ] claude: parse `SessionID` from system/init event → set `RunResult.SessionID`
- [ ] codex: parse session_id from protocol output → set `RunResult.SessionID`
- [ ] pi: session ID not available in current protocol → leave empty
- [ ] opencode: parse `SessionID` from raw event → set `RunResult.SessionID`
- [ ] CI lint passes
- [ ] CI tests pass

### US-005: Update all tests
**Description:** As a developer, I want existing tests to pass after the Session removal refactor.

**Acceptance Criteria:**
- [ ] All orchestrator tests adapted to use `Prompt` + `SessionID` instead of `Session` + `NewMessage`
- [ ] All agent tests adapted
- [ ] Retry tests adapted
- [ ] Compressor tests adapted (operate on `[]Message` internally — unchanged)
- [ ] CI lint passes
- [ ] CI tests pass

### US-006: Update examples and docs
**Description:** As a developer, I want examples and docs to reflect the simplified API.

**Acceptance Criteria:**
- [ ] `examples/basic/main.go`: use `Prompt` instead of `Session`
- [ ] `examples/multi-turn/main.go`: use `SessionID` for resume
- [ ] `examples/approval/main.go`: use `Prompt`
- [ ] `examples/budget/main.go`: use `Prompt`
- [ ] `README.md`: update quick-start code
- [ ] `docs/quickstart.md`: update
- [ ] CI lint passes
- [ ] CI tests pass

## Functional Requirements

- FR-1: System must delete wrapper-level Session/SessionStore/memory
- FR-2: `RunInput` must have `Prompt string` and `SessionID string` fields
- FR-3: `SessionID` empty → agent runs one-shot（不传任何 session flag）
- FR-4: `SessionID` non-empty → agent passes native session flag to subprocess
- FR-5: `RunResult.SessionID` must be populated from agent subprocess output (when available)
- FR-6: Orchestrator must internally track messages for approval/budget/compression — but never persist them
- FR-7: Each agent must map `SessionID` to its provider-specific flag

## Non-Goals

- 不实现跨 agent run 的 session 管理（agent runtime 已做）
- 不实现 session 列表查询（agent runtime 已有 `claude sessions` / `opencode session` 等命令）
- 不修改压缩器、审批、预算的语义
- 不实现 SessionID 的自动生成（调用方提供或 agent runtime 生成并返回）

## Agent Session Flag Mapping

| Provider | One-shot (SessionID="") | Resume (SessionID="xxx") |
|----------|------------------------|--------------------------|
| claude | `claude -p "..." --output-format stream-json --verbose` | `claude -p "..." --output-format stream-json --verbose --resume xxx` |
| codex | `codex exec "..." --json` | `codex exec "..." --json --resume xxx` |
| pi | `pi -p "..." --mode json --no-session` | `pi -p "..." --mode json --session-id xxx` |
| opencode | `opencode run "..." --format json` | `opencode run "..." --format json --session xxx` |

**Verified flags:**
- claude: `--resume <value>` — resume by session ID (verified via `claude --help`)
- codex: `--resume <session_id>` — resume by session ID in exec mode (verified via `codex exec --help`)
- opencode: `--session <id>` — session ID to continue (verified via `opencode run --help`)
- pi: `--session-id <id>` — use exact project session ID

- Orchestrator 内部仍需要临时消息缓冲区（accumulate assistant/tool_use/tool_result），用于写回和审批决策。但缓冲区的生命周期仅在一次 `Run` 调用内
- `RunResult.SessionID` 来源优先级：agent 输出 > `input.SessionID`（fallback）
- 压缩器仍使用 `[]types.Message` — 消息类型本身保留（纯数据），不删除

## Open Questions

- PI agent 目前硬编码 `--no-session`，改为 `--session-id` 后需要确认 PI CLI 支持该 flag（PI binary 未安装，待验证）
- `RunResult.SessionID` 是 agent runtime 生成的 UUID，还是直接返回 `input.SessionID`？（当前选择：agent runtime 返回的实际 session ID，fallback 到 `input.SessionID`）
- ~~codex 的 `--session` flag~~ → 已验证为 `--resume`
