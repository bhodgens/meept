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
