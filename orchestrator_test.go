package agentwrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

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

// mockStore implements SessionStore in memory.
type mockStore struct {
	sessions map[string]*types.Session
	saves    int
}

func newMockStore() *mockStore {
	return &mockStore{sessions: make(map[string]*types.Session)}
}

func (s *mockStore) Create() (*types.Session, error) {
	sess := types.NewSession()
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *mockStore) Get(id string) (*types.Session, error) {
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return sess, nil
}

func (s *mockStore) Save(session *types.Session) error {
	s.saves++
	s.sessions[session.ID] = session
	return nil
}

func (s *mockStore) Delete(id string) error {
	delete(s.sessions, id)
	return nil
}

func (s *mockStore) List() []*types.SessionSummary {
	return nil
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
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("go"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turnEnds int
	for range out {
		turnEnds++
	}

	// Session should have accumulated messages across all turns.
	// Turn 1: assistant "Hello" + tool_use c1 + tool_result c1
	// Turn 2: tool_use c2 + tool_result c2
	// Turn 3: assistant " Done."
	// Total: 1 user + 1 assistant + 2 tool_use + 2 tool_result + 1 assistant = 7
	msgs := session.Messages
	if len(msgs) != 7 {
		t.Errorf("expected 7 session messages, got %d", len(msgs))
	}

	// First message should be the user message.
	if msgs[0].Role != types.RoleUser || msgs[0].Content != "go" {
		t.Errorf("first message: expected user 'go', got role=%v content=%q", msgs[0].Role, msgs[0].Content)
	}

	// Second message should be assistant from turn 1.
	if msgs[1].Role != types.RoleAssistant || msgs[1].Content != "Hello" {
		t.Errorf("second message: expected assistant 'Hello', got role=%v content=%q", msgs[1].Role, msgs[1].Content)
	}

	// Store should have been saved (once per turn = 3).
	if store.saves != 3 {
		t.Errorf("expected 3 saves, got %d", store.saves)
	}
}

func TestOrchestratorTwoRunsSameSession(t *testing.T) {
	// First run events.
	events1 := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "First answer"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}
	// Second run events.
	events2 := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "Second answer"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	store := newMockStore()
	session := types.NewSession()
	countAfterFirst := 0

	// First run.
	agent1 := newMockAgent(events1)
	orch1 := NewOrchestrator(agent1, store)
	out1, err := orch1.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("q1"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	for range out1 {
	}
	countAfterFirst = len(session.Messages)

	// Second run with a new agent instance (different events).
	agent2 := newMockAgent(events2)
	orch2 := NewOrchestrator(agent2, store)
	out2, err := orch2.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("q2"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	for range out2 {
	}

	// Second run's session should include first run's messages.
	msgs := session.Messages
	if len(msgs) <= countAfterFirst {
		t.Errorf("expected more messages after second run: had %d, now %d", countAfterFirst, len(msgs))
	}

	// First message should be "q1" from run 1.
	if msgs[0].Content != "q1" {
		t.Errorf("expected first message 'q1', got %q", msgs[0].Content)
	}

	// Should find "First answer" somewhere in messages.
	found := false
	for _, m := range msgs {
		if m.Content == "First answer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'First answer' in session messages from run 1")
	}

	// Should find "Second answer" in messages.
	found = false
	for _, m := range msgs {
		if m.Content == "Second answer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Second answer' in session messages from run 2")
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
	store := newMockStore()
	session := types.NewSession()

	var approvedTool string
	orch := NewOrchestrator(agent, store, WithApprovalHandler(func(ctx context.Context, call ToolCall) (*Decision, error) {
		approvedTool = call.Name
		return &Decision{Action: ActionAllow}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
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
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(agent, store, WithApprovalHandler(func(ctx context.Context, call ToolCall) (*Decision, error) {
		return &Decision{Action: ActionDeny, Reason: "not allowed"}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
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

	// Session should contain the denied tool_use + synthetic tool_result.
	msgs := session.Messages
	var hasDenied bool
	for _, m := range msgs {
		if m.Role == types.RoleToolResult && m.IsToolError && m.Content == "DENIED: not allowed" {
			hasDenied = true
		}
	}
	if !hasDenied {
		t.Error("expected denied tool_result in session messages")
	}
}

func TestOrchestratorApprovalAbort(t *testing.T) {
	events := []types.Event{
		{Type: types.EventToolCall, ToolCallID: "c1", ToolName: "bash", ToolInput: json.RawMessage(`{}`)},
		{Type: types.EventTextDelta, TextDelta: "should not reach"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(agent, store, WithApprovalHandler(func(ctx context.Context, call ToolCall) (*Decision, error) {
		return &Decision{Action: ActionAbort, Reason: "user cancelled"}, nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
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
	store := newMockStore()
	session := types.NewSession()

	callCount := 0
	orch := NewOrchestrator(agent, store, WithBudgetHandler(func(ctx context.Context, usage types.TokenUsage) error {
		callCount++
		if usage.TotalTokens > 200 {
			return fmt.Errorf("budget exceeded: used %d tokens", usage.TotalTokens)
		}
		return nil
	}))

	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Drain events — orchestrator stops after budget check fails.
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
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
		MaxTurns:   2,
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
		t.Errorf("expected 2 turn_end events with MaxTurns=2, got %d", turnEnds)
	}
}

func TestOrchestratorContextCancellation(t *testing.T) {
	// Agent that produces events slowly.
	slowEvents := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "start"},
	}

	agent := newMockAgent(slowEvents)
	store := newMockStore()
	session := types.NewSession()

	ctx, cancel := context.WithCancel(context.Background())

	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(ctx, types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Cancel after a short delay.
	time.AfterFunc(100*time.Millisecond, cancel)

	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return // channel closed — expected
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
	store := newMockStore()
	session := types.NewSession()

	// No approval handler set — should default to allow.
	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
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

func TestOrchestratorNilSession(t *testing.T) {
	agent := newMockAgent(nil)
	store := newMockStore()
	orch := NewOrchestrator(agent, store)

	_, err := orch.Run(context.Background(), types.RunInput{Session: nil})
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestOrchestratorErrorEvent(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "starting"},
		{Type: types.EventError, Error: fmt.Errorf("agent crashed")},
	}

	agent := newMockAgent(events)
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("run"); return &m }(),
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

	// Session should still have the accumulated text.
	var hasText bool
	for _, m := range session.Messages {
		if m.Role == types.RoleAssistant && m.Content == "starting" {
			hasText = true
		}
	}
	if !hasText {
		t.Error("expected 'starting' assistant message in session after error")
	}
}

func TestOrchestratorNoNewMessage(t *testing.T) {
	events := []types.Event{
		{Type: types.EventTextDelta, TextDelta: "response"},
		{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
	}

	agent := newMockAgent(events)
	store := newMockStore()
	session := types.NewSession()
	session.Messages = append(session.Messages, types.NewUserMessage("existing"))

	orch := NewOrchestrator(agent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session: session,
		// No NewMessage
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for range out {
	}

	// Session should still have the original user message + new assistant.
	msgs := session.Messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "existing" {
		t.Errorf("expected original 'existing' message, got %q", msgs[0].Content)
	}
	if msgs[1].Role != types.RoleAssistant || msgs[1].Content != "response" {
		t.Errorf("expected assistant 'response', got role=%v content=%q", msgs[1].Role, msgs[1].Content)
	}
}
