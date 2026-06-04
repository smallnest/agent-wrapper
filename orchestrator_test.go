package agentwrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/smallnest/agent-wrapper/harness"
	"github.com/smallnest/agent-wrapper/types"
)

// mockAgent implements Agent with a pre-defined event sequence.
type mockAgent struct {
	events []types.Event
	ch     chan types.Event
	runs   int
	mu     sync.Mutex
}

func newMockAgent(events []types.Event) *mockAgent {
	return &mockAgent{events: events}
}

func (a *mockAgent) Name() string             { return "mock" }
func (a *mockAgent) Provider() types.Provider { return "mock" }
func (a *mockAgent) Close() error             { return nil }

func (a *mockAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	a.mu.Lock()
	a.runs++
	a.mu.Unlock()

	ch := make(chan types.Event, len(a.events)+1)
	a.ch = ch
	go func() {
		defer close(ch)
		for _, evt := range a.events {
			select {
			case ch <- evt:
			case <-ctx.Done():
				ch <- types.Event{Type: types.EventError, Error: ctx.Err()}
				return
			}
		}
	}()
	return ch, nil
}

func (a *mockAgent) Runs() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

// --- Tests ---

func TestOrchestratorThreeTurnAccumulation(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "Hello"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "tool_use"},
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{"cmd":"ls"}`)},
		{Type: types.EventToolResult, ToolResultID: "c1", ToolResultOutput: "file1.go"},
		{Type: types.EventTurnEnd, TurnNumber: 2, StopReason: "tool_use"},
		{Type: types.EventToolCall, ToolCallID: "c2", ToolName: "read", ToolInput: json.RawMessage(`{"path":"file1.go"}`)},
		{Type: types.EventToolResult, ToolResultID: "c2", ToolResultOutput: "package main"},
		{Type: types.EventTextDelta, TextDelta: " Done."},
		{Type: types.EventTurnEnd, TurnNumber: 3, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turnEnds int
	for range out {
		turnEnds++
	}

	// 9 events total (3 turn ends + text deltas + tool calls + tool results).
	if turnEnds != 9 {
		t.Errorf("expected 9 events, got %d", turnEnds)
	}
}

func TestOrchestratorApprovalAllow(t *testing.T) {
	events := []types.Event{
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventToolResult, ToolResultID: "c1", ToolResultOutput: "output"},
		{Type: types.EventTextDelta, TextDelta: "done"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)

	var approvedTool string
	orch := NewOrchestrator(agent, WithApprovalHandler(func(ctx context.Context, call harness.ToolCall) (*harness.Decision, error) {
		approvedTool = call.Name
		return &harness.Decision{Action: harness.ActionAllow}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotToolResult bool
	for evt := range out {
		if evt.Type == types.EventToolResult && evt.ToolResultID == "c1" {
			gotToolResult = true
		}
	}

	if approvedTool != "bash" {
		t.Errorf("expected approval for 'bash', got %q", approvedTool)
	}
	if !gotToolResult {
		t.Error("expected tool_result event for allowed tool call")
	}
}

func TestOrchestratorApprovalDeny(t *testing.T) {
	events := []types.Event{
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventTextDelta, TextDelta: "ok"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent, WithApprovalHandler(func(ctx context.Context, call harness.ToolCall) (*harness.Decision, error) {
		return &harness.Decision{Action: harness.ActionDeny, Reason: "not allowed"}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotSyntheticResult bool
	for evt := range out {
		if evt.Type == types.EventToolResult && evt.ToolResultID == "c1" && evt.ToolResultError {
			gotSyntheticResult = true
			if evt.ToolResultOutput != "DENIED: not allowed" {
				t.Errorf("expected 'DENIED: not allowed', got %q", evt.ToolResultOutput)
			}
		}
	}

	if !gotSyntheticResult {
		t.Error("expected synthetic tool_result for denied tool call")
	}
}

func TestOrchestratorApprovalAbort(t *testing.T) {
	events := []types.Event{
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventTextDelta, TextDelta: "should not reach"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent, WithApprovalHandler(func(ctx context.Context, call harness.ToolCall) (*harness.Decision, error) {
		return &harness.Decision{Action: harness.ActionAbort, Reason: "user cancelled"}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotAborted bool
	var gotPostAbortText bool
	for evt := range out {
		if evt.Type == types.EventTurnEnd && evt.StopReason == "aborted" {
			gotAborted = true
		}
		if evt.Type == types.EventTextDelta {
			gotPostAbortText = true
		}
	}

	if !gotAborted {
		t.Error("expected turn_end with stopReason 'aborted'")
	}
	if gotPostAbortText {
		t.Error("should not receive text events after abort")
	}
}

func TestOrchestratorBudgetExceeded(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "hi"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "tool_use",
			TokenUsage: &types.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}},
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventToolResult, ToolResultID: "c1", ToolResultOutput: "out"},
		{Type: types.EventTextDelta, TextDelta: " should not reach"},
		{Type: types.EventTurnEnd, TurnNumber: 2, StopReason: "end_turn",
			TokenUsage: &types.TokenUsage{InputTokens: 200, OutputTokens: 100, TotalTokens: 300}},
	}

	agent := newMockAgent(events)
	callCount := 0
	orch := NewOrchestrator(agent, WithBudgetHandler(func(ctx context.Context, usage types.TokenUsage) error {
		callCount++
		if usage.TotalTokens > 200 {
			return fmt.Errorf("budget exceeded: used %d tokens", usage.TotalTokens)
		}
		return nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range out {
	}
	if callCount != 2 {
		t.Errorf("expected 2 budget handler calls, got %d", callCount)
	}
}

func TestOrchestratorMaxTurns(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "turn1"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "tool_use"},
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventToolResult, ToolResultID: "c1", ToolResultOutput: "out"},
		{Type: types.EventTextDelta, TextDelta: "turn2"},
		{Type: types.EventTurnEnd, TurnNumber: 2, StopReason: "tool_use"},
		{Type: types.EventToolCall, ToolCallID: "c2", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventToolResult, ToolResultID: "c2", ToolResultOutput: "out"},
		{Type: types.EventTextDelta, TextDelta: "turn3"},
		{Type: types.EventTurnEnd, TurnNumber: 3, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt:   "run",
		MaxTurns: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turnEnds int
	for evt := range out {
		if evt.Type == types.EventTurnEnd {
			turnEnds++
		}
	}
	if turnEnds != 2 {
		t.Errorf("expected 2 turn_end events, got %d", turnEnds)
	}
}

func TestOrchestratorContextCancellation(t *testing.T) {
	slowEvents := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "start"},
	}

	agent := newMockAgent(slowEvents)
	ctx, cancel := context.WithCancel(context.Background())

	orch := NewOrchestrator(agent)
	out, err := orch.Run(ctx, types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	time.AfterFunc(100*time.Millisecond, cancel)

	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("event channel did not close after context cancellation")
		}
	}
}

func TestOrchestratorDefaultApprovalAllowAll(t *testing.T) {
	events := []types.Event{
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventToolResult, ToolResultID: "c1", ToolResultOutput: "ok"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotToolCall, gotToolResult bool
	for evt := range out {
		if evt.Type == types.EventToolCall && evt.ToolCallID == "c1" {
			gotToolCall = true
		}
		if evt.Type == types.EventToolResult && evt.ToolResultID == "c1" {
			gotToolResult = true
		}
	}

	if !gotToolCall {
		t.Error("expected tool_call event forwarded")
	}
	if !gotToolResult {
		t.Error("expected tool_result event forwarded")
	}
}

func TestOrchestratorNilPrompt(t *testing.T) {
	agent := newMockAgent(nil)
	orch := NewOrchestrator(agent)

	_, err := orch.Run(context.Background(), types.RunInput{Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestOrchestratorErrorEvent(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "starting"},
		{Type: types.EventError, Error: fmt.Errorf("agent crashed")},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "run",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotError bool
	for evt := range out {
		if evt.Type == types.EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error event")
	}
}

func TestRunSyncSuccess(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "Hello"},
		{Type: types.EventTextDelta, TextDelta: " world"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn",
			TokenUsage: &types.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	result, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if result.Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result.Text)
	}
	if result.Usage == nil {
		t.Fatal("expected non-nil Usage")
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", result.Usage.TotalTokens)
	}
}

func TestRunSyncError(t *testing.T) {
	events := []types.Event{
		{Type: types.EventError, Error: fmt.Errorf("agent crashed")},
	}

	agent := newMockAgent(events)
	orch := NewOrchestrator(agent)
	result, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err == nil {
		t.Fatal("expected error from RunSync")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestRunSyncNilPrompt(t *testing.T) {
	agent := newMockAgent(nil)
	orch := NewOrchestrator(agent)

	_, err := orch.RunSync(context.Background(), types.RunInput{Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}
