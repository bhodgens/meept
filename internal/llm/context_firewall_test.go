package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubChatter returns a preconfigured response or error.
type stubChatter struct {
	resp *Response
	err  error
}

func (s *stubChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *stubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *stubChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// makeFiller returns a string whose heuristic-tokenizer count is approximately n tokens.
func makeFiller(tokens int) string {
	return strings.Repeat("x", tokens*3)
}

func TestFirewall_DropOldContextStatsIncrements(t *testing.T) {
	// ContextLimit 1000, HardLimit 0.3 -> drop path triggers above 300 tokens
	// but ValidateContextSize requires total <= 1000.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		OverflowStrategy:       "drop",
		HardLimit:              0.3,
		WrapUpThreshold:        0.1,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	stats := f.Stats()
	if stats.DropEvents == 0 {
		t.Errorf("expected DropEvents > 0, got 0")
	}
	if stats.DroppedMessages == 0 {
		t.Errorf("expected DroppedMessages > 0, got 0")
	}
}

func TestFirewall_SummarizationFailureStatsIncrements(t *testing.T) {
	// Configure so summarization triggers but the summarizer returns an error.
	// DropContextOnHardLimit=false so messages aren't dropped before summarization.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		SummarizeHistory:       true,
		DropContextOnHardLimit: false,
		HardLimit:              0.3,
		WrapUpThreshold:        0.1,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summarizer := &stubChatter{err: errors.New("summarizer unavailable")}
	f := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	if got := f.Stats().SummarizationFailures; got == 0 {
		t.Errorf("expected SummarizationFailures > 0, got 0")
	}
}

func TestFirewall_StatsStartAtZero(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{Enabled: true}
	f := NewContextFirewall(&stubChatter{resp: &Response{}}, model, cfg, nil, nil, &HeuristicTokenizer{})

	stats := f.Stats()
	if stats.DropEvents != 0 || stats.DroppedMessages != 0 || stats.SummarizationFailures != 0 {
		t.Errorf("expected zero stats, got %+v", stats)
	}
}

func TestContextFirewall_RestartStrategy(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:          true,
		OverflowStrategy: "restart",
		HardLimit:        0.3,
		WrapUpThreshold:  0.1,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summaryModel := &stubChatter{resp: &Response{Content: "Summary of conversation."}}
	f := NewContextFirewall(inner, model, cfg, summaryModel, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "final question"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	stats := f.Stats()
	if stats.RestartEvents == 0 {
		t.Errorf("expected RestartEvents > 0, got 0")
	}
}

func TestContextFirewall_RestartFallbackOnLLMFailure(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		OverflowStrategy:       "restart",
		HardLimit:              0.3,
		WrapUpThreshold:        0.1,
		DropContextOnHardLimit: true,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summaryModel := &stubChatter{err: errors.New("model unavailable")}
	f := NewContextFirewall(inner, model, cfg, summaryModel, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	stats := f.Stats()
	if stats.SummarizationFailures == 0 {
		t.Errorf("expected SummarizationFailures > 0 after fallback, got 0")
	}
	// Should have fallen back to dropOldContext.
	if stats.DropEvents == 0 {
		t.Errorf("expected DropEvents > 0 after fallback, got 0")
	}
	// Restart should NOT have been counted since the LLM failed.
	if stats.RestartEvents != 0 {
		t.Errorf("expected RestartEvents == 0 after fallback, got %d", stats.RestartEvents)
	}
}

func TestContextFirewall_RestartNoOpFewMessages(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:          true,
		OverflowStrategy: "restart",
		HardLimit:        0.3,
		WrapUpThreshold:  0.1,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summaryModel := &stubChatter{resp: &Response{Content: "summary"}}
	f := NewContextFirewall(inner, model, cfg, summaryModel, nil, &HeuristicTokenizer{})

	// Only 2 non-system messages (less than 3).
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Restart should not have fired.
	stats := f.Stats()
	if stats.RestartEvents != 0 {
		t.Errorf("expected RestartEvents == 0 with few messages, got %d", stats.RestartEvents)
	}
}

// capturingChatter records the messages it receives and returns a stub response.
type capturingChatter struct {
	got []ChatMessage
}

func (c *capturingChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	c.got = append([]ChatMessage(nil), messages...)
	return &Response{Content: "ok"}, nil
}

func (c *capturingChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *capturingChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// TestContextFirewall_OversizedSystemMessageIsBounded reproduces the incident:
// a 219 KB system message (a full skill catalog) against a 32k-token model.
// Every compaction strategy preserves system messages verbatim, so without
// the final bound ValidateContextSize rejects the request and the LLM call
// dies. With the fix, the system message is bounded to a small size and the
// model call succeeds.
func TestContextFirewall_OversizedSystemMessageIsBounded(t *testing.T) {
	inner := &capturingChatter{}
	model := &ModelConfig{ContextLimit: 32768}
	cfg := ContextFirewallConfig{
		Enabled:          true,
		OverflowStrategy: "restart",
		HardLimit:        0.8,
		WrapUpThreshold:  0.5,
	}
	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	hugeCatalog := "# Skill Catalog\n\n" + strings.Repeat("- skill: description [load: file_read /skills/x/SKILL.md]\n", 4000)
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: hugeCatalog},
		{Role: RoleUser, Content: "list the skills"},
	}

	resp, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat with oversized system message failed: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(inner.got) == 0 {
		t.Fatal("inner chatter received no messages")
	}
	var systemLen int
	for _, m := range inner.got {
		if m.Role == RoleSystem {
			systemLen = len(m.Content)
		}
	}
	if systemLen > maxSystemMessageBytes+256 {
		t.Fatalf("system message not bounded: %d bytes (bound %d)", systemLen, maxSystemMessageBytes)
	}
	if systemLen >= len(hugeCatalog) {
		t.Fatalf("system message unchanged: %d bytes", systemLen)
	}
	// Input must not be mutated.
	if len(msgs[0].Content) != len(hugeCatalog) {
		t.Error("processMessages mutated the input system message")
	}
}

// TestContextFirewall_SmallSystemMessageUntouched pins that normal system
// prompts pass through the bound byte-identically.
func TestContextFirewall_SmallSystemMessageUntouched(t *testing.T) {
	inner := &capturingChatter{}
	model := &ModelConfig{ContextLimit: 32768}
	cfg := ContextFirewallConfig{Enabled: true, HardLimit: 0.8, WrapUpThreshold: 0.5}
	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	const sys = "You are a helpful assistant."
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: sys},
		{Role: RoleUser, Content: "hello"},
	}
	if _, err := f.Chat(context.Background(), msgs); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(inner.got) == 0 || inner.got[0].Content != sys {
		t.Errorf("small system message changed: %+v", inner.got)
	}
}

func TestContextFirewall_DropStrategyUnchanged(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		OverflowStrategy:       "drop",
		DropContextOnHardLimit: true,
		HardLimit:              0.3,
		WrapUpThreshold:        0.1,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	stats := f.Stats()
	if stats.DropEvents == 0 {
		t.Errorf("expected DropEvents > 0 for drop strategy, got 0")
	}
	if stats.RestartEvents != 0 {
		t.Errorf("expected RestartEvents == 0 for drop strategy, got %d", stats.RestartEvents)
	}
}
