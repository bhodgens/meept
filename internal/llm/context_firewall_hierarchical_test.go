package llm

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

// countingStubChatter tracks how many times Chat is called and records the
// prompt it received. It returns a fixed-size response.
type countingStubChatter struct {
	calls      atomic.Int32
	lastPrompt string
	respSize   int // tokens in the response (using heuristic 3 chars/token)
	err        error
}

func (c *countingStubChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	c.calls.Add(1)
	if len(messages) > 0 {
		c.lastPrompt = messages[0].Content
	}
	if c.err != nil {
		return nil, c.err
	}
	content := strings.Repeat("s", c.respSize*3)
	return &Response{Content: content}, nil
}

func (c *countingStubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *countingStubChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// tieredStubChatter returns responses whose size shrinks with each call,
// simulating the behavior where a summarizer produces shorter and shorter
// summaries. Each successive call returns respSize - shrinkBy*tokens tokens.
type tieredStubChatter struct {
	calls    atomic.Int32
	respSize int // base tokens for first response
	shrinkBy int // tokens to subtract per call
	err      error
}

func (t *tieredStubChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	callNum := int(t.calls.Add(1)) - 1
	if t.err != nil {
		return nil, t.err
	}
	tokens := max(t.respSize-t.shrinkBy*callNum, 1)
	content := strings.Repeat("s", tokens*3)
	return &Response{Content: content}, nil
}

func (t *tieredStubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return t.Chat(ctx, messages, opts...)
}

func (t *tieredStubChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

func TestHierarchicalSummarization_Disabled(t *testing.T) {
	// When HierarchicalSummarization is false, summarization should work as
	// before -- single pass, no recursion, SummaryLevel=1 on the summary.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: false,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     100,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summarizer := &countingStubChatter{respSize: 50}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	// Build enough messages to trigger summarization (need > keepCount).
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

	_, err := firewall.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	if got := summarizer.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 summarizer call when hierarchical summarization is disabled, got %d", got)
	}
}

func TestHierarchicalSummarization_RecursesWhenSummaryExceedsThreshold(t *testing.T) {
	// The summarizer returns a 300-token response on the first call and a
	// 50-token response on the second call. With SummaryLevelThreshold=100,
	// the 300-token summary should trigger a second summarization pass.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     100,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	// First call returns 300 tokens, second returns 50 tokens
	summarizer := &tieredStubChatter{respSize: 300, shrinkBy: 250}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

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

	_, err := firewall.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Should have called summarizer twice: level 1 (300 tokens > 100 threshold)
	// then level 2 (50 tokens < 100 threshold, stops).
	if got := summarizer.calls.Load(); got != 2 {
		t.Errorf("expected 2 summarizer calls for recursive summarization, got %d", got)
	}
}

func TestHierarchicalSummarization_MaxLevelRespected(t *testing.T) {
	// The summarizer always returns 300 tokens. With MaxSummaryLevel=2,
	// summarization should stop after level 2 even though the summary
	// still exceeds the threshold.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           2,
		SummaryLevelThreshold:     100,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	// Always returns 300 tokens (never shrinks)
	summarizer := &tieredStubChatter{respSize: 300, shrinkBy: 0}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

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

	_, err := firewall.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// MaxSummaryLevel=2, so we should get exactly 2 calls: level 1 and level 2.
	// Level 3 would exceed MaxSummaryLevel so recursion stops.
	if got := summarizer.calls.Load(); got != 2 {
		t.Errorf("expected 2 summarizer calls with MaxSummaryLevel=2, got %d", got)
	}
}

func TestHierarchicalSummarization_LevelMetadata(t *testing.T) {
	// Verify that the summary messages carry the correct SummaryLevel.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     100,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	// Returns 300 tokens, then 50 tokens (under threshold)
	summarizer := &tieredStubChatter{respSize: 300, shrinkBy: 250}

	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	// Call summarizeOldHistory directly so we can inspect the result.
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

	result, err := firewall.summarizeOldHistory(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarizeOldHistory: unexpected error: %v", err)
	}

	// Find the summary message(s) -- they should have SummaryLevel > 0
	var summaries []ChatMessage
	for _, m := range result {
		if m.SummaryLevel > 0 {
			summaries = append(summaries, m)
		}
	}

	if len(summaries) != 1 {
		t.Fatalf("expected exactly 1 summary message, got %d", len(summaries))
	}

	// The final summary should be at level 2 (level 1 recursed because
	// 300 tokens > 100 threshold).
	summary := summaries[0]
	if summary.SummaryLevel != 2 {
		t.Errorf("expected summary level 2, got %d", summary.SummaryLevel)
	}

	// Verify the content contains the level indicator (now wrapped in
	// <<<CONTEXT_SUMMARY>>> boundary markers).
	expectedPrefix := "[Conversation summary level 2]:"
	if !strings.Contains(summary.Content, expectedPrefix) {
		t.Errorf("expected summary content to contain %q, got %q", expectedPrefix, summary.Content[:min(120, len(summary.Content))])
	}
	// Verify boundary markers are present
	if !strings.HasPrefix(summary.Content, "<<<CONTEXT_SUMMARY") {
		t.Errorf("expected summary content to start with boundary marker, got %q", summary.Content[:min(60, len(summary.Content))])
	}
	if !strings.HasSuffix(summary.Content, "<<<END_CONTEXT_SUMMARY>>>") {
		t.Errorf("expected summary content to end with end boundary marker, got %q", summary.Content[max(0, len(summary.Content)-40):])
	}
}

func TestHierarchicalSummarization_SummaryBelowThresholdNoRecursion(t *testing.T) {
	// When the initial summary is below the threshold, no recursion occurs.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     500,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	// Returns only 50 tokens, well below 500 threshold
	summarizer := &countingStubChatter{respSize: 50}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

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

	_, err := firewall.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	if got := summarizer.calls.Load(); got != 1 {
		t.Errorf("expected 1 summarizer call when summary is below threshold, got %d", got)
	}
}

func TestHierarchicalSummarization_Defaults(t *testing.T) {
	// Verify that MaxSummaryLevel and SummaryLevelThreshold get sensible defaults.
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		HierarchicalSummarization: true,
	}

	firewall := NewContextFirewall(
		&stubChatter{resp: &Response{Content: "ok"}},
		&ModelConfig{ContextLimit: 1000},
		cfg, nil, nil, &HeuristicTokenizer{},
	)

	if firewall.config.MaxSummaryLevel != 3 {
		t.Errorf("expected default MaxSummaryLevel=3, got %d", firewall.config.MaxSummaryLevel)
	}
	if firewall.config.SummaryLevelThreshold != 500 {
		t.Errorf("expected default SummaryLevelThreshold=500, got %d", firewall.config.SummaryLevelThreshold)
	}
}

