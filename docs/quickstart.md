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

    // 5. 读取事件流
    for evt := range events {
        if evt.Type == types.EventTextDelta {
            fmt.Print(evt.TextDelta)
        }
    }
}
```

## CLI 使用

```bash
# 构建
go build ./cmd/agent-wrapper

# 运行
./agent-wrapper run --provider claude-code "解释这段代码"

# 查看可用 provider
./agent-wrapper list

# 自动审批 + 预算限制
./agent-wrapper run --provider codex "修复 bug" --approve-all --budget-tokens 5000

# 查看版本
./agent-wrapper version
```

## 前置条件

使用前需安装对应的 agent CLI：

| Provider | 安装命令 |
|----------|---------|
| claude-code | `npm install -g @anthropic-ai/claude-code` |
| codex | `npm install -g @openai/codex` |
| pi-agent | `npm install -g @anthropic-ai/pi` |
| opencode | `go install github.com/opencode-ai/opencode@latest` |
