# Provider 特性对比

## 概览

| 特性 | Claude Code | Cursor | Kimi Code | Antigravity | Codex | Pi Agent | OpenCode | ACP |
|------|------------|--------|-----------|-------------|-------|----------|----------|-----|
| 协议 | CLI stream-json | CLI --print stream-json | CLI --prompt stream-json | CLI --print (text) | CLI exec --json | CLI --mode json | CLI run --format json | ACP JSON-RPC |
| 流式输出 | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| 工具调用 | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| 工具结果 | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Token 用量 | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| Session 管理 | ✅ --resume | ✅ --resume | ✅ --session | ✅ --conversation | ✅ --resume | ✅ --session-id | ✅ --session | ✅ |
| System Prompt | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ |
| Binary 自动检测 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

## 各 Provider 详解

### Claude Code (`claude-code`)

**启动命令**: `claude -p <prompt> --output-format stream-json --verbose`

**Binary 搜索路径**: PATH, `~/.local/bin/claude`, `~/.npm-global/bin/claude`

**特点**:
- NDJSON 事件流（system / assistant / result 三种事件类型）
- 完整支持 text_delta、tool_use、tool_result、turn_end
- Token 用量包含在 result 事件中
- 支持 `--model`、`--max-turns`、`--allowedTools`、`--resume` 参数

**安装**: `npm install -g @anthropic-ai/claude-code`

### Cursor Agent (`cursor`)

**启动命令**: `agent --print --output-format stream-json <prompt>`

**Binary 搜索路径**: PATH, `~/.local/bin/agent`

**特点**:
- NDJSON 事件流，格式类似 claude-code
- 支持 `--model <model>`、`--workspace <path>`、`--yolo`（自动审批）
- 支持 `--resume <id>` 和 `--continue` 恢复会话
- Plan 模式可通过 `--plan` 启用

**安装**: 通过 Cursor IDE 内置安装，或从 [cursor.com](https://cursor.com) 获取

### Kimi Code (`kimi-code`)

**启动命令**: `kimi --prompt <prompt> --output-format stream-json`

**Binary 搜索路径**: PATH, `~/.kimi-code/bin/kimi`, `~/.local/bin/kimi-cli`（legacy）

**特点**:
- v0.8.0+ 使用简化的 `--prompt` flag（无 `--print`）
- NDJSON 流式输出
- 支持 `--model <model>`、`--yolo`（自动审批）、`--plan`（plan 模式）
- 支持 `--session <id>` 和 `--continue` 恢复会话
- 无 `--work-dir` flag — 通过子进程 cwd 设置工作目录
- Legacy binary `kimi-cli`（v1.46.0）在 `~/.local/bin/` 也被自动发现

**安装**: `uv tool install kimi-code` 或访问 [moonshotai.github.io/kimi-code](https://moonshotai.github.io/kimi-code)

### Antigravity (`agy`)

**启动命令**: `agy --print <prompt>`

**Binary 搜索路径**: PATH, `~/.local/bin/agy`

**特点**:
- 纯文本输出模式（无 JSON 格式），读取完整 stdout 作为响应
- 不支持流式、工具调用、token 用量（API 限制）
- 支持 `--conversation <id>` 和 `--continue` 恢复会话
- 支持 `--dangerously-skip-permissions`（自动审批）
- 适合简单的 Q&A 场景，不适合需要工具调用的复杂任务

**安装**: 从 [Google Labs](https://labs.google.com/antigravity) 获取

### Codex (`codex`)

**启动命令**: `codex exec --json <prompt>`

**Binary 搜索路径**: PATH, `~/.local/bin/codex`, `~/.npm-global/bin/codex`

**特点**:
- `exec --json` 模式返回 JSON（非流式）
- 支持 `--model <model>`、`--resume <id>`、`--max-turns <n>`
- 工具调用和结果在 JSON 响应中包含

**安装**: `npm install -g @openai/codex`

### Pi Agent (`pi-agent`)

**启动命令**: `pi -p <prompt> --mode json`

**Binary 搜索路径**: PATH, `~/.local/bin/pi`

**特点**:
- JSONL 事件流（`-p --mode json`），非 RPC 模式
- 支持 `--model <model>`、`--provider <provider>`、`--system-prompt <text>`
- 支持 `--session-id <id>` 和 `--no-session`

**安装**: `npm install -g @anthropic-ai/pi`

### OpenCode (`opencode`)

**启动命令**: `opencode -p <prompt> -f json -q`

**Binary 搜索路径**: PATH, `~/.local/bin/opencode`, `~/go/bin/opencode`

**注意**: OpenCode 项目已归档并更名为 [Crush](https://github.com/charmbracelet/crush)。

**安装**: `go install github.com/opencode-ai/opencode@latest`

### ACP (`acp`)

**启动命令**: `acpx`

**Binary 搜索路径**: PATH（可通过 `BinaryPath` 覆盖）

**特点**:
- 完整的 ACP 生命周期：Initialize → NewSession/ResumeSession → Prompt
- SessionUpdate 通知路由到 `types.Event`
- 自动审批权限请求

**安装**: `npm install -g acpx` 或使用任何 ACP 兼容二进制

## 注册 Provider

```go
registry := agentwrapper.NewRegistry()
claude.RegisterIn(registry)     // 替换 claude-code stub
cursor.RegisterIn(registry)     // 替换 cursor stub
kimicode.RegisterIn(registry)   // 替换 kimi-code stub（import alias: kimicode）
agy.RegisterIn(registry)        // 替换 agy stub
codex.RegisterIn(registry)      // 替换 codex stub
pi.RegisterIn(registry)         // 替换 pi-agent stub
opencode.RegisterIn(registry)   // 替换 opencode stub
acp.RegisterIn(registry)        // 替换 acp stub

// 导入路径示例
import (
    "github.com/smallnest/agent-wrapper/provider/claude"
    "github.com/smallnest/agent-wrapper/provider/cursor"
    kimicode "github.com/smallnest/agent-wrapper/provider/kimi-code"
    "github.com/smallnest/agent-wrapper/provider/agy"
)
```

## 自定义 Binary Path

```go
agent, _ := claude.New(claude.Options{BinaryPath: "/usr/local/bin/claude"})
```

或在 CLI 中：

```bash
agent-wrapper run --provider claude-code --binary-path /usr/local/bin/claude "hello"
```
