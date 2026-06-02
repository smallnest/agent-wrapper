package memory

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/smallnest/agent-wrapper/types"
)

func TestCreateAndGet(t *testing.T) {
	store := New()

	s, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if len(s.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(s.Messages))
	}

	got, err := store.Get(s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("expected ID %s, got %s", s.ID, got.ID)
	}
}

func TestCreateSaveGet(t *testing.T) {
	store := New()

	s, _ := store.Create()
	s.Messages = append(s.Messages, types.NewUserMessage("hello"))
	s.Messages = append(s.Messages, types.NewAssistantMessage("world"))

	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, _ := store.Get(s.ID)
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Content != "hello" {
		t.Errorf("msg 0: expected 'hello', got %q", got.Messages[0].Content)
	}
	if got.Messages[1].Content != "world" {
		t.Errorf("msg 1: expected 'world', got %q", got.Messages[1].Content)
	}
}

func TestGetNotFound(t *testing.T) {
	store := New()

	_, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}

	var snf *types.SessionNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("expected *SessionNotFoundError, got %T: %v", err, err)
	}
	if snf.ID != "nonexistent" {
		t.Errorf("expected ID 'nonexistent', got %q", snf.ID)
	}
}

func TestSaveNotFound(t *testing.T) {
	store := New()

	fake := &types.Session{ID: "fake"}
	err := store.Save(fake)
	if err == nil {
		t.Fatal("expected error saving nonexistent session")
	}

	var snf *types.SessionNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("expected *SessionNotFoundError, got %T", err)
	}
}

func TestDelete(t *testing.T) {
	store := New()

	s, _ := store.Create()
	_ = store.Delete(s.ID)

	_, err := store.Get(s.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}

	var snf *types.SessionNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("expected *SessionNotFoundError, got %T", err)
	}
}

func TestList(t *testing.T) {
	store := New()

	s1, _ := store.Create()
	s1.Messages = append(s1.Messages, types.NewUserMessage("a"))
	_ = store.Save(s1)

	s2, _ := store.Create()
	s2.Messages = append(s2.Messages, types.NewUserMessage("b"))
	s2.Messages = append(s2.Messages, types.NewUserMessage("c"))
	_ = store.Save(s2)

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(list))
	}

	byID := map[string]*types.SessionSummary{}
	for _, sum := range list {
		byID[sum.ID] = sum
	}

	if byID[s1.ID].MessageCount != 1 {
		t.Errorf("s1: expected 1 message, got %d", byID[s1.ID].MessageCount)
	}
	if byID[s2.ID].MessageCount != 2 {
		t.Errorf("s2: expected 2 messages, got %d", byID[s2.ID].MessageCount)
	}
}

func TestSaveUpdatesUpdatedAt(t *testing.T) {
	store := New()

	s, _ := store.Create()
	original := s.UpdatedAt

	s.Messages = append(s.Messages, types.NewUserMessage("hi"))
	_ = store.Save(s)

	got, _ := store.Get(s.ID)
	if !got.UpdatedAt.After(original) {
		t.Errorf("expected UpdatedAt to advance: before=%v after=%v", original, got.UpdatedAt)
	}
}

func TestConcurrentSaves(t *testing.T) {
	store := New()

	// 10 sessions, each goroutine does 100 appends via Get-Append-Save
	const goroutines = 10
	const appendsPer = 100
	sessions := make([]*types.Session, goroutines)
	for i := range sessions {
		s, _ := store.Create()
		sessions[i] = s
	}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			id := sessions[i].ID
			for j := range appendsPer {
				current, _ := store.Get(id)
				current.Messages = append(current.Messages, types.NewUserMessage(fmt.Sprintf("msg-%d", j)))
				_ = store.Save(current)
			}
		})
	}
	wg.Wait()

	for i, s := range sessions {
		got, _ := store.Get(s.ID)
		if len(got.Messages) != appendsPer {
			t.Errorf("session %d: expected %d messages, got %d", i, appendsPer, len(got.Messages))
		}
	}
}

func TestConcurrentSavesSameSession(t *testing.T) {
	store := New()
	s, _ := store.Create()

	// 10 goroutines concurrently saving to the same session.
	// Verifies no panics, no data races. Last write wins.
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			for j := range 100 {
				_ = store.Save(&types.Session{
					ID:       s.ID,
					Messages: []types.Message{types.NewUserMessage(fmt.Sprintf("g%d-m%d", i, j))},
				})
			}
		})
	}
	wg.Wait()

	got, _ := store.Get(s.ID)
	if len(got.Messages) != 1 {
		t.Errorf("expected 1 message (last write wins), got %d", len(got.Messages))
	}
}

func TestGetReturnsCopy(t *testing.T) {
	store := New()

	s, _ := store.Create()
	_ = store.Save(s)

	got1, _ := store.Get(s.ID)
	got2, _ := store.Get(s.ID)

	got1.Messages = append(got1.Messages, types.NewUserMessage("mutated"))

	if len(got2.Messages) != 0 {
		t.Error("modifying one Get result should not affect another")
	}
}
