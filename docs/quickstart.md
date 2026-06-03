# 快速开始

5 分钟上手 agent-wrapper。

## 安装

```bash
go get github.com/smallnest/agent-wrapper
```

## 最简示例

```go
package main

import (
    "context"
    "fmt"

    agentwrapper "github.com/smallnest/agent-wrapper"
    "github.com/smallnest/agent-wrapper/claude"
    "github.com/smallnest/agent-wrapper/sessionstore/memory"
    "github.com/smallnest/agent-wrapper/types"
)

func main() {
    // 1. 注册 provider
    registry := agentwrapper.NewRegistry()
    claude.RegisterIn(registry)

    // 2. 创建 agent
    agent, _ := registry.Get("claude-code", nil)

    // 3. 创建 session
    store := memory.New()
    session, _ := store.Create()

    // 4. 运行
    orch := agentwrapper.NewOrchestrator(agent, store)
    events, _ := orch.Run(context.Background(), types.RunInput{
        Session:    session,
        NewMessage: func() *types.Message { m := types.NewUserMessage("你好"); return &m }(),
    })

    // 5. 读取事件流（也可用 RunSync 直接获取聚合结果）
    for evt := range events {
        if evt.Type == types.EventTextDelta {
            fmt.Print(evt.TextDelta)
        }
    }

    // 或使用同步 API：
    // result, _ := orch.RunSync(ctx, types.RunInput{...})
    // fmt.Println(result.Text)
}
```

## CLI 使用

```bash
# 构建
go build ./cmd/agent-wrapper

# 默认流式输出（文本→stdout，元数据→stderr）
./agent-wrapper run --provider claude-code "解释这段代码"

# JSON 聚合输出（适合脚本/CI）
./agent-wrapper run --provider claude-code "hello" --json

# NDJSON 流式输出（适合管道处理）
./agent-wrapper run --provider claude-code "hello" --json --stream | jq .

# 查看可用 provider
./agent-wrapper list

# 自动审批 + 预算限制
./agent-wrapper run --provider codex "修复 bug" --approve-all --budget-tokens 5000

# 查看版本
./agent-wrapper version
```

### 输出格式

| Flags | 模式 | 描述 |
|-------|------|------|
| (默认) | stream | 文本增量输出到 stdout，工具调用/元数据到 stderr |
| `--json` | aggregated JSON | 运行结束后输出单个 `{"text","usage","session_id"}` 对象 |
| `--json --stream` | stream-json (NDJSON) | 每个事件序列化为一行 JSON，全部输出到 stdout |

## 前置条件

使用前需安装对应的 agent CLI：

| Provider | 安装命令 |
|----------|---------|
| claude-code | `npm install -g @anthropic-ai/claude-code` |
| codex | `npm install -g @openai/codex` |
| pi-agent | `npm install -g @anthropic-ai/pi` |
| opencode | `go install github.com/opencode-ai/opencode@latest` |
