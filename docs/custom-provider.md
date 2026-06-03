# 如何编写自定义 Provider

## 概述

自定义 Provider 需实现 `Agent` 接口，并通过 `Registry` 注册。本文档以一个 Echo Agent 为例，演示完整流程。

## Agent 接口

```go
type Agent interface {
    Name() string
    Provider() types.Provider
    Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error)
    Close() error
}
```

`Run` 返回事件 channel。同步聚合由 `Orchestrator.RunSync` 提供——它调用 `Run` 然后排空 channel 返回 `*RunResult`。

## 最小实现

```go
package echo

import (
    "context"

    agentwrapper "github.com/smallnest/agent-wrapper"
    "github.com/smallnest/agent-wrapper/types"
)

type EchoAgent struct{}

func New() *EchoAgent { return &EchoAgent{} }

func (a *EchoAgent) Name() string     { return "Echo" }
func (a *EchoAgent) Provider() types.Provider { return "echo" }
func (a *EchoAgent) Close() error     { return nil }

func (a *EchoAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
    ch := make(chan types.Event, 4)

    go func() {
        defer close(ch)

        // 获取用户消息
        prompt := input.Prompt

        // 发送 TextDelta
        ch <- types.Event{Type: types.EventTextDelta, TextDelta: "Echo: " + prompt}

        // 发送 TurnEnd
        ch <- types.Event{
            Type:       types.EventTurnEnd,
            TurnNumber: 1,
            StopReason: "end_turn",
        }
    }()

    return ch, nil
}

// RegisterIn 注册到全局 Registry。
func RegisterIn(r *agentwrapper.Registry) error {
    return r.Register("echo", func(opts map[string]any) (agentwrapper.Agent, error) {
        return New(), nil
    }, true)
}
```

## 注册到 Registry

```go
registry := agentwrapper.NewRegistry()
echo.RegisterIn(registry)

agent, _ := registry.Get("echo", nil)
```

## 带子进程的 Provider

如果需要驱动外部 CLI，参考以下模式：

```go
type MyAgent struct {
    opts   Options
    binary string
    once   sync.Once
}

func (a *MyAgent) resolveBinary() (string, error) {
    if a.opts.BinaryPath != "" {
        return a.opts.BinaryPath, nil
    }
    var onceErr error
    a.once.Do(func() {
        if p, err := exec.LookPath("my-agent-cli"); err == nil {
            a.binary = p
            return
        }
        onceErr = fmt.Errorf("my-agent-cli not found in PATH")
    })
    return a.binary, onceErr
}

func (a *MyAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
    bin, err := a.resolveBinary()
    if err != nil {
        return nil, err
    }

    proc, err := process.StartProcess(ctx, process.ProcessConfig{
        Command: bin,
        Args:    []string{"--json"},
        WorkDir: input.WorkingDir,
    })
    if err != nil {
        return nil, err
    }

    // 向 stdin 写入请求...

    events := make(chan types.Event, 64)
    go func() {
        defer close(events)
        defer proc.Close()

        // 使用已有的 scanner 解析 stdout
        scanner := process.NewJSONRPCScanner(proc.Stdout())
        // 或
        // scanner := process.NewSSEScanner(proc.Stdout())

        for scanner.Scan() {
            frame := scanner.Frame()
            // 解析 frame.Data 为 Event...
        }
    }()

    return events, nil
}
```

## 可用的 Scanner

| Scanner | 用途 | 格式 |
|---------|------|------|
| `NewJSONRPCScanner(r)` | 行分隔 JSON | `{"jsonrpc":"2.0",...}\n` |
| `NewSSEScanner(r)` | Server-Sent Events | `data: {...}\n\n` |

## Convert 模式

每个 provider 通常包含一个 `convert.go` 文件，处理 `[]Message` ↔ 原生格式的转换：

```go
// convert.go

// messagesToNativeFormat 将 []Message 转换为 CLI 原生输入格式。
func messagesToNativeFormat(msgs []types.Message) []MyMessage {
    result := make([]MyMessage, 0, len(msgs))
    for _, msg := range msgs {
        switch msg.Role {
        case types.RoleUser:
            result = append(result, MyMessage{Role: "user", Content: msg.Content})
        case types.RoleAssistant:
            result = append(result, MyMessage{Role: "assistant", Content: msg.Content})
        // ... tool_use, tool_result ...
        }
    }
    return result
}
```

## 编写测试

使用 mock binary 测试子进程 provider：

```go
func mockBinary(t *testing.T, output string) string {
    t.Helper()
    dir := t.TempDir()
    script := filepath.Join(dir, "my-agent")
    content := "#!/bin/sh\necho '" + output + "'\n"
    os.WriteFile(script, []byte(content), 0o755)
    return script
}

func TestMyAgent(t *testing.T) {
    bin := mockBinary(t, `{"response":"hello"}`)
    agent := New(Options{BinaryPath: bin})

    events, err := agent.Run(context.Background(), types.RunInput{
        Session: types.NewSession(),
    })
    if err != nil {
        t.Fatalf("Run: %v", err)
    }

    for evt := range events {
        if evt.Type == types.EventTextDelta {
            // 验证输出
        }
    }
}
```

## 完整示例

参见 `examples/custom-provider/` 目录中的可运行示例。
