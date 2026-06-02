# Provider 特性对比

## 概览

| 特性 | Claude Code | Codex | Pi Agent | OpenCode |
|------|------------|-------|----------|----------|
| 协议 | JSON-RPC 2.0 | SSE (OpenAI) | JSONL (RPC mode) | 非交互模式 |
| 流式输出 | ✅ TextDelta | ✅ Content delta | ✅ text_delta | ❌ 一次性输出 |
| 工具调用 | ✅ | ✅ | ✅ | ❌ |
| 工具结果 | ✅ | ✅ | ✅ | ❌ |
| Token 用量 | ✅ | ✅ | ❌ | ❌ |
| Session 注入 | ✅ 完整历史 | ✅ OpenAI 格式 | ❌ 单条 prompt | ❌ 单条 prompt |
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
- ❌ 无流式输出，一次性返回完整响应
- ❌ 无工具调用/结果事件
- ❌ 无消息历史注入，仅接受最后一条用户消息
- ❌ 无 Token 用量信息
- ✅ 支持 System Prompt（通过 `OPENCODE_SYSTEM_PROMPT` 环境变量）
- ✅ 支持 Model（通过 `OPENCODE_MODEL` 环境变量）

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

## 自定义 Binary Path

```go
agent, _ := claude.New(claude.Options{BinaryPath: "/usr/local/bin/claude"})
```

或在 CLI 中：

```bash
agent-wrapper run --provider claude-code --binary-path /usr/local/bin/claude "hello"
```
