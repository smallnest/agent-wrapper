package harness

import (
	"context"

	"github.com/smallnest/agent-wrapper/types"
)

// BudgetHandler 在每个 turn 结束后被调用，返回 error 时终止运行。
type BudgetHandler func(ctx context.Context, usage types.TokenUsage) error
