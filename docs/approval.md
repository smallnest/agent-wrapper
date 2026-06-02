# 审批流程详解

## 概述

当 agent 请求调用工具时（如执行 bash 命令、读写文件），Orchestrator 会调用 `ApprovalHandler` 决定是否允许执行。

## 三种决策

| 决策 | 行为 | 场景 |
|------|------|------|
| `Allow` | 允许工具执行，等待 ToolResult | 信任 agent 的工具选择 |
| `Deny` | 拒绝执行，注入合成拒绝，agent 继续运行 | 工具不安全但允许 agent 换个方案 |
| `Abort` | 立即终止整个运行 | 工具危险且需要停止所有操作 |

## 默认行为

如果未设置 `ApprovalHandler`，所有工具调用默认允许（等同于 `--approve-all`）。

## 使用方式

### 自动允许所有

```go
orch := agentwrapper.NewOrchestrator(agent, store,
    agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
        return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
    }),
)
```

或直接不设置 handler（默认行为）。

### 按工具名称过滤

```go
agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
    // 只允许只读工具
    switch call.Name {
    case "read", "ls", "grep", "glob":
        return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
    default:
        return &agentwrapper.Decision{
            Action: agentwrapper.ActionDeny,
            Reason: "只允许只读工具",
        }, nil
    }
})
```

### 交互式审批

```go
agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
    fmt.Printf("Agent 想要执行: %s(%s)\n", call.Name, string(call.Input))
    fmt.Print("允许? (y/n/a=abort): ")

    var input string
    fmt.Scanln(&input)

    switch input {
    case "y":
        return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
    case "a":
        return &agentwrapper.Decision{Action: agentwrapper.ActionAbort, Reason: "用户中止"}, nil
    default:
        return &agentwrapper.Decision{Action: agentwrapper.ActionDeny, Reason: "用户拒绝"}, nil
    }
})
```

### 带预算检查的审批

```go
var totalTokens int
agentwrapper.WithApprovalHandler(func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
    if totalTokens > 50000 {
        return &agentwrapper.Decision{Action: agentwrapper.ActionAbort, Reason: "接近预算上限"}, nil
    }
    return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
})
```

## Deny 的行为

当工具调用被 Deny 时：

1. Orchestrator 构造合成 `ToolResult`：`"DENIED: <reason>"`，`isError: true`
2. 合成结果追加到 session 的 `turnMessages`
3. 两个事件转发给调用方：`ToolCall` + 合成 `ToolResult`
4. Agent 继续当前 turn 的运行（收到拒绝后可能选择其他方案）

## Abort 的行为

当工具调用被 Abort 时：

1. 触发 session writeback（保存已有内容）
2. 发送 `TurnEnd{StopReason: "aborted"}`
3. 事件循环终止
4. 调用方收到最后一个 aborted 事件

## CLI 使用

```bash
# 自动允许所有工具调用
agent-wrapper run --provider claude-code --approve-all "修复 bug"

# 不加 --approve-all 时，handler 为 nil（默认也是 allow）
# 注意：当前 CLI 不支持交互式审批，需要通过 Go API 实现
```

## 注意事项

- **审批时机**：对于 Claude Code 和 Codex，工具在 agent 子进程内执行。当 Orchestrator 收到 `ToolCall` 事件时，工具可能已经执行完毕。审批检查主要是通知性的。
- **双向通信**：当前 `Agent.Run()` 返回只读 channel，Orchestrator 无法将拒绝结果注入回 agent 进程。Deny 的合成 `ToolResult` 仅记录在 session 中。
