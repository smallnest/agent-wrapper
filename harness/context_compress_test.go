package harness

import (
	"errors"
	"fmt"
	"testing"

	"github.com/smallnest/agent-wrapper/types"
)

func TestContextLengthExceededError_ImplementsError(t *testing.T) {
	err := &ContextLengthExceededError{Err: errors.New("boom")}
	_ = error(err)
}

func TestContextLengthExceededError_Error(t *testing.T) {
	err := &ContextLengthExceededError{Err: errors.New("token limit exceeded")}
	got := err.Error()
	if got != "context length exceeded: token limit exceeded" {
		t.Errorf("Error(): got %q", got)
	}
}

func TestContextLengthExceededError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := &ContextLengthExceededError{Err: inner}
	if !errors.Is(err, inner) {
		t.Error("expected errors.Is to find inner")
	}
}

func TestContextLengthExceededError_As(t *testing.T) {
	err := &ContextLengthExceededError{Err: errors.New("x")}
	var ce *ContextLengthExceededError
	if !errors.As(err, &ce) {
		t.Error("expected errors.As to succeed")
	}
}

func TestIsContextLengthExceeded_TypedError(t *testing.T) {
	err := &ContextLengthExceededError{Err: errors.New("some cause")}
	if !IsContextLengthExceeded(err) {
		t.Error("expected true for typed error")
	}
}

func TestIsContextLengthExceeded_KeywordMatch(t *testing.T) {
	for _, msg := range []string{
		"request failed: context length exceeded, max 200000 tokens",
		"token limit hit",
		"input too long for model",
		"context_length_exceeded: please reduce messages",
		"max_tokens exceeded",
	} {
		t.Run(msg, func(t *testing.T) {
			if !IsContextLengthExceeded(errors.New(msg)) {
				t.Errorf("expected true for keyword-matched: %q", msg)
			}
		})
	}
}

func TestIsContextLengthExceeded_UnrelatedError(t *testing.T) {
	if IsContextLengthExceeded(errors.New("network timeout")) {
		t.Error("expected false for unrelated error")
	}
	if IsContextLengthExceeded(errors.New("authentication failed")) {
		t.Error("expected false for unrelated error")
	}
}

func TestIsContextLengthExceeded_Nil(t *testing.T) {
	if IsContextLengthExceeded(nil) {
		t.Error("expected false for nil")
	}
}

func TestIsContextLengthExceeded_WrappedKeyword(t *testing.T) {
	inner := errors.New("context length exceeded: too many tokens")
	wrapped := fmt.Errorf("agent run failed: %w", inner)
	if !IsContextLengthExceeded(wrapped) {
		t.Error("expected true for wrapped error with keyword")
	}
}

func TestIsContextLengthExceeded_CaseInsensitive(t *testing.T) {
	err := errors.New("CONTEXT LENGTH EXCEEDED: MAX TOKENS")
	if !IsContextLengthExceeded(err) {
		t.Error("expected true for uppercase keyword")
	}
}

type stubCompressor struct{}

func (s stubCompressor) Compress(msgs []types.Message) []types.Message {
	return msgs
}

func TestStubCompressor_SatisfiesInterface(t *testing.T) {
	var _ ContextCompressor = stubCompressor{}
}

func TestWrapIfContextExceeded_Nil(t *testing.T) {
	if err := WrapIfContextExceeded(nil, ""); err != nil {
		t.Errorf("expected nil for nil input, got %v", err)
	}
}

func TestWrapIfContextExceeded_ErrorMatches(t *testing.T) {
	err := errors.New("context length exceeded: too many tokens")
	wrapped := WrapIfContextExceeded(err, "")
	if !IsContextLengthExceeded(wrapped) {
		t.Error("expected wrapped error to be detected")
	}
	var ce *ContextLengthExceededError
	if !errors.As(wrapped, &ce) {
		t.Error("expected *ContextLengthExceededError")
	}
	if ce.Err != err {
		t.Error("expected wrapped inner to be original error")
	}
}

func TestWrapIfContextExceeded_StderrMatches(t *testing.T) {
	err := errors.New("request failed with exit code 1")
	stderr := "Error: context length exceeded. Maximum 200000 tokens allowed."
	wrapped := WrapIfContextExceeded(err, stderr)
	if !IsContextLengthExceeded(wrapped) {
		t.Error("expected detection via stderr keyword match")
	}
	var ce *ContextLengthExceededError
	if !errors.As(wrapped, &ce) {
		t.Error("expected *ContextLengthExceededError")
	}
}

func TestWrapIfContextExceeded_NoMatch(t *testing.T) {
	err := errors.New("network timeout")
	wrapped := WrapIfContextExceeded(err, "")
	if wrapped != err {
		t.Errorf("expected original error returned unchanged, got %T", wrapped)
	}
}

func TestWrapIfContextExceeded_EmptyStderr(t *testing.T) {
	err := errors.New("some other error")
	wrapped := WrapIfContextExceeded(err, "")
	if wrapped != err {
		t.Error("expected original error when neither err nor stderr matches")
	}
}

func TestWrapIfContextExceeded_AlreadyTyped(t *testing.T) {
	inner := errors.New("token limit hit")
	err := &ContextLengthExceededError{Err: inner}
	wrapped := WrapIfContextExceeded(err, "")
	if wrapped != err {
		t.Error("expected already-typed error to be returned as-is")
	}
}
