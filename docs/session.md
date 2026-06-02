# Session 机制详解

## 概念

Session 是跨 turn 保持的会话上下文，包含完整消息历史。Orchestrator 在每个 TurnEnd 时自动将新消息追加到 Session 并保存到 SessionStore。

## Session 结构

```go
type Session struct {
    ID        string            // UUID v4
    Messages  []Message         // 消息历史
    CreatedAt time.Time
    UpdatedAt time.Time
    Metadata  map[string]string // 自定义元数据
}
```

## 消息类型

| Role | 含义 | 关键字段 |
|------|------|---------|
| `user` | 用户消息 | `Content` |
| `assistant` | agent 回复 | `Content` |
| `tool_use` | 工具调用请求 | `ToolCallID`, `ToolName`, `ToolInput` |
| `tool_result` | 工具执行结果 | `ToolCallResultID`, `Content`, `IsToolError` |

## Session 生命周期

```
创建 → 追加用户消息 → Agent.Run() → 事件流处理 → TurnEnd 回写 → 保存
                                                              │
                              下一个 turn ←── 继续（tool_use 场景）
```

### 回写规则

每个 TurnEnd 事件触发一次性回写：

1. 累积的 assistant 文本 → `Message{Role: "assistant"}`
2. 本 turn 的 tool_use 消息 → `Message{Role: "tool_use"}`
3. 本 turn 的 tool_result 消息 → `Message{Role: "tool_result"}`
4. 调用 `SessionStore.Save()`

## SessionStore 接口

```go
type SessionStore interface {
    Create() (*types.Session, error)
    Get(id string) (*types.Session, error)
    Save(session *types.Session) error
    Delete(id string) error
    List() []*types.SessionSummary
}
```

### 内存实现

```go
store := memory.New()
session, _ := store.Create()
// ... 运行 agent ...
store.Save(session)
```

内存实现是并发安全的，使用 `sync.RWMutex` 保护。`Get()` 返回 session 的深拷贝，`Save()` 深拷贝传入的 Messages。

### 自定义实现

实现 `SessionStore` 接口即可接入 Redis、SQLite 等后端：

```go
type RedisSessionStore struct { /* ... */ }

func (s *RedisSessionStore) Create() (*types.Session, error) { /* ... */ }
func (s *RedisSessionStore) Get(id string) (*types.Session, error) { /* ... */ }
func (s *RedisSessionStore) Save(session *types.Session) error { /* ... */ }
func (s *RedisSessionStore) Delete(id string) error { /* ... */ }
func (s *RedisSessionStore) List() []*types.SessionSummary { /* ... */ }
```

## Session 恢复

通过 `--session-id` 参数可恢复已有 session 继续对话：

```go
session, _ := store.Get("existing-session-id")
orch.Run(ctx, types.RunInput{
    Session:    session,
    NewMessage: func() *types.Message { m := types.NewUserMessage("继续"); return &m }(),
})
```

注意：内存存储的 session 在进程退出后丢失。如需持久化，请实现文件或数据库存储。

## 消息累积示例

3-turn 对话后的 Session.Messages：

```
[0] user:        "分析这个项目"
[1] assistant:   "我来帮你分析..."
[2] tool_use:    bash(ls -la)           ← Turn 1 结束
[3] tool_result: file1.go file2.go
[4] assistant:   "发现两个文件..."        ← Turn 2 结束
[5] tool_use:    read(file1.go)
[6] tool_result: package main...
[7] assistant:   "分析完成。"             ← Turn 3 结束
```
