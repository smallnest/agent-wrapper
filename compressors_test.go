package agentwrapper

import (
	"testing"

	"github.com/smallnest/agent-wrapper/types"
)

func msg(role types.Role, content string) types.Message {
	return types.Message{Role: role, Content: content}
}

func TestSlidingWindow_UnderLimit(t *testing.T) {
	comp := NewSlidingWindowCompressor(10)
	in := []types.Message{
		msg("user", "hello"),
		msg("assistant", "hi"),
	}
	out := comp.Compress(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
}

func TestSlidingWindow_OverLimit(t *testing.T) {
	comp := NewSlidingWindowCompressor(3)
	var in []types.Message
	for i := range 10 {
		in = append(in, msg("user", string(rune('a'+i))))
	}
	out := comp.Compress(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	// Should be the last 3.
	if out[0].Content != "h" {
		t.Errorf("expected 'h' at position 0, got %q", out[0].Content)
	}
}

func TestSlidingWindow_PreservesSystemPrompt(t *testing.T) {
	comp := NewSlidingWindowCompressor(3)

	// A long first message (> 200 chars) is treated as a system prompt.
	sysPrompt := ""
	for range 201 {
		sysPrompt += "x"
	}
	in := []types.Message{
		msg("user", sysPrompt),
		msg("user", "a"),
		msg("assistant", "b"),
		msg("user", "c"),
		msg("assistant", "d"),
	}
	out := comp.Compress(in)
	if len(out) != 4 {
		t.Fatalf("expected 4 (system + 3 window), got %d", len(out))
	}
	if out[0].Content != sysPrompt {
		t.Error("system prompt should be preserved at position 0")
	}
	// Last 3 messages.
	if out[1].Content != "b" || out[2].Content != "c" || out[3].Content != "d" {
		t.Errorf("window mismatch: %q %q %q", out[1].Content, out[2].Content, out[3].Content)
	}
}

func TestSlidingWindow_NeverBelowTwo(t *testing.T) {
	// retain=1 should clamp to 2.
	comp := NewSlidingWindowCompressor(1)
	in := []types.Message{
		msg("user", "a"),
		msg("assistant", "b"),
		msg("user", "c"),
		msg("assistant", "d"),
	}
	out := comp.Compress(in)
	if len(out) < 2 {
		t.Fatalf("expected at least 2, got %d", len(out))
	}
	if len(out) != 2 {
		t.Fatalf("expected exactly 2 after clamping, got %d", len(out))
	}
}

func TestSlidingWindow_SmallInput(t *testing.T) {
	comp := NewSlidingWindowCompressor(20)
	in := []types.Message{msg("user", "only one")}
	out := comp.Compress(in)
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
}

func TestSlidingWindow_EmptyInput(t *testing.T) {
	comp := NewSlidingWindowCompressor(20)
	out := comp.Compress(nil)
	if len(out) != 0 {
		t.Fatalf("expected 0, got %d", len(out))
	}
}

func TestSummaryCompressor_Basic(t *testing.T) {
	var in []types.Message
	for i := range 10 {
		in = append(in, msg("user", string(rune('a'+i))))
	}
	comp := NewSummaryCompressor(3, nil)
	out := comp.Compress(in)

	// Should have summary + 3 retained.
	if len(out) != 4 {
		t.Fatalf("expected 4 (summary + 3 window), got %d", len(out))
	}
	if out[0].Content[:10] != "[SUMMARY] " {
		t.Errorf("first message should be summary, got %q", out[0].Content)
	}
}

func TestSummaryCompressor_UnderLimit(t *testing.T) {
	comp := NewSummaryCompressor(10, nil)
	in := []types.Message{msg("user", "a"), msg("assistant", "b")}
	out := comp.Compress(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 (no compression needed), got %d", len(out))
	}
}

func TestSummaryCompressor_CustomSummarizer(t *testing.T) {
	custom := func(msgs []types.Message) (string, error) {
		return "custom summary of " + string(rune('0'+len(msgs))), nil
	}
	comp := NewSummaryCompressor(2, custom)
	var in []types.Message
	for range 5 {
		in = append(in, msg("user", "msg"))
	}
	out := comp.Compress(in)
	if out[0].Content != "[SUMMARY] custom summary of 3" {
		t.Errorf("unexpected summary: %q", out[0].Content)
	}
}

func TestChainedCompressor_FirstReduces(t *testing.T) {
	sw := NewSlidingWindowCompressor(2)
	sc := NewSummaryCompressor(2, nil)
	chained := NewChainedCompressor(sw, sc)

	var in []types.Message
	for i := range 10 {
		in = append(in, msg("user", string(rune('a'+i))))
	}
	out := chained.Compress(in)
	// SlidingWindow should reduce 10 -> 2, no summary needed.
	if len(out) != 2 {
		t.Fatalf("expected 2 from sliding window, got %d", len(out))
	}
}

func TestChainedCompressor_FallbackToSecond(t *testing.T) {
	// First compressor does nothing (returns same), second reduces.
	noop := stubCompressor{}
	sw := NewSlidingWindowCompressor(2)
	chained := NewChainedCompressor(noop, sw)

	var in []types.Message
	for i := range 10 {
		in = append(in, msg("user", string(rune('a'+i))))
	}
	out := chained.Compress(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 from fallback, got %d", len(out))
	}
}

func TestChainedCompressor_NoneReduce(t *testing.T) {
	noop1 := stubCompressor{}
	noop2 := stubCompressor{}
	chained := NewChainedCompressor(noop1, noop2)

	in := []types.Message{msg("user", "a"), msg("assistant", "b")}
	out := chained.Compress(in)
	if len(out) != 2 {
		t.Fatalf("expected unchanged 2, got %d", len(out))
	}
}

func TestChainedCompressor_EmptyChain(t *testing.T) {
	chained := NewChainedCompressor()
	in := []types.Message{msg("user", "a")}
	out := chained.Compress(in)
	if len(out) != 1 {
		t.Fatalf("expected unchanged, got %d", len(out))
	}
}

func TestDefaultRetain(t *testing.T) {
	sw := NewSlidingWindowCompressor(0)
	if sw.RetainMessages != 20 {
		t.Errorf("expected default 20, got %d", sw.RetainMessages)
	}
	sc := NewSummaryCompressor(0, nil)
	if sc.RetainMessages != 20 {
		t.Errorf("expected default 20, got %d", sc.RetainMessages)
	}
}