func TestHierarchicalSummarization_SummaryLevelOnDirectSummarizeWithLevel(t *testing.T) {
	// Call summarizeWithLevel directly and verify level metadata at each depth.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     50,
	}

	// Summarizer always returns 200 tokens -- will recurse until MaxSummaryLevel.
	summarizer := &tieredStubChatter{respSize: 200, shrinkBy: 0}
	firewall := NewContextFirewall(nil, model, cfg, summarizer, nil, &HeuristicTokenizer{})

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

	result, err := firewall.summarizeWithLevel(context.Background(), msgs, 1)
	if err != nil {
		t.Fatalf("summarizeWithLevel: unexpected error: %v", err)
	}

	// The final summary should be at level 3 (MaxSummaryLevel).
	var summaries []ChatMessage
	for _, m := range result {
		if m.SummaryLevel > 0 {
			summaries = append(summaries, m)
		}
	}

	if len(summaries) != 1 {
		t.Fatalf("expected exactly 1 summary message, got %d", len(summaries))
	}

	if summaries[0].SummaryLevel != 3 {
		t.Errorf("expected final summary at level 3 (MaxSummaryLevel), got %d", summaries[0].SummaryLevel)
	}

	// Should have made exactly 3 calls: level 1, level 2, level 3.
	if got := summarizer.calls.Load(); got != 3 {
		t.Errorf("expected 3 summarizer calls, got %d", got)
	}
}

func TestHierarchicalSummarization_ContentFormat(t *testing.T) {
	// Verify that summary content includes the correct level prefix at each level.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		OverflowStrategy:        "summarize",
		DropContextOnHardLimit:    false,
		HierarchicalSummarization: false, // disabled -- single level
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     50,
	}

	summarizer := &countingStubChatter{respSize: 20}
	firewall := NewContextFirewall(nil, model, cfg, summarizer, nil, &HeuristicTokenizer{})

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

	result, err := firewall.summarizeOldHistory(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarizeOldHistory: unexpected error: %v", err)
	}

	// Find the summary message
	var summary *ChatMessage
	for i := range result {
		if result[i].SummaryLevel > 0 {
			summary = &result[i]
			break
		}
	}

	if summary == nil {
		t.Fatal("expected to find a summary message with SummaryLevel > 0")
	}

	if summary.SummaryLevel != 1 {
		t.Errorf("expected level 1 without hierarchical summarization, got %d", summary.SummaryLevel)
	}

	expectedPrefix := "[Conversation summary level 1]:"
	if !strings.Contains(summary.Content, expectedPrefix) {
		t.Errorf("expected content to contain %q, got %q", expectedPrefix, summary.Content[:min(100, len(summary.Content))])
	}
	// Verify boundary markers wrap the summary
	if !strings.HasPrefix(summary.Content, "<<<CONTEXT_SUMMARY") {
		t.Errorf("expected content to start with boundary marker, got %q", summary.Content[:min(60, len(summary.Content))])
	}
	if !strings.HasSuffix(summary.Content, "<<<END_CONTEXT_SUMMARY>>>") {
		t.Errorf("expected content to end with boundary marker, got %q", summary.Content[max(0, len(summary.Content)-40):])
	}

}
