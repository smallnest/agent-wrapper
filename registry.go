package agentwrapper

import (
	"fmt"
	"sort"
	"sync"

	"github.com/smallnest/agent-wrapper/types"
)

// Factory creates an Agent instance from options.
type Factory func(opts map[string]any) (Agent, error)

// entry holds a registered factory and its overwrite policy.
type entry struct {
	factory   Factory
	overwrite bool
}

// Registry manages Agent providers by name.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
}

// NewRegistry creates a Registry with the four built-in provider stubs.
func NewRegistry() *Registry {
	r := &Registry{
		entries: make(map[string]entry),
	}

	stub := func(name string) Factory {
		return func(_ map[string]any) (Agent, error) {
			return nil, fmt.Errorf("agent %q: not yet implemented", name)
		}
	}

	r.entries[string(types.ProviderClaudeCode)] = entry{factory: stub(string(types.ProviderClaudeCode))}
	r.entries[string(types.ProviderCodex)] = entry{factory: stub(string(types.ProviderCodex))}
	r.entries[string(types.ProviderPiAgent)] = entry{factory: stub(string(types.ProviderPiAgent))}
	r.entries[string(types.ProviderOpenCode)] = entry{factory: stub(string(types.ProviderOpenCode))}

	return r
}

// Register adds a provider factory. If overwrite is false and the name
// already exists, returns an error.
func (r *Registry) Register(name string, factory Factory, overwrite bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[name]; exists && !overwrite {
		return fmt.Errorf("agent %q: already registered", name)
	}

	r.entries[name] = entry{factory: factory, overwrite: overwrite}
	return nil
}

// Get creates an Agent by provider name.
func (r *Registry) Get(name string, opts map[string]any) (Agent, error) {
	r.mu.RLock()
	e, ok := r.entries[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("agent %q: not registered", name)
	}

	return e.factory(opts)
}

// List returns all registered provider names in sorted order.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Unregister removes a provider by name.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries[name]; !ok {
		return fmt.Errorf("agent %q: not registered", name)
	}

	delete(r.entries, name)
	return nil
}
