# Provider 特性对比

## 概览

| 特性 | Claude Code | Codex | Pi Agent | OpenCode | ACP |
|------|------------|-------|----------|----------|-----|
| 协议 | CLI stream-json | CLI exec --json | CLI --mode json | CLI run --format json | ACP JSON-RPC |
| 流式输出 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 工具调用 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 工具结果 | ✅ | ✅ | ✅ | ✅ | ✅ |
| Token 用量 | ✅ | ✅ | ✅ | ✅ | ✅ |
| Session 管理 | ✅ --resume | ✅ --resume | ✅ --session-id | ✅ --session | ✅ ACP resume |
| System Prompt | ✅ | ✅ | ✅ | ✅ (env var) |
| Binary 自动检测 | ✅ | ✅ | ✅ | ✅ |
| 上下文取消 | ✅ | ✅ | ✅ | ✅ |

## 各 Provider 详解

### Claude Code (`claude-code`)

**启动命令**: `claude agent`

**协议**: JSON-RPC 2.0，stdin 写入请求，stdout 读取通知。

**特点**:
- 完整的消息历史注入（通过 `initialize` 方法）
- 流式 TextDelta、ToolUse、ToolResult 通知
- TurnEnd 包含 TokenUsage
- 支持 `--model`、`--max-turns`、`--allowedTools` 参数

**安装**: `npm install -g @anthropic-ai/claude-code`

**二进制搜索路径**: PATH, `~/.local/bin/claude`, `~/.npm-global/bin/claude`

### Codex (`codex`)

**启动命令**: `codex chat --model <model>`

**协议**: SSE (OpenAI Chat Completions 格式)，stdin 写入请求 JSON，stdout 读取 SSE 流。

**特点**:
- OpenAI Chat Completions 兼容格式
- 流式 Content delta、ToolCalls
- 支持 `delta.tool_calls` 增量
- 默认模型: `codex-mini-latest`

**安装**: `npm install -g @openai/codex`

**二进制搜索路径**: PATH, `~/.local/bin/codex`, `~/.npm-global/bin/codex`

### Pi Agent (`pi-agent`)

**启动命令**: `pi --mode rpc --no-session`

**协议**: JSONL，stdin 写入命令，stdout 读取事件。

**特点**:
- RPC 模式接受单条 prompt
- `message_update` 事件包含 TextDelta 和 ToolCallDelta
- `tool_execution_start/end` 事件
- `agent_end` 标记会话结束
- 支持 `--model`、`--provider`、`--system-prompt` 参数

**安装**: `npm install -g @anthropic-ai/pi`

**二进制搜索路径**: PATH, `~/.local/bin/pi`

### OpenCode (`opencode`)

**启动命令**: `opencode -p <prompt> -f json -q`

**协议**: 非交互模式，单次 JSON 输出 `{"response":"text"}`。

**限制**:
- ✅ 流式输出（`run --format json`）
- ✅ 工具调用和结果事件
- ❌ 不支持 system prompt
- ✅ 支持 `-m` model flag

**注意**: OpenCode 项目已归档并更名为 [Crush](https://github.com/charmbracelet/crush)。

**安装**: `go install github.com/opencode-ai/opencode@latest`

**二进制搜索路径**: PATH, `~/.local/bin/opencode`, `~/go/bin/opencode`

## 注册 Provider

```go
registry := agentwrapper.NewRegistry()
claude.RegisterIn(registry)   // 替换 claude-code stub
codex.RegisterIn(registry)    // 替换 codex stub
pi.RegisterIn(registry)       // 替换 pi-agent stub
opencode.RegisterIn(registry) // 替换 opencode stub

agent, _ := registry.Get("claude-code", nil)
```

### ACP (`acp`)

**启动命令**: `acpx` (默认)

**协议**: ACP JSON-RPC over stdio，通过 `github.com/coder/acp-go-sdk` 通信。

**特点**:
- 完整的 ACP 生命周期：Initialize → NewSession/ResumeSession → Prompt
- SessionUpdate 通知路由到 `types.Event`（text_delta、tool_call、tool_result）
- 支持 session resume（透传 `--session` flag）
- 自动审批权限请求（默认允许第一个选项）

**安装**: `npm install -g acpx` 或使用任何 ACP 兼容二进制

**二进制搜索路径**: PATH，可通过 `BinaryPath` 覆盖

## 自定义 Binary Path

```go
agent, _ := claude.New(claude.Options{BinaryPath: "/usr/local/bin/claude"})
```

或在 CLI 中：

```bash
agent-wrapper run --provider claude-code --binary-path /usr/local/bin/claude "hello"
```
