# PRD: Multi-Agent Expansion (agy, cursor-cli, kimi-code)

## Introduction

Add three new agent providers — agy (Google Antigravity CLI), cursor (Cursor Agent CLI), and kimi-code (MoonshotAI Kimi Code CLI) — to the agent-wrapper so users can run any of these coding agents through the unified `agent-wrapper` CLI. Each agent wraps its native subprocess, parses its output format into the common `types.Event` stream, and registers via `RegisterIn` into the shared registry.

All three CLIs are already installed on the user's machine at `~/.local/bin/`.

## Goals

- Add `agy`, `cursor`, and `kimi-code` as first-class provider names alongside the existing 4
- Each agent fully implements the `agentwrapper.Agent` interface (Run, Name, Provider, Close)
- Each agent parses the CLI's native output (text or stream-json) into `types.Event` stream
- Each agent auto-discovers its binary from PATH + common install paths
- CLI `--provider` flag accepts the new names: `agy`, `cursor`, `kimi-code`
- All three agents have tests

## User Stories

### US-001: Register agy, cursor, kimi-code provider names in types
**Description:** As a developer, I need provider constants for the new agents so they can be registered and discovered.

**Acceptance Criteria:**
- [ ] Add `ProviderAgy = "agy"`, `ProviderCursor = "cursor"`, `ProviderKimiCode = "kimi-code"` to `types/types.go`
- [ ] Add to `AllProviders()` slice
- [ ] Add stub entries in `registry.go` `NewRegistry()`
- [ ] `agent-wrapper list` shows the three new names
- [ ] Typecheck passes

### US-002: Implement agy agent
**Description:** As a user, I want to run the antigravity CLI through agent-wrapper so I can use it with the same interface as other agents.

**Acceptance Criteria:**
- [ ] `agents/agy/agent.go` — spawns `agy --print "<prompt>"` subprocess, captures stdout
- [ ] Binary auto-discovery: PATH → `~/.local/bin/agy`
- [ ] Parses output as raw text (agy `--print` mode outputs plain text, no JSON format available)
- [ ] Emits `EventTextDelta` with full text output, then `EventTurnEnd`
- [ ] Supports `--continue` as session resume (agy has `--continue` flag)
- [ ] `agents/agy/agent.go` — `RegisterIn` wired
- [ ] Tests in `agents/agy/agent_test.go`
- [ ] Typecheck passes

### US-003: Implement cursor agent
**Description:** As a user, I want to run Cursor Agent CLI through agent-wrapper so I can use it with the same interface as other agents.

**Acceptance Criteria:**
- [ ] `agents/cursor/agent.go` — spawns `agent --print --output-format stream-json "<prompt>"` subprocess
- [ ] Binary auto-discovery: PATH → `~/.local/bin/agent`
- [ ] Parses NDJSON stream (similar to claude agent pattern; `--output-format stream-json`)
- [ ] Emits `EventTextDelta`, `EventToolCall`, `EventToolResult`, `EventTurnEnd` events
- [ ] Supports `--resume <sessionID>` for session resume
- [ ] Supports `--model <model>` option
- [ ] Supports `--workspace <path>` via `input.WorkingDir`
- [ ] Supports `--yolo` (auto-approve) via extra options
- [ ] `agents/cursor/convert.go` — NDJSON event parser (similar to `claude/convert.go`)
- [ ] `agents/cursor/agent.go` — `RegisterIn` wired
- [ ] Tests in `agents/cursor/agent_test.go`
- [ ] Typecheck passes

### US-004: Implement kimi-code agent
**Description:** As a user, I want to run Kimi Code CLI through agent-wrapper so I can use it with the same interface as other agents.

**Acceptance Criteria:**
- [ ] `agents/kimi-code/agent.go` — spawns `kimi --prompt "<prompt>" --output-format stream-json` subprocess
- [ ] Binary auto-discovery: PATH → `~/.kimi-code/bin/kimi` → `~/.local/bin/kimi-cli`
- [ ] Parses NDJSON stream from `--output-format stream-json`
- [ ] Emits `EventTextDelta`, `EventToolCall`, `EventToolResult`, `EventTurnEnd` events
- [ ] Supports `--session <sessionID>` for session resume
- [ ] Supports `--continue` to resume last session
- [ ] Supports `--model <model>` option
- [ ] Supports `--yolo` (auto-approve) via extra options
- [ ] Supports `--plan` (plan mode) via extra options
- [ ] `agents/kimi-code/convert.go` — NDJSON event parser
- [ ] `agents/kimi-code/agent.go` — `RegisterIn` wired
- [ ] Tests in `agents/kimi-code/agent_test.go`
- [ ] Typecheck passes

