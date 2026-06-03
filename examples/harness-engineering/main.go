// harness-engineering 演示调用 agent runtime 失败后的恢复处理。
//
// 场景：
//  1. 上下文超限 → 自动压缩 + 重试（内置 retry loop）
//  2. 网络超时/其他错误 → 不重试，直接返回错误
//  3. 重试耗尽 → 最终返回错误
//
// 使用方法:
//
//	go run main.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/agents/claude"
	"github.com/smallnest/agent-wrapper/types"
)

func main() {
	registry := agentwrapper.NewRegistry()
	_ = claude.RegisterIn(registry)

	// --- Example 1: Default — retry + compression built-in ---
	fmt.Println("=== Example 1: Default retry+compression ===")
	runWithDefaultRetry(registry)

	// --- Example 2: Custom compression strategy ---
	fmt.Println("\n=== Example 2: Custom compressor ===")
	runWithCustomCompressor(registry)

	// --- Example 3: Disable retry ---
	fmt.Println("\n=== Example 3: MaxRetries=0 (no retry) ===")
	runWithNoRetry(registry)

	// --- Example 4: Handle errors from RunSync ---
	fmt.Println("\n=== Example 4: Error handling with RunSync ===")
	runSyncWithErrorHandling(registry)
}

// Default orchestrator already has 3 retries + ChainedCompressor built-in.
func runWithDefaultRetry(registry *agentwrapper.Registry) {
	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Printf("get agent: %v\n", err)
		return
	}

	orch := agentwrapper.NewOrchestrator(agent) // defaults: 3 retries, chained compressor

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	events, err := orch.Run(ctx, types.RunInput{
		Prompt: "explain what a harness is in engineering",
	})
	if err != nil {
		if agentwrapper.IsContextLengthExceeded(err) {
			fmt.Printf("[retry exhausted] context too long even after compression: %v\n", err)
		} else {
			fmt.Printf("[unrecoverable] %v\n", err)
		}
		return
	}

	for evt := range events {
		if evt.Type == types.EventTextDelta {
			fmt.Print(evt.TextDelta)
		}
		if evt.Type == types.EventError {
			fmt.Fprintf(os.Stderr, "[error] %v\n", evt.Error)
		}
	}
	fmt.Println()
}

// Inject a custom compressor that minimizes more aggressively.
func runWithCustomCompressor(registry *agentwrapper.Registry) {
	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Printf("get agent: %v\n", err)
		return
	}

	// Aggressive: only keep last 5 messages on retry.
	comp := agentwrapper.NewSlidingWindowCompressor(5)

	orch := agentwrapper.NewOrchestrator(agent,
		agentwrapper.WithContextCompressor(comp),
		agentwrapper.WithMaxRetries(2),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := orch.RunSync(ctx, types.RunInput{
		Prompt: "explain engineering harness patterns",
	})
	if err != nil {
		if agentwrapper.IsContextLengthExceeded(err) {
			fmt.Printf("[retry exhausted] %v\n", err)
		} else {
			fmt.Printf("[error] %v\n", err)
		}
		return
	}

	fmt.Printf("Text: %s\n", truncStr(result.Text, 200))
	fmt.Printf("Session: %s\n", result.SessionID)
}

// Disable retry entirely — first error is returned immediately.
func runWithNoRetry(registry *agentwrapper.Registry) {
	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Printf("get agent: %v\n", err)
		return
	}

	orch := agentwrapper.NewOrchestrator(agent, agentwrapper.WithMaxRetries(0))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	events, err := orch.Run(ctx, types.RunInput{
		Prompt: "explain something very long " + strings.Repeat("with lots of context ", 100),
	})
	if err != nil {
		fmt.Printf("[error - no retry] %v\n", err)
		return
	}

	for evt := range events {
		if evt.Type == types.EventTextDelta {
			fmt.Print(evt.TextDelta)
		}
	}
	fmt.Println()
}

// RunSync error handling: check typed error, extract info.
func runSyncWithErrorHandling(registry *agentwrapper.Registry) {
	agent, err := registry.Get("claude-code", nil)
	if err != nil {
		fmt.Printf("get agent: %v\n", err)
		return
	}

	orch := agentwrapper.NewOrchestrator(agent)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := orch.RunSync(ctx, types.RunInput{
		Prompt: "tell me about harness engineering",
	})
	if err != nil {
		var ce *agentwrapper.ContextLengthExceededError
		switch {
		case agentwrapper.IsContextLengthExceeded(err):
			fmt.Printf("[context exceeded] %v\n", err)
			fmt.Println("Consider reducing input size or using a model with larger context window.")
		case strings.Contains(err.Error(), "timeout"):
			fmt.Printf("[timeout] %v\n", err)
			fmt.Println("Network issue — retry later or check connectivity.")
		default:
			fmt.Printf("[unhandled] %v\n", err)
		}
		_ = ce // suppress unused
		return
	}

	fmt.Printf("Success!\n")
	fmt.Printf("Text: %s\n", truncStr(result.Text, 100))
	fmt.Printf("SessionID: %s\n", result.SessionID)
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
