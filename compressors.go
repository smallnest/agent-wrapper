package agentwrapper

import (
	"fmt"
	"strings"

	"github.com/smallnest/agent-wrapper/types"
)

// SlidingWindowCompressor keeps the last N messages, optionally preserving
// a system-prompt-like first message at the head.
type SlidingWindowCompressor struct {
	RetainMessages int
}

// NewSlidingWindowCompressor creates a compressor that retains the most recent
// messages. If retain <= 0, defaults to 20.
func NewSlidingWindowCompressor(retain int) *SlidingWindowCompressor {
	if retain <= 0 {
		retain = 20
	}
	return &SlidingWindowCompressor{RetainMessages: retain}
}

func (c *SlidingWindowCompressor) Compress(msgs []types.Message) []types.Message {
	n := len(msgs)
	if n <= c.RetainMessages {
		return msgs
	}

	// Ensure we never drop below a minimum of 2 messages (last user + room
	// for assistant response).
	keep := c.RetainMessages
	if keep < 2 {
		keep = 2
	}
	if n <= keep {
		return msgs
	}

	out := make([]types.Message, 0, keep+1)

	// Preserve the first message if it looks like a system prompt: a user
	// message with substantial content at position 0. It is kept alongside
	// the window (does not consume a window slot).
	if isSystemPrompt(msgs[0]) {
		out = append(out, msgs[0])
	}

	if keep > 0 {
		out = append(out, msgs[n-keep:]...)
	}
	if len(out) < 2 && n >= 2 {
		// Should not happen with keep >= 2, but be defensive.
		return msgs[n-2:]
	}
	return out
}

// isSystemPrompt returns true if the message looks like a system prompt
// injected as a user message at position 0.
func isSystemPrompt(m types.Message) bool {
	return m.Role == types.RoleUser && len(m.Content) > 200
}

// Summarizer produces a concise summary of a set of messages.
type Summarizer func(messages []types.Message) (string, error)

// SummaryCompressor summarizes the truncated prefix and prepends the summary
// as a user message before the retained window.
type SummaryCompressor struct {
	RetainMessages int
	Summarizer     Summarizer
}

// NewSummaryCompressor creates a compressor that summarizes truncated messages.
// If summarizer is nil, a naive default is used (concatenates first 100 chars
// of each message).
func NewSummaryCompressor(retain int, summarizer Summarizer) *SummaryCompressor {
	if retain <= 0 {
		retain = 20
	}
	if summarizer == nil {
		summarizer = naiveSummarizer
	}
	return &SummaryCompressor{RetainMessages: retain, Summarizer: summarizer}
}

func (c *SummaryCompressor) Compress(msgs []types.Message) []types.Message {
	n := len(msgs)
	keep := c.RetainMessages
	if keep < 2 {
		keep = 2
	}
	if n <= keep {
		return msgs
	}

	prefix := msgs[:n-keep]
	summary, err := c.Summarizer(prefix)
	if err != nil {
		summary = fmt.Sprintf("[%d earlier messages omitted]", len(prefix))
	}

	out := make([]types.Message, 0, keep+1)
	out = append(out, types.NewUserMessage("[SUMMARY] "+summary))
	out = append(out, msgs[n-keep:]...)
	return out
}

// naiveSummarizer concatenates the first 100 chars of each message.
func naiveSummarizer(msgs []types.Message) (string, error) {
	if len(msgs) == 0 {
		return "(no earlier messages)", nil
	}
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("; ")
		}
		content := m.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		b.WriteString(strings.ReplaceAll(content, "\n", " "))
	}
	return b.String(), nil
}

// ChainedCompressor tries compressors in order and returns the output of the
// first one that actually reduces the message count.
type ChainedCompressor struct {
	compressors []ContextCompressor
}

// NewChainedCompressor creates a compressor that chains multiple strategies.
func NewChainedCompressor(compressors ...ContextCompressor) *ChainedCompressor {
	return &ChainedCompressor{compressors: compressors}
}

func (c *ChainedCompressor) Compress(msgs []types.Message) []types.Message {
	for _, comp := range c.compressors {
		out := comp.Compress(msgs)
		if len(out) < len(msgs) {
			return out
		}
	}
	return msgs
}
