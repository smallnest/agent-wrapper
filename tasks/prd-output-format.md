# PRD: Output Format — Stream, JSON, and Stream-JSON

## Introduction

当前 agent-wrapper CLI 的 `run` 命令硬编码了输出格式：文本增量输出到 stdout，元数据（工具调用、token 用量、错误）打印到 stderr。无法以结构化的方式消费输出（如管道给 jq、集成到其他工具），也无法选择聚合式 JSON 输出。

本功能引入三种输出格式和对应的 API 收集器，让调用方可以按场景选择流式文本、逐行 NDJSON、或单个 JSON 结果。

## Goals

- CLI `run` 支持 `--json` 和 `--stream` flag 控制输出格式
- `--stream`（默认）：保持当前行为，text→stdout，meta→stderr
- `--json`：所有输出合并到 stdout，输出单个 JSON 对象 `{ text, usage, session_id }`
- `--json --stream`：逐行 NDJSON（stream-json），每行一个 Event JSON
- API 层新增 `RunResult` 类型和 `Orchestrator.RunSync` 方法
- 三种输出格式全部输出到 stdout，stderr 仅用于 fatal error（如参数错误、进程启动失败）

## User Stories

### US-001: Add RunResult type to types package
**Description:** As a developer, I need a structured result type so I can access the final output programmatically without consuming an event channel.

**Acceptance Criteria:**
- [ ] Define `RunResult` struct: `Text string`, `Usage *TokenUsage`, `SessionID string`
- [ ] Define `OutputFormat` type: `"stream"`, `"json"`, `"stream-json"`
- [ ] Add `OutputFormat` field to `RunInput`
- [ ] `RunResult` has JSON tags for marshaling
- [ ] CI lint passes
- [ ] CI tests pass

### US-002: Add RunSync method to Orchestrator
**Description:** As a developer, I want a synchronous API that collects all events and returns a `RunResult` instead of manually draining a channel.

**Acceptance Criteria:**
- [ ] `Orchestrator.RunSync(ctx, input) (*types.RunResult, error)`
- [ ] Internally calls `Run`, drains the channel, accumulates text
- [ ] Tracks last `TokenUsage` and `session.ID` for the result
- [ ] On `EventError`, returns the error (no result)
- [ ] CI lint passes
- [ ] CI tests pass

### US-003: Add stream-json output mode to CLI
**Description:** As a user, I want `agent-wrapper run --json --stream` to emit one JSON line per event, so I can pipe to `jq` or log processors for real-time structured monitoring.

**Acceptance Criteria:**
- [ ] `--json --stream` flag combination enables stream-json mode
- [ ] Each line is `json.Marshal(types.Event)` with `\n` delimiter
- [ ] All events output to stdout (text_delta, tool_call, tool_result, turn_end, error)
- [ ] No output to stderr (except banner/fatal errors)
- [ ] Verify with `go run ./cmd/agent-wrapper run --provider claude-code --json --stream "hello" | jq .`
- [ ] CI lint passes
- [ ] CI tests pass

### US-004: Add json (aggregated) output mode to CLI
**Description:** As a user, I want `agent-wrapper run --json` to output a single JSON object after the run completes, so I can use it in scripts and CI pipelines.

**Acceptance Criteria:**
- [ ] `--json` (without `--stream`) triggers aggregated JSON mode
- [ ] CLI uses `RunSync` to collect the result
- [ ] Output: `{"text":"...","usage":{"input_tokens":N,"output_tokens":N,"total_tokens":N},"session_id":"uuid"}`
- [ ] On error: output `{"error":"message"}` to stdout, exit non-zero
- [ ] CI lint passes
- [ ] CI tests pass

### US-005: Refactor CLI run command to use output format dispatch
**Description:** As a developer, I want the run command dispatch logic to be clean and testable, with the existing stream behavior preserved as default.

**Acceptance Criteria:**
- [ ] `--json` and `--stream` flags added to `parseRunFlags`
- [ ] Default mode (`--stream` without `--json`): behaves identically to current behavior (text→stdout, meta→stderr)
- [ ] `--json` without `--stream`: calls `RunSync`, prints JSON object to stdout
- [ ] `--json --stream`: calls `Run`, serializes each event as JSON line to stdout
- [ ] Fatal errors (missing provider, session not found) still print to stderr and exit non-zero
- [ ] CI lint passes
- [ ] CI tests pass

## Functional Requirements

- FR-1: System must define `OutputFormat` type with values `"stream"`, `"json"`, `"stream-json"`
- FR-2: System must define `RunResult` struct with `Text`, `Usage`, `SessionID` fields
- FR-3: `RunInput` must include `OutputFormat` field (hint for agent, but primarily used by orchestrator/CLI)
- FR-4: `Orchestrator.RunSync` must drain the event channel, accumulate text, and return `RunResult`
- FR-5: CLI `--json` flag must switch output to single JSON object mode (via `RunSync`)
- FR-6: CLI `--stream` flag (default true) must control whether `--json` outputs stream-json or aggregate-json
- FR-7: All structured output must go to stdout; stderr reserved for fatal errors and startup diagnostics
- FR-8: `stream-json` mode must emit each event as a complete JSON line (`json.Marshal` + `\n`)

## Non-Goals

- 不修改底层 agent 子进程的参数（`--output-format` 是 CLI 消费端的行为，不影响传给子进程的参数）
- 不修改 Event 协议（Event 结构体已有的 JSON tags 直接复用）
- 不支持非 JSON 的输出格式（如 YAML、CSV）
- 不做 stdout/stderr 重定向到文件（用户可以用 shell 重定向）

## Design Considerations

- `--stream` flag 默认为 true，所以 `agent-wrapper run "hello"` 行为不变
- `--json` 单独使用 = 聚合 JSON；`--json --stream` = 流式 JSON
- 组合规则：`--json --stream=false` = 聚合 JSON，`--stream --json=false` = 当前默认文本流模式

## Technical Considerations

- `RunSync` 在内部调用 `Run` 然后 drain channel，复用以避免逻辑分叉
- `RunSync` 返回 error 时调用方不需要检查 `RunResult`（类似 `os.ReadFile` 模式）
- Event 已有 `json:"..."` tags，stream-json 直接 `json.Marshal` 即可
- `TokenUsage` 使用最后一个 `EventTurnEnd` 中的值

## Success Metrics

- CLI 的 `--json` 输出可以通过 `jq .text` 提取纯文本
- stream-json 输出可以通过 `jq -s` 完整重建所有事件
- 现有 `agent-wrapper run --provider codex "hello"` 默认行为不变（无回归）
- `RunSync` 能覆盖所有 CLI 聚合 JSON 的需求

## Open Questions

- `--stream` flag 作为独立 flag 还是 `--json` 的子选项？（当前选独立 flag）
- 聚合 JSON 中是否需要包含 `turns` 数量？
- 是否需要 `--pretty` flag 输出格式化 JSON（vs 紧凑 JSON）？
