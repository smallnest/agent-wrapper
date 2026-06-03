# PRD: Context Compression & Retry

## Introduction

当前 agent-wrapper 调用底层 coding agent 时，如果 LLM 上下文超限（context length exceeded），调用直接失败，没有任何恢复机制。本功能引入上下文压缩与重试机制：检测到上下文超限错误后自动压缩消息历史并重试，提供可配置的压缩策略和重试次数，提升健壮性和用户体验。

## Goals

- 自动检测上下文超限错误（类型化错误 + 字符串匹配兜底）
- 提供两级压缩策略：滑动窗口截断 → 摘要压缩，默认链式尝试
- 支持用户通过接口注入自定义压缩器
- 在 Agent 层和 Orchestrator 层均支持重试
- 可配置最大重试次数（通过 OrchestratorOption），默认 3 次
- 压缩和重试过程对下游调用方完全透明

## User Stories

### US-001: Define ContextLengthExceededError type and detection helpers
**Description:** As a developer, I need a standard error type and detection function so agents can report and detect context-length errors consistently.

**Acceptance Criteria:**
- [ ] Add `ContextLengthExceededError` type implementing `error` interface
- [ ] Add `IsContextLengthExceeded(err error) bool` helper
- [ ] Helper returns true for the typed error OR for errors whose message contains known keywords ("context length", "token limit", "too long", "context_length_exceeded", "max_tokens")
- [ ] CI lint passes
- [ ] CI tests pass

### US-002: Define ContextCompressor interface
**Description:** As a developer, I need a pluggable `ContextCompressor` interface so users can provide custom compression strategies.

**Acceptance Criteria:**
- [ ] Define `ContextCompressor` interface with single method: `Compress(messages []types.Message) []types.Message`
- [ ] Interface lives in package `agentwrapper` (alongside orchestrator)
- [ ] CI lint passes
- [ ] CI tests pass

### US-003: Implement SlidingWindowCompressor
**Description:** As a user, when context is too large, I want the system to keep only the most recent messages so the agent can still respond with relevant context.

**Acceptance Criteria:**
- [ ] Implement `SlidingWindowCompressor` satisfying `ContextCompressor`
- [ ] Constructor accepts `RetainMessages int` (default 20)
- [ ] `Compress` returns last N messages, preserving system prompt messages (first message if role is user and appears to be a system prompt)
- [ ] Never returns fewer than 2 messages (last user + space for assistant)
- [ ] CI lint passes
- [ ] CI tests pass

### US-004: Implement SummaryCompressor
**Description:** As a user, when sliding window is still too large, I want earlier messages summarized into a single system message so key context is preserved while reducing token count.

**Acceptance Criteria:**
- [ ] Implement `SummaryCompressor` satisfying `ContextCompressor`
- [ ] Constructor accepts a `Summarizer func([]types.Message) (string, error)` — users inject their own LLM or use a default
- [ ] Default summarizer produces a concise English summary (naive fallback: concatenates first 100 chars of each message)
- [ ] Compression output: summary line prepended as a system-prompt-style user message, followed by last `RetainMessages` messages
- [ ] CI lint passes
- [ ] CI tests pass

### US-005: Implement ChainedCompressor
**Description:** As a user, I want the system to try sliding window first, then summary compression if the error persists, so the least destructive strategy is tried first.

**Acceptance Criteria:**
- [ ] Implement `ChainedCompressor` satisfying `ContextCompressor`
- [ ] Accepts `[]ContextCompressor` list, tries each in order until messages are reduced
- [ ] Constructor: `NewChainedCompressor(compressors ...ContextCompressor)`
- [ ] `Compress` returns output of first compressor that actually reduces message count; if none reduce, returns original
- [ ] CI lint passes
- [ ] CI tests pass

### US-006: Add context-exceeded detection and retry to Agent layer
**Description:** As a developer, I want each agent backend to detect context-length errors from subprocess stderr and return `ContextLengthExceededError` so the orchestrator can act on it.

