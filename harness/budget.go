package harness

import (
	"context"

	"github.com/smallnest/agent-wrapper/types"
)

// BudgetHandler is called after each turn; returning an error terminates the run.
type BudgetHandler func(ctx context.Context, usage types.TokenUsage) error
