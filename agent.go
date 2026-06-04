package agentwrapper

import (
	"context"

	"github.com/smallnest/agent-wrapper/types"
)

// Agent is the unified interface for all coding agent backends.
type Agent interface {
	Name() string
	Provider() types.Provider
	Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error)
	Close() error
}
