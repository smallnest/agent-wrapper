package agentwrapper

import (
	"context"
	"testing"

	"github.com/smallnest/agent-wrapper/types"
)

type stubAgent struct {
	name     string
	provider types.Provider
}

func (s *stubAgent) Name() string             { return s.name }
func (s *stubAgent) Provider() types.Provider { return s.provider }
func (s *stubAgent) Run(_ context.Context, _ types.RunInput) (<-chan types.Event, error) {
	return nil, nil
}
func (s *stubAgent) Close() error { return nil }

func TestRegistry_BuiltInProviders(t *testing.T) {
	r := NewRegistry()

	names := r.List()
	if len(names) != 7 {
		t.Fatalf("expected 7 built-in providers, got %d: %v", len(names), names)
	}

	for _, name := range []string{"claude-code", "codex", "pi-agent", "opencode", "agy", "cursor", "kimi-code"} {
		_, err := r.Get(name, nil)
		if err == nil {
			t.Errorf("expected stub error for %q, got nil", name)
		}
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	err := r.Register("test-agent", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "Test", provider: "test-agent"}, nil
	}, false)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent, err := r.Get("test-agent", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Name() != "Test" {
		t.Errorf("expected name 'Test', got %q", agent.Name())
	}
}

func TestRegistry_GetNotRegistered(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unregistered name")
	}
}

func TestRegistry_RegisterDuplicateNoOverwrite(t *testing.T) {
	r := NewRegistry()

	err := r.Register("dup", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "first"}, nil
	}, false)
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err = r.Register("dup", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "second"}, nil
	}, false)
	if err == nil {
		t.Fatal("expected error registering duplicate without overwrite")
	}

	agent, _ := r.Get("dup", nil)
	if agent.Name() != "first" {
		t.Errorf("expected first agent to remain, got %q", agent.Name())
	}
}

func TestRegistry_RegisterDuplicateWithOverwrite(t *testing.T) {
	r := NewRegistry()

	_ = r.Register("dup", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "first"}, nil
	}, false)

	err := r.Register("dup", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "second"}, nil
	}, true)
	if err != nil {
		t.Fatalf("overwrite Register: %v", err)
	}

	agent, _ := r.Get("dup", nil)
	if agent.Name() != "second" {
		t.Errorf("expected second agent after overwrite, got %q", agent.Name())
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	_ = r.Register("zebra", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "Zebra"}, nil
	}, false)
	_ = r.Register("alpha", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "Alpha"}, nil
	}, false)

	names := r.List()

	// Must include the 7 built-ins + 2 custom = 9 total
	if len(names) != 9 {
		t.Fatalf("expected 9 names, got %d: %v", len(names), names)
	}

	// Must be sorted
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("List not sorted: %v", names)
		}
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()

	_ = r.Register("temp", func(_ map[string]any) (Agent, error) {
		return &stubAgent{name: "Temp"}, nil
	}, false)

	err := r.Unregister("temp")
	if err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	_, err = r.Get("temp", nil)
	if err == nil {
		t.Fatal("expected error after unregister")
	}
}

func TestRegistry_UnregisterNotExists(t *testing.T) {
	r := NewRegistry()

	err := r.Unregister("nonexistent")
	if err == nil {
		t.Fatal("expected error unregistering nonexistent name")
	}
}
