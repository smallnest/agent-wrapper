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
	store := newMockStore()
	session := types.NewSession()
	session.Messages = append(session.Messages,
		types.NewUserMessage("old1"),
		types.NewAssistantMessage("old2"),
		types.NewUserMessage("old3"),
	)

	orch := NewOrchestrator(retryAgent, store, WithContextCompressor(comp), WithMaxRetries(3))
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("latest"); return &m }(),
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
	// Latest user message must survive compression.
	found := false
	for _, m := range session.Messages {
		if m.Role == types.RoleUser && m.Content == "latest" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'latest' user message to survive compression+retry")
	}
}

func TestRetryExhaustion(t *testing.T) {
	// Always returns context-length error (numErrors=10 but maxRetries=3).
	retryAgent := &retryingAgent{numErrors: 10, errMsg: "too long"}

	comp := &countingCompressor{}
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(retryAgent, store, WithContextCompressor(comp), WithMaxRetries(3))
	_, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("go"); return &m }(),
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !IsContextLengthExceeded(err) {
		t.Errorf("expected ContextLengthExceededError, got %T: %v", err, err)
	}
	if retryAgent.Runs() != 4 { // initial + 3 retries
		t.Errorf("expected 4 runs, got %d", retryAgent.Runs())
	}
	if comp.calls != 3 {
		t.Errorf("expected 3 compress calls, got %d", comp.calls)
	}
}

func TestRetryNonContextErrorPassthrough(t *testing.T) {
	// Returns a plain (non-context-length) error.
	plainAgent := &plainErrorAgent{err: fmt.Errorf("network timeout")}

	comp := &countingCompressor{}
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(plainAgent, store, WithContextCompressor(comp), WithMaxRetries(3))
	_, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("go"); return &m }(),
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

	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(retryAgent, store, WithMaxRetries(0))
	_, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("go"); return &m }(),
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

	store := newMockStore()
	session := types.NewSession()
	for i := range 50 {
		session.Messages = append(session.Messages, types.NewUserMessage(fmt.Sprintf("msg-%d", i)))
	}

	orch := NewOrchestrator(retryAgent, store)
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("latest"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range out {
	}
	if retryAgent.Runs() != 2 {
		t.Errorf("expected 2 runs, got %d", retryAgent.Runs())
	}
	if len(session.Messages) >= 51 {
		t.Errorf("expected session messages compressed (was %d)", len(session.Messages))
	}
}

func TestRetryPreservesLatestUserMessage(t *testing.T) {
	retryAgent := &retryingAgent{
		numErrors: 1, errMsg: "too long",
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "done"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
		},
	}

	comp := &countingCompressor{}
	store := newMockStore()
	session := types.NewSession()
	session.Messages = append(session.Messages,
		types.NewUserMessage("msg-1"),
		types.NewAssistantMessage("ans-1"),
		types.NewUserMessage("msg-2"),
	)

	orch := NewOrchestrator(retryAgent, store, WithContextCompressor(comp), WithMaxRetries(3))
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("this-is-the-latest"); return &m }(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range out {
	}

	var lastUser string
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == types.RoleUser {
			lastUser = session.Messages[i].Content
			break
		}
	}
	if lastUser != "this-is-the-latest" {
		t.Errorf("expected last user message 'this-is-the-latest', got %q", lastUser)
	}
}

func TestRetryKeywordMatchTrigger(t *testing.T) {
	// Plain error with keyword triggers retry even without typed error.
	keywordAgent := &keywordErrorAgent{
		err: fmt.Errorf("request failed: context length exceeded"),
		events: []types.Event{
			{Type: types.EventTextDelta, TextDelta: "recovered"},
			{Type: types.EventTurnEnd, TurnNumber: 1, StopReason: "end_turn"},
		},
	}

	comp := &countingCompressor{}
	store := newMockStore()
	session := types.NewSession()

	orch := NewOrchestrator(keywordAgent, store, WithContextCompressor(comp), WithMaxRetries(3))
	out, err := orch.Run(context.Background(), types.RunInput{
		Session:    session,
		NewMessage: func() *types.Message { m := types.NewUserMessage("test"); return &m }(),
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

// --- helpers ---

// retryingAgent returns ContextLengthExceededError for first numErrors calls, then succeeds.
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

// plainErrorAgent always returns a plain (non-context-length) error.
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

// keywordErrorAgent returns a plain error with context-length keyword once, then succeeds.
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

// countingCompressor records how many times Compress was called.
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