**Acceptance Criteria:**
- [ ] Each agent (claude, codex, pi, opencode) inspects subprocess stderr on failure for known context-length keywords
- [ ] When detected, agent wraps the error with `ContextLengthExceededError`
- [ ] `Agent.Run` contract updated: on context exceeded, agent returns typed error (does NOT internally retry in V1)
- [ ] CI lint passes
- [ ] CI tests pass

### US-007: Add retry loop to Orchestrator with compression
**Description:** As a user, when the agent fails with context-length error, I want the orchestrator to compress the session messages and retry the turn automatically.

**Acceptance Criteria:**
- [ ] Add `WithContextCompressor(c ContextCompressor)` OrchestratorOption
- [ ] Add `WithMaxRetries(n int)` OrchestratorOption (default 3)
- [ ] On `agent.Run` returning `ContextLengthExceededError`: compress `session.Messages`, update session, retry `agent.Run`
- [ ] Retry count limited by `MaxRetries`; on exhaustion, return original error to caller
- [ ] Retries happen within the same orchestrator `Run` invocation, transparent to downstream event consumer
- [ ] CI lint passes
- [ ] CI tests pass

### US-008: Integration test for end-to-end retry+compression flow
**Description:** As a developer, I want an integration test proving the full retry pipeline works: simulate context exceeded → compress → retry → success.

**Acceptance Criteria:**
- [ ] Test uses a mock agent that returns `ContextLengthExceededError` on first call, then succeeds on retry
- [ ] Verifies `Compress` was called with correct messages
- [ ] Verifies session messages are modified after compression
- [ ] Verifies retry count is respected (3 failures → final error)
- [ ] CI lint passes
- [ ] CI tests pass

## Functional Requirements

- FR-1: System must detect context-length errors via `ContextLengthExceededError` type check and keyword matching on error strings
- FR-2: System must provide `SlidingWindowCompressor` retaining the last N messages by default (N=20)
- FR-3: System must provide `SummaryCompressor` that prepends a summary message before the retained window
- FR-4: System must provide `ChainedCompressor` to chain multiple compressors in fallback order
- FR-5: System must allow users to inject custom `ContextCompressor` via `WithContextCompressor` option
- FR-6: System must provide default compressor chain: `SlidingWindowCompressor` → `SummaryCompressor`
- FR-7: System must allow configuring max retries via `WithMaxRetries` (default 3, minimum 0 = no retry)
- FR-8: Each agent backend must inspect subprocess stderr and wrap context-length errors as `ContextLengthExceededError`
- FR-9: Orchestrator `Run` must catch `ContextLengthExceededError` from `agent.Run`, compress session messages, and retry
- FR-10: Retry must preserve the user's latest message (the one that triggered the current turn)

## Non-Goals

- 不在 agent 内部做自动压缩重试（V1 只在 orchestrator 层处理）
- 不引入外部 LLM 依赖（默认摘要器使用 naive 字符串截断）
- 不支持指数退避或 backoff（立即重试）
- 不修改 Event 协议（压缩重试对下游透明）
- 不对非上下文超限的错误进行重试

## Technical Considerations

- `ContextCompressor` 接口接收 `[]types.Message` 返回 `[]types.Message`，不修改原始 session
- 压缩后的 messages 写回 `session.Messages`，压缩前的历史是否保留由 compressor 实现决定
- `NewMessage`（本轮用户输入）必须在压缩后保留（注入回末尾）
- `Agent.Run` 返回 `ContextLengthExceededError` 时，agent 子进程已退出；重试时重新启动子进程
- 重试发生在 `Orchestrator.Run` 的 goroutine 内，事件通道保持同一个

## Success Metrics

- 上下文超限场景下，不低于 80% 的 case 通过一次压缩（滑动窗口）恢复
- 两次压缩（滑动窗口 + 摘要）后恢复率 ≥ 95%
- 重试过程不产生重复事件或事件丢失
- 无回归：现有所有测试通过

## Open Questions

- 滑动窗口默认保留 20 条消息是否合理？（可通过选项调整）
- 是否需要为不同 provider 提供不同的默认压缩参数？（V1 统一处理）
