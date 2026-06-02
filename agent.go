package agentwrapper

import (
	"context"

	"github.com/smallnest/agent-wrapper/types"
)

// Agent 是所有 coding agent 后端的统一接口。
type Agent interface {
	Name() string
	Provider() types.Provider
	Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error)
	Close() error
}
