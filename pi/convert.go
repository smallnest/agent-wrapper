package pi

import (
	"github.com/smallnest/agent-wrapper/types"
)

// messagesToPrompt extracts the last user message from the session for the
// pi prompt command. Pi's RPC mode accepts a single message string per prompt.
func messagesToPrompt(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
