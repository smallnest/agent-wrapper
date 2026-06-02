package opencode

import (
	"github.com/smallnest/agent-wrapper/types"
)

// messagesToPrompt extracts the user prompt from the session messages for the
// opencode non-interactive mode. OpenCode -p accepts a single prompt string;
// we send the last user message as the prompt.
func messagesToPrompt(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
