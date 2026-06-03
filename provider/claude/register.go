package claude

import agentwrapper "github.com/smallnest/agent-wrapper"

// RegisterIn replaces the claude-code stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("claude-code", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}
