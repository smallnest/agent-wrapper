package types

import (
	"fmt"
	"time"
)

// ExitError 表示 agent CLI 进程异常退出。
type ExitError struct {
	ExitCode int
	Stderr   string
	Command  string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("agent '%s' exited with code %d: %s", e.Command, e.ExitCode, e.Stderr)
}

// ProtocolError 表示无法解析 agent CLI 的输出。
type ProtocolError struct {
	Provider    Provider
	RawBytes    []byte
	Description string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("failed to parse agent output from '%s': %s (raw: %s)",
		e.Provider, e.Description, string(e.RawBytes))
}

// BudgetExceededError 表示 token 预算耗尽。
type BudgetExceededError struct {
	Used  int
	Limit int
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("token budget exceeded: used %d of %d", e.Used, e.Limit)
}

// SessionNotFoundError 表示 session ID 不存在。
type SessionNotFoundError struct {
	ID string
}

func (e *SessionNotFoundError) Error() string {
	return fmt.Sprintf("session '%s' not found", e.ID)
}

// TimeoutError 表示 agent 运行超时。
type TimeoutError struct {
	Duration time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("agent run timed out after %s", e.Duration)
}
