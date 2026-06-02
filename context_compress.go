package agentwrapper

import (
	"errors"
	"strings"

	"github.com/smallnest/agent-wrapper/types"
)

// ContextLengthExceededError indicates the LLM rejected a request because
// the context (message history) exceeded its maximum token limit.
type ContextLengthExceededError struct {
	Err error
}

func (e *ContextLengthExceededError) Error() string {
	return "context length exceeded: " + e.Err.Error()
}

func (e *ContextLengthExceededError) Unwrap() error {
	return e.Err
}

// contextLengthKeywords are substrings commonly found in context-length error
// messages from different LLM providers.
var contextLengthKeywords = []string{
	"context length",
	"token limit",
	"too long",
	"context_length_exceeded",
	"max_tokens",
}

// IsContextLengthExceeded returns true if err is a *ContextLengthExceededError
// or if the error message contains a known context-length keyword.
func IsContextLengthExceeded(err error) bool {
	var ce *ContextLengthExceededError
	if errors.As(err, &ce) {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range contextLengthKeywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// ContextCompressor compresses message history to reduce token usage.
type ContextCompressor interface {
	Compress(messages []types.Message) []types.Message
}
