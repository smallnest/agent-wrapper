package agentwrapper

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/smallnest/agent-wrapper/types"
)

func TestRetrySingleSuccess(t *testing.T) {
	retryAgent := &retryingAgent{
		numErrors: 1, errMsg: "context too long",
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "success"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
		},
	}

	comp := &countingCompressor{}
	orch := NewOrchestrator(retryAgent, WithContextCompressor(comp), WithMaxRetries(3))
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "latest",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for range out {
	}

	if retryAgent.Runs() != 2 {
		t.Errorf("expected 2 runs (1 failure + 1 success), got %d", retryAgent.Runs())
	}
	if comp.calls != 1 {
		t.Errorf("expected 1 compress call, got %d", comp.calls)
	}
}

func TestRetryExhaustion(t *testing.T) {
	retryAgent := &retryingAgent{numErrors: 10, errMsg: "too long"}

	comp := &countingCompressor{}
	orch := NewOrchestrator(retryAgent, WithContextCompressor(comp), WithMaxRetries(3))
	_, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !IsContextLengthExceeded(err) {
		t.Errorf("expected ContextLengthExceededError, got %T: %v", err, err)
	}
	if retryAgent.Runs() != 4 {
		t.Errorf("expected 4 runs, got %d", retryAgent.Runs())
	}
	if comp.calls != 3 {
		t.Errorf("expected 3 compress calls, got %d", comp.calls)
	}
}

func TestRetryNonContextErrorPassthrough(t *testing.T) {
	plainAgent := &plainErrorAgent{err: fmt.Errorf("network timeout")}

	comp := &countingCompressor{}
	orch := NewOrchestrator(plainAgent, WithContextCompressor(comp), WithMaxRetries(3))
	_, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if IsContextLengthExceeded(err) {
		t.Error("expected non-context error to pass through unwrapped")
	}
	if plainAgent.Runs() != 1 {
		t.Errorf("expected 1 run, got %d", plainAgent.Runs())
	}
	if comp.calls != 0 {
		t.Errorf("expected 0 compress calls, got %d", comp.calls)
	}
}

func TestRetryMaxRetriesZero(t *testing.T) {
	retryAgent := &retryingAgent{numErrors: 1, errMsg: "too long"}

	orch := NewOrchestrator(retryAgent, WithMaxRetries(0))
	_, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "go",
	})
	if err == nil {
		t.Fatal("expected error when maxRetries is 0")
	}
	if retryAgent.Runs() != 1 {
		t.Errorf("expected 1 run, got %d", retryAgent.Runs())
	}
}

func TestRetryDefaultCompressor(t *testing.T) {
	retryAgent := &retryingAgent{
		numErrors: 1, errMsg: "too long",
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "ok"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
		},
	}

	orch := NewOrchestrator(retryAgent)
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "latest",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range out {
	}
	if retryAgent.Runs() != 2 {
		t.Errorf("expected 2 runs, got %d", retryAgent.Runs())
	}
}

func TestRetryKeywordMatchTrigger(t *testing.T) {
	keywordAgent := &keywordErrorAgent{
		err: fmt.Errorf("request failed: context length exceeded"),
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "recovered"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
		},
	}

	comp := &countingCompressor{}
	orch := NewOrchestrator(keywordAgent, WithContextCompressor(comp), WithMaxRetries(3))
	out, err := orch.Run(context.Background(), types.RunInput{
		Prompt: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range out {
	}

	if keywordAgent.Runs() != 2 {
		t.Errorf("expected 2 runs, got %d", keywordAgent.Runs())
	}
	if comp.calls != 1 {
		t.Errorf("expected 1 compress call, got %d", comp.calls)
	}
}

func TestRunSyncSuccessRetry(t *testing.T) {
	retryAgent := &retryingAgent{
		numErrors: 1, errMsg: "too long",
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "Hello"},
			{Type: types.EventTextDelta, TextDelta: " world"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn",
				TokenUsage: &types.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
		},
	}

	orch := NewOrchestrator(retryAgent, WithMaxRetries(3))
	result, err := orch.RunSync(context.Background(), types.RunInput{
		Prompt:    "go",
		SessionID: "test-session",
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
	if result.SessionID != "test-session" {
		t.Errorf("expected 'test-session', got %q", result.SessionID)
	}
}

// --- helpers ---

type retryingAgent struct {
	numErrors int
	errMsg    string
	events    []types.Event
	runs      int
	mu        sync.Mutex
}

func (a *retryingAgent) Name() string             { return "retry-mock" }
func (a *retryingAgent) Provider() types.Provider { return "mock" }
func (a *retryingAgent) Close() error             { return nil }

func (a *retryingAgent) Run(ctx context.Context, _ types.RunInput) (<-chan types.Event, error) {
	a.mu.Lock()
	run := a.runs
	a.runs++
	a.mu.Unlock()

	if run < a.numErrors {
		return nil, &ContextLengthExceededError{Err: errors.New(a.errMsg)}
	}

	ch := make(chan types.Event, len(a.events)+1)
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

func (a *retryingAgent) Runs() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

type plainErrorAgent struct {
	err  error
	runs int
	mu   sync.Mutex
}

func (a *plainErrorAgent) Name() string             { return "plain-err" }
func (a *plainErrorAgent) Provider() types.Provider { return "mock" }
func (a *plainErrorAgent) Close() error             { return nil }

func (a *plainErrorAgent) Run(_ context.Context, _ types.RunInput) (<-chan types.Event, error) {
	a.mu.Lock()
	a.runs++
	a.mu.Unlock()
	return nil, a.err
}

func (a *plainErrorAgent) Runs() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

type keywordErrorAgent struct {
	err    error
	events []types.Event
	runs   int
	mu     sync.Mutex
}

func (a *keywordErrorAgent) Name() string             { return "kw-err" }
func (a *keywordErrorAgent) Provider() types.Provider { return "mock" }
func (a *keywordErrorAgent) Close() error             { return nil }

func (a *keywordErrorAgent) Run(ctx context.Context, _ types.RunInput) (<-chan types.Event, error) {
	a.mu.Lock()
	run := a.runs
	a.runs++
	a.mu.Unlock()

	if run == 0 {
		return nil, a.err
	}

	ch := make(chan types.Event, len(a.events)+1)
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

func (a *keywordErrorAgent) Runs() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

type countingCompressor struct {
	calls int
	mu    sync.Mutex
}

func (c *countingCompressor) Compress(msgs []types.Message) []types.Message {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if len(msgs) <= 2 {
		return msgs
	}
	return msgs[len(msgs)-2:]
}
