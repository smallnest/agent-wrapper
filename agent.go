package agentwrapper

import (
	"context"

	"github.com/smallnest/agent-wrapper/types"
)

// Agent 是所有 coding agent 后端的统一接口。
type Agent interface {
	Name() string
	Provider() types.Provider
	Run(ctx context.Context, input RunInput) (<-chan types.Event, error)
	Close() error
}

// RunInput 是一次 agent 调用的全部输入。
type RunInput struct {
	Session      *types.Session
	NewMessage   *types.Message
	SystemPrompt string
	WorkingDir   string
	MaxTurns     int
	AllowedTools []string
	Extra        map[string]any
}