### US-005: Wire new agents into CLI main.go
**Description:** As a user, I want `agent-wrapper run --provider agy|cursor|kimi-code` to work.

**Acceptance Criteria:**
- [ ] `cmd/agent-wrapper/main.go` imports all three new agents
- [ ] `cmdRun()` and `cmdList()` register all three via `RegisterIn`
- [ ] `--provider` help text updated to list all 7 providers
- [ ] `agent-wrapper run --provider agy "hello"` works end-to-end
- [ ] `agent-wrapper run --provider cursor "hello"` works end-to-end
- [ ] `agent-wrapper run --provider kimi-code "hello"` works end-to-end
- [ ] Typecheck passes

## Functional Requirements

- FR-1: System must add `agy`, `cursor`, `kimi` provider constants to `types/types.go`
- FR-2: System must register stub entries for the three new providers in `registry.go`
- FR-3: System must implement agy agent that wraps `agy --print "<prompt>"` subprocess
- FR-4: System must implement cursor agent that wraps `agent --print --output-format stream-json "<prompt>"` subprocess
- FR-5: System must implement kimi-code agent that wraps `kimi --prompt "<prompt>" --output-format stream-json` subprocess
- FR-6: Each agent must auto-discover its binary from PATH and `~/.local/bin/`
- FR-7: Each agent must register itself via `RegisterIn` into the registry
- FR-8: CLI `--provider` flag must accept `agy`, `cursor`, `kimi-code` as valid values

## Non-Goals

- No ACP server mode support (kimi-code has `kimi acp`, but we wrap the CLI directly instead)
- No interactive mode — print/non-interactive mode only
- No MCP server configuration passthrough
- No plugin passthrough
- No worktree mode passthrough (cursor has `--worktree`, skip for now)

## Design Considerations

- Follow existing pattern from `agents/claude/` — Agent struct with Options, resolveBinary, Run, parseEvent
- `cursor` and `kimi` both support `--output-format stream-json` → reuse NDJSON scanner pattern
- `agy` only has text output (`--print` mode) → simpler: read all stdout, emit as single text_delta then turn_end

## Technical Considerations

### CLI Surface Summary

| Feature | agy | cursor | kimi-code |
|---------|-----|--------|-----------|
| Non-interactive flag | `--print` / `-p` | `--print` / `-p` | `--prompt <text>` / `-p` |
| JSON output | None | `--output-format stream-json` | `--output-format stream-json` |
| Session resume | `--continue` (last session) or `--conversation <id>` | `--resume <id>` or `--continue` | `--session <id>` / `--continue` |
| Model select | None | `--model <model>` | `--model <model>` |
| Work dir | `--add-dir` only | `--workspace <path>` | None (uses cwd) |
| Auto-approve | `--dangerously-skip-permissions` | `--yolo` or `--force` | `--yolo` / `-y` |
| Max turns | None | None | None |
| Plan mode | None | `--plan` | `--plan` |
| Binary name | `agy` | `agent` | `kimi` (at `~/.kimi-code/bin/kimi` or `~/.local/bin/kimi-cli`) |

### agy Output Format
`agy --print "prompt"` outputs plain text to stdout. No JSON mode. The agent reads all stdout until process exit, emits one `EventTextDelta` with the full text, then `EventTurnEnd`.

### cursor Output Format
`agent --print --output-format stream-json "prompt"` outputs NDJSON. Need to discover the exact JSON schema at implementation time (likely similar to Claude's stream-json).

### kimi-code Output Format
`kimi --prompt "prompt" --output-format stream-json` outputs NDJSON. Binary is at `~/.kimi-code/bin/kimi` (v0.8.0) or `~/.local/bin/kimi-cli` (legacy v1.46.0). The v0.8.0 binary uses simpler flags (no `--print`, prompt goes after `--prompt`). Need to discover the exact JSON schema at implementation time.

## Success Metrics

- `agent-wrapper list` shows 7 providers (existing 4 + 3 new)
- `agent-wrapper run --provider agy "hello"` completes without error
- `agent-wrapper run --provider cursor "hello"` completes without error
- `agent-wrapper run --provider kimi-code "hello"` completes without error
- All new agent tests pass

## Open Questions

- Exact NDJSON schema for cursor and kimi-code `stream-json` output — discover at implementation time by running the CLIs with `--output-format stream-json`
- Whether cursor/kimi-code NDJSON events map 1:1 to the Claude event schema or need separate parse functions
- kimi-code v0.8.0 has no `--work-dir` flag — handle working directory via process config's WorkDir (cwd-based)
