# PRD: ACP Client Provider

## Introduction

agent-wrapper 当前 4 个内置 provider（claude/codex/pi/opencode）均通过 CLI 子进程 stdin/stdout 通信。本项目不实现 ACP 协议，但可以通过 `Agent` 接口接入 ACP agent——让 agent-wrapper 既能包装 CLI，也能对接 ACP server。

## Goals

- 实现一个 `acp` provider，通过 ACP JSON-RPC（stdio）连接任意 ACP agent
- 将 ACP 事件映射到 `types.Event`（text_delta / tool_call / tool_result / turn_end）
- 支持 ACP session 管理（透传 session ID）
- ACP agent 与 CLI agent 在 Registry 中平等共存，`RunInput` 接口不变

## User Stories

### US-001: Add acp Agent provider
**Description:** As a developer, I want to connect to any ACP-compatible agent through the same Agent interface, so I can use ACP agents alongside CLI agents.

**Acceptance Criteria:**
- [ ] New package `acp` with `AcpAgent` implementing `agentwrapper.Agent`
- [ ] Options: `BinaryPath` (acpx or any ACP binary), `Model`, `Extra`
- [ ] Spawns ACP child process via JSON-RPC over stdio
- [ ] Parses ACP events (content_delta, tool_use_start, tool_use_stop, complete, error)
- [ ] Maps to types.Event with correct SessionID propagation
- [ ] CI lint passes
- [ ] CI tests pass

### US-002: Add ACP session support
**Description:** As a developer, I want ACP session IDs to flow through RunResult, so I can resume ACP sessions like CLI agent sessions.

**Acceptance Criteria:**
- [ ] Parse session ID from ACP init event
- [ ] Attach SessionID to all subsequent events
- [ ] SessionID appears in RunResult
- [ ] Support `--session-id` to resume (passed through to ACP binary)
- [ ] CI lint passes
- [ ] CI tests pass

### US-003: Register in CLI and Registry
**Description:** As a CLI user, I want `agent-wrapper run --provider acp` to work with ACP agents.

**Acceptance Criteria:**
- [ ] `acp.RegisterIn(registry)` registers `"acp"` provider
- [ ] CLI accepts `--provider acp` and `acp` specific options
- [ ] Default ACP binary = `acpx` (fallback to PATH lookup)
- [ ] CI lint passes
- [ ] CI tests pass

### US-004: Add acp example and docs
**Description:** As a developer, I want a working example showing how to use the ACP provider.

**Acceptance Criteria:**
- [ ] `examples/acp/main.go`: demonstrates connecting to an ACP agent
- [ ] README updated with ACP provider in provider table
- [ ] `docs/providers.md` updated with ACP entry
- [ ] CI lint passes
- [ ] CI tests pass

## Functional Requirements

- FR-1: System must provide `acp` package implementing `Agent` interface
- FR-2: ACP agent must communicate via JSON-RPC over stdio with the ACP binary
- FR-3: ACP events must be mapped to `types.Event` types
- FR-4: Session ID must be captured from ACP init events and propagated
- FR-5: `--session-id` must be forwarded to the ACP binary for session resume
- FR-6: ACP provider must register as `"acp"` in the Registry

## Non-Goals

- 不实现 ACP 协议本身——依赖现有 ACP binary（如 acpx）
- 不支持 ACP HTTP/WebSocket 传输（V1 仅 stdio）
- 不提供 ACP 协议的 Go 实现或库
- 不支持 ACP agenda 模式（批量异步执行）

## Technical Considerations

- ACP binary 默认使用 `acpx`，可通过 `BinaryPath` 覆盖
- ACP JSON-RPC 消息格式：`{"jsonrpc":"2.0","id":N,"method":"...","params":{...}}`
- 事件类型参考 ACP 规范：`content_delta`、`tool_use_start`、`tool_use_stop`、`complete`、`error`
- Session ID 来源：ACP `initialize` 响应中的 `sessionId` 字段
- 复用现有 `process.StartProcess` 和 `process.NewJSONRPCScanner`

## Open Questions

- ACP JSON-RPC 是双向协议（client 需发送 `initialize` 请求）——需要扩展 `process` 包支持 stdin 写入
- ACP binary `acpx` 的 JSON 输出格式是否与 CLI agent 相同？需要验证真实输出
- ACP transport 未来扩展（HTTP/WebSocket）是否需要统一抽象？V1 不处理
