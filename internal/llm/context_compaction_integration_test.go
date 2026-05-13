package llm

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Integration tests: compactor -> compressor -> firewall pipeline
// ---------------------------------------------------------------------------

// summaryResponse is a reusable structured LLM response for compaction tests.
const summaryResponse = `## Goal
Fix the authentication bug

## Constraints
Must work with existing API

## Progress
Investigated the issue and found root cause

## Key Decisions
- Use JWT tokens instead of sessions
- Rate limit at 100 req/min

## Files
- read: internal/auth/handler.go
- write: internal/auth/middleware.go
- edit: internal/config/routes.go

## Important Discoveries
- Token expiry was not being checked
- Session store has race condition

## Errors Encountered
- Database connection timeout during load test

## Next Steps
- Implement token refresh endpoint
- Add integration tests

## Critical Context
- API endpoint: /api/v1/auth`

// makeIntegrationMessages builds a message list with enough tokens to exceed
// a given utilization threshold for a model with the given context limit.
func makeIntegrationMessages(pairs int) []ChatMessage {
	msgs := []ChatMessage{{Role: RoleSystem, Content: "system prompt"}}
	for i := range pairs {
		msgs = append(msgs,
			ChatMessage{Role: RoleUser, Content: fmt.Sprintf("user message %d %s", i, makeLongString(90))},
			ChatMessage{Role: RoleAssistant, Content: fmt.Sprintf("assistant reply %d %s", i, makeLongString(90))},
		)
	}
	return msgs
}

// ---------------------------------------------------------------------------
// Test: Compactor triggers at TriggerRatio in firewall processMessages
// ---------------------------------------------------------------------------

func TestIntegration_CompactorTriggersAtTriggerRatio(t *testing.T) {
	// Set up a model with a 3000-token context limit. With 10 pairs of
	// user/assistant messages at ~90 tokens each, we get ~1800 tokens,
	// which is 60% utilization -- right at the default TriggerRatio.
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	// Create and wire the compactor with a 0.60 trigger ratio
	compactor := NewContextCompactor(
		CompactorConfig{
			KeepRecentTokens: 600,
			TrackFileOps:     true,
			TimeoutSeconds:   5,
		},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// The compactor mock should have been called
	if !compactorMock.called {
		t.Error("expected compactor Chat to be called")
	}

	// Compaction stats should be incremented
	stats := f.Stats()
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0")
	}
	if stats.CompactionTokensSaved == 0 {
		t.Error("expected CompactionTokensSaved > 0")
	}
}

// ---------------------------------------------------------------------------
// Test: Compactor does NOT trigger below TriggerRatio
// ---------------------------------------------------------------------------

func TestIntegration_CompactorBelowTriggerRatio(t *testing.T) {
	// Model with 10000-token limit. 10 pairs at ~90 tokens each = ~1800 tokens = 18%
	model := &ModelConfig{ContextLimit: 10000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Compactor should NOT have been called (18% < 60%)
	if compactorMock.called {
		t.Error("compactor should not be called below trigger ratio")
	}

	stats := f.Stats()
	if stats.CompactionEvents != 0 {
		t.Errorf("expected 0 CompactionEvents, got %d", stats.CompactionEvents)
	}
}

// ---------------------------------------------------------------------------
// Test: Compactor fallback when it fails to compact
// ---------------------------------------------------------------------------

func TestIntegration_CompactorFallbackOnFailure(t *testing.T) {
	// Model with 3000-token limit -> 10 pairs at ~1800 tokens = 60%
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
		ProactiveCompression:   true, // enable compressor as Layer 2
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		err: fmt.Errorf("compaction LLM unavailable"),
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Compactor should have been attempted
	if !compactorMock.called {
		t.Error("expected compactor to be attempted")
	}

	// Fallback counter should be incremented
	stats := f.Stats()
	if stats.CompactionFallbacks == 0 {
		t.Error("expected CompactionFallbacks > 0")
	}

	// The compressor (Layer 2) should still apply aggressive or summarize
	// stage since utilization is still high after compaction failed.
	if stats.CompressionSummarizeEvents == 0 && stats.CompressionAggressiveEvents == 0 {
		t.Error("expected compressor to run as fallback after compaction failure")
	}
}

// ---------------------------------------------------------------------------
// Test: Full pipeline -- compactor (Layer 1) + compressor (Layer 2) + hard limit (Layer 3)
// ---------------------------------------------------------------------------

func TestIntegration_FullPipeline(t *testing.T) {
	// Model with a small context limit to force all three layers.
	// 10 pairs at ~1800 tokens, context limit 2400 -> utilization ~75%.
	// Compactor triggers at 60%, compressor at 70%, hard limit at 90%.
	model := &ModelConfig{ContextLimit: 2400}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
		ProactiveCompression:   true,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	resp, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	stats := f.Stats()

	// Layer 1: compaction should have triggered
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0")
	}

	// The compaction should have reduced tokens, saving some
	if stats.CompactionTokensSaved == 0 {
		t.Error("expected CompactionTokensSaved > 0")
	}
}

// ---------------------------------------------------------------------------
// Test: Compactor with ProactiveCompression disabled still works
// ---------------------------------------------------------------------------

func TestIntegration_CompactorWithoutProactiveCompression(t *testing.T) {
	// The compactor should trigger independently of the compressor.
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
		ProactiveCompression:   false, // compressor NOT enabled
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Compactor should still fire even without ProactiveCompression
	if !compactorMock.called {
		t.Error("compactor should be called independently of ProactiveCompression")
	}

	stats := f.Stats()
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0 without ProactiveCompression")
	}
}

// ---------------------------------------------------------------------------
// Test: Compressor delegates to compactor in summarizeOldHistory
// ---------------------------------------------------------------------------

func TestIntegration_CompressorDelegatesToCompactor(t *testing.T) {
	// Create a compressor with ProactiveCompression enabled and a compactor set.
	// At stage 2 (60-70%), the compressor should delegate to the compactor.
	mock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	compactorCfg := CompactorConfig{
		KeepRecentTokens: 600,
		TrackFileOps:     true,
		TimeoutSeconds:   5,
	}
	compactor := NewContextCompactor(compactorCfg, mock, &HeuristicTokenizer{}, nil)

	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    DefaultWarningRatio,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)
	c.SetCompactor(compactor)

	// 10 user messages, utilization 0.65 -> stage 2 (summarize)
	msgs := makeCompressorMessages(10)
	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Stage != CompressionStageSummarize {
		t.Fatalf("expected Summarize stage, got %s", result.Stage)
	}

	// The compactor mock should have been called (delegated from summarizeOldHistory)
	if !mock.called {
		t.Error("expected compactor to be called via compressor delegation")
	}

	// Result should be compacted (compactor produces a summary + kept messages)
	if !result.Compressed {
		t.Error("expected Compressed=true")
	}

	// Should have fewer messages than the original
	if len(result.Messages) >= len(msgs) {
		t.Errorf("expected fewer messages after compaction, got %d (original %d)",
			len(result.Messages), len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Test: Firewall summarizeWithLevel delegates to compactor at level 1
// ---------------------------------------------------------------------------

func TestIntegration_FirewallSummarizeWithLevelDelegatesToCompactor(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		SummarizeHistory:       true,
		DropContextOnHardLimit: false,
		HardLimit:              0.30, // low threshold to trigger summarizeOldHistory
		WrapUpThreshold:        0.10,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// Build messages with enough tokens to exceed the HardLimit threshold
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: strings.Repeat("word ", 200)},
		{Role: RoleAssistant, Content: strings.Repeat("word ", 200)},
		{Role: RoleUser, Content: strings.Repeat("word ", 200)},
		{Role: RoleAssistant, Content: strings.Repeat("word ", 200)},
		{Role: RoleUser, Content: strings.Repeat("word ", 200)},
		{Role: RoleAssistant, Content: strings.Repeat("word ", 200)},
		{Role: RoleUser, Content: "latest"},
	}

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// The compactor should have been called through the processMessages
	// trigger OR through the legacy summarizeOldHistory path
	if !compactorMock.called {
		t.Error("expected compactor to be called")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction result includes file operation tracking
// ---------------------------------------------------------------------------

func TestIntegration_CompactionTracksFileOps(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)
	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// File ops should be tracked on the compactor
	fo := compactor.FileOperations()
	if fo == nil {
		t.Fatal("expected file operations to be tracked")
	}
	if !fo.Read["internal/auth/handler.go"] {
		t.Error("expected handler.go in reads")
	}
	if !fo.Written["internal/auth/middleware.go"] {
		t.Error("expected middleware.go in writes")
	}
	if !fo.Edited["internal/config/routes.go"] {
		t.Error("expected routes.go in edits")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction stats in FirewallStats snapshot
// ---------------------------------------------------------------------------

func TestIntegration_CompactionStatsInSnapshot(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// Before any chat: stats should be zero
	stats := f.Stats()
	if stats.CompactionEvents != 0 || stats.CompactionTokensSaved != 0 || stats.CompactionFallbacks != 0 {
		t.Errorf("expected zero compaction stats initially, got events=%d saved=%d fallbacks=%d",
			stats.CompactionEvents, stats.CompactionTokensSaved, stats.CompactionFallbacks)
	}

	msgs := makeIntegrationMessages(10)
	f.Chat(context.Background(), msgs)

	stats = f.Stats()
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0 after chat")
	}
	if stats.CompactionTokensSaved == 0 {
		t.Error("expected CompactionTokensSaved > 0 after chat")
	}
}

// ---------------------------------------------------------------------------
// Test: Iterative compaction updates (two consecutive compactions)
// ---------------------------------------------------------------------------

func TestIntegration_IterativeCompaction(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}

	// First compactor mock
	mock1 := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work
## Key Decisions
- d1
## Files
- read: a.go
## Progress
started
## Important Discoveries
none
## Errors Encountered
none
## Next Steps
continue
## Critical Context
none
## Constraints
none`},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		mock1,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// First chat: compaction with initial summary
	msgs1 := makeIntegrationMessages(10)
	f.Chat(context.Background(), msgs1)

	if !mock1.called {
		t.Fatal("first compaction should have been called")
	}
	firstSummary := compactor.LastSummary()
	if firstSummary == "" {
		t.Fatal("expected non-empty first summary")
	}

	// Second compactor mock (for iterative update)
	mock2 := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work
## Key Decisions
- d1
- d2
## Files
- read: a.go
- write: b.go
## Progress
continued
## Important Discoveries
found something
## Errors Encountered
none
## Next Steps
finish
## Critical Context
none
## Constraints
none`},
	}
	compactor.summarizer = mock2

	// Second chat: should use iterative update prompt
	msgs2 := makeIntegrationMessages(10)
	f.Chat(context.Background(), msgs2)

	if !mock2.called {
		t.Fatal("second compaction should have been called")
	}

	// The second call should have received the iterative update prompt
	if len(mock2.lastMsgs) == 0 {
		t.Fatal("expected messages to be sent to summarizer")
	}
	prompt := mock2.lastMsgs[0].Content
	if !strings.Contains(prompt, "You are updating a conversation summary") {
		t.Errorf("second compaction should use iterative update prompt, got: %s",
			prompt[:min(200, len(prompt))])
	}

	// File ops should be cumulative across both compactions
	fo := compactor.FileOperations()
	if !fo.Read["a.go"] {
		t.Error("a.go should still be tracked from first compaction")
	}
	if !fo.Written["b.go"] {
		t.Error("b.go should be tracked from second compaction")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction with context cancellation / timeout
// ---------------------------------------------------------------------------

func TestIntegration_CompactionContextCancellation(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	ctxAwareMock := &contextAwareMockForIntegration{
		err: fmt.Errorf("context deadline exceeded"),
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		ctxAwareMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)

	// Chat should succeed even when compaction fails (fallback behavior)
	resp, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat should not fail when compaction fails: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Fallback counter should be incremented
	stats := f.Stats()
	if stats.CompactionFallbacks == 0 {
		t.Error("expected CompactionFallbacks > 0 when compaction fails")
	}
}

// contextAwareMockForIntegration is a mock that always returns an error,
// simulating compaction failure for fallback tests.
type contextAwareMockForIntegration struct {
	err error
}

func (m *contextAwareMockForIntegration) Chat(_ context.Context, _ []ChatMessage, _ ...ChatOption) (*Response, error) {
	return nil, m.err
}

func (m *contextAwareMockForIntegration) ChatWithProgress(_ context.Context, msgs []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(context.Background(), msgs)
}

func (m *contextAwareMockForIntegration) Config() *ModelConfig {
	return &ModelConfig{}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Test: Compaction preserves critical information from conversation
// ---------------------------------------------------------------------------

func TestIntegration_CompactionPreservesCriticalInfo(t *testing.T) {
	// Build a conversation with specific critical information that must be preserved
	criticalSummary := `## Goal
Fix authentication bypass in production

## Constraints
Must maintain backward compatibility with v1 API

## Progress
Investigated three files, found root cause

## Key Decisions
- Use JWT tokens instead of sessions
- Rate limit at 100 req/min

## Files
- read: internal/auth/handler.go
- write: internal/auth/jwt.go
- edit: internal/auth/middleware.go

## Important Discoveries
- Token expiry was not being checked
- Session store has race condition

## Errors Encountered
- Database timeout at 500 concurrent users

## Next Steps
- Implement token refresh endpoint
- Add integration tests

## Critical Context
- API endpoint: /api/v1/auth/token
- DB pool size: 50`

	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: criticalSummary},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)
	resp, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify file operations contain the critical file paths
	fo := compactor.FileOperations()
	if !fo.Read["internal/auth/handler.go"] {
		t.Error("expected handler.go in reads")
	}
	if !fo.Written["internal/auth/jwt.go"] {
		t.Error("expected jwt.go in writes")
	}
	if !fo.Edited["internal/auth/middleware.go"] {
		t.Error("expected middleware.go in edits")
	}

	// Verify the compactor's last summary preserves the critical info
	lastSummary := compactor.LastSummary()
	criticalItems := []string{
		"JWT tokens",
		"/api/v1/auth/token",
		"Token expiry",
		"token refresh endpoint",
	}
	for _, item := range criticalItems {
		if !strings.Contains(lastSummary, item) {
			t.Errorf("last summary missing critical info: %q", item)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction with tool call chains in messages
// ---------------------------------------------------------------------------

func TestIntegration_CompactionWithToolCallChains(t *testing.T) {
	// Build messages that contain tool call/result pairs
	toolSummary := `## Goal
Read and fix Go files
## Files
- read: internal/auth/handler.go
- read: internal/auth/middleware.go
- edit: internal/auth/handler.go
## Progress
Read two files, applied fixes
## Important Discoveries
Both files had missing error handling
## Key Decisions
- Add error wrapping with fmt.Errorf
## Errors Encountered
none
## Next Steps
Run tests
## Critical Context
none`

	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: toolSummary},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// Build messages with tool call chains
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "Read the auth files and fix them" + makeLongString(50)},
	}

	// First tool call chain
	toolCalls1 := []ToolCall{
		{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path": "internal/auth/handler.go"}`}},
	}
	msgs = append(msgs, ChatMessage{Role: RoleAssistant, Content: "Reading handler.go" + makeLongString(40), ToolCalls: toolCalls1})
	msgs = append(msgs, ChatMessage{Role: RoleTool, Content: "package auth\nfunc HandleAuth() { ... }" + makeLongString(50), ToolCallID: "call_1"})

	// Second tool call chain
	toolCalls2 := []ToolCall{
		{ID: "call_2", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path": "internal/auth/middleware.go"}`}},
	}
	msgs = append(msgs, ChatMessage{Role: RoleAssistant, Content: "Reading middleware.go" + makeLongString(40), ToolCalls: toolCalls2})
	msgs = append(msgs, ChatMessage{Role: RoleTool, Content: "package auth\nfunc Middleware() { ... }" + makeLongString(50), ToolCallID: "call_2"})

	// More messages to push utilization over 60%
	for i := range 8 {
		msgs = append(msgs,
			ChatMessage{Role: RoleUser, Content: fmt.Sprintf("user follow-up %d %s", i, makeLongString(90))},
			ChatMessage{Role: RoleAssistant, Content: fmt.Sprintf("assistant response %d %s", i, makeLongString(90))},
		)
	}

	resp, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Compactor should have been called
	if !compactorMock.called {
		t.Error("expected compactor to be called for conversation with tool calls")
	}

	stats := f.Stats()
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0")
	}

	// File operations should be tracked
	fo := compactor.FileOperations()
	if !fo.Read["internal/auth/handler.go"] {
		t.Error("expected handler.go in reads")
	}
	if !fo.Read["internal/auth/middleware.go"] {
		t.Error("expected middleware.go in reads")
	}
	if !fo.Edited["internal/auth/handler.go"] {
		t.Error("expected handler.go in edits")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction stats are atomically incremented (concurrent safety)
// ---------------------------------------------------------------------------

func TestIntegration_CompactionStatsConcurrentSafety(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// Run multiple chats in parallel to exercise atomic counters
	done := make(chan struct{})
	for range 5 {
		go func() {
			defer func() { done <- struct{}{} }()
			msgs := makeIntegrationMessages(10)
			_, err := f.Chat(context.Background(), msgs)
			if err != nil {
				t.Errorf("Chat: unexpected error: %v", err)
			}
		}()
	}

	// Wait for all goroutines to finish
	for range 5 {
		<-done
	}

	stats := f.Stats()
	if stats.CompactionEvents == 0 {
		t.Error("expected CompactionEvents > 0 after concurrent chats")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction does not trigger when context limit is large
// ---------------------------------------------------------------------------

func TestIntegration_CompactionNotNeededForSmallContext(t *testing.T) {
	// Large context limit means utilization stays well below trigger
	model := &ModelConfig{ContextLimit: 100000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	f.SetCompactor(compactor, 0.60)

	// Only 5 pairs -- very small relative to 100K context
	msgs := makeIntegrationMessages(5)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	if compactorMock.called {
		t.Error("compactor should not be called when utilization is low")
	}

	stats := f.Stats()
	if stats.CompactionEvents != 0 {
		t.Error("expected zero CompactionEvents for small context")
	}
}

// ---------------------------------------------------------------------------
// Test: Zero trigger ratio means compactor only used through compressor
// ---------------------------------------------------------------------------

func TestIntegration_ZeroTriggerRatioCompactorOnlyViaCompressor(t *testing.T) {
	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
		ProactiveCompression:   true, // compressor enabled
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		nil,
	)
	// Zero trigger ratio: compactor only fires through compressor pipeline
	f.SetCompactor(compactor, 0)

	msgs := makeIntegrationMessages(10)

	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	// Compactor should have been called through the compressor's
	// summarizeOldHistory path (stage 2 triggers at 60%)
	if !compactorMock.called {
		t.Error("expected compactor to be called via compressor path")
	}

	// But compaction stats (direct trigger) should be zero
	stats := f.Stats()
	if stats.CompactionEvents != 0 {
		t.Errorf("expected 0 direct CompactionEvents with zero trigger ratio, got %d",
			stats.CompactionEvents)
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction logging observability
// ---------------------------------------------------------------------------

func TestIntegration_CompactionLoggingObservability(t *testing.T) {
	// Capture log output via a buffer
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: false,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		response: &Response{Content: summaryResponse},
	}

	f := NewContextFirewall(inner, model, cfg, nil, logger, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TrackFileOps: true, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		logger,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)
	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat: unexpected error: %v", err)
	}

	logOutput := buf.String()

	// Verify that the compactor logged its compaction event
	if !strings.Contains(logOutput, "context compacted") {
		t.Error("expected 'context compacted' log message from compactor")
	}
	if !strings.Contains(logOutput, "tokens_before") {
		t.Error("expected 'tokens_before' in compaction log")
	}
	if !strings.Contains(logOutput, "tokens_after") {
		t.Error("expected 'tokens_after' in compaction log")
	}
	if !strings.Contains(logOutput, "files_tracked") {
		t.Error("expected 'files_tracked' in compaction log")
	}

	// Verify that the firewall logged its compaction applied event
	if !strings.Contains(logOutput, "context compaction applied") {
		t.Error("expected 'context compaction applied' log message from firewall")
	}
	if !strings.Contains(logOutput, "utilization_before") {
		t.Error("expected 'utilization_before' in firewall compaction log")
	}
}

// ---------------------------------------------------------------------------
// Test: Compaction logging on failure (fallback)
// ---------------------------------------------------------------------------

func TestIntegration_CompactionLoggingOnFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	model := &ModelConfig{ContextLimit: 3000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		DropContextOnHardLimit: true,
		HardLimit:              0.90,
		WrapUpThreshold:        0.50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	compactorMock := &compactorMockChatter{
		err: fmt.Errorf("compaction LLM unavailable"),
	}

	f := NewContextFirewall(inner, model, cfg, nil, logger, &HeuristicTokenizer{})
	compactor := NewContextCompactor(
		CompactorConfig{KeepRecentTokens: 600, TimeoutSeconds: 5},
		compactorMock,
		&HeuristicTokenizer{},
		logger,
	)
	f.SetCompactor(compactor, 0.60)

	msgs := makeIntegrationMessages(10)
	_, err := f.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Chat should not fail: %v", err)
	}

	logOutput := buf.String()

	// Compactor should log a warning about summarization failure
	if !strings.Contains(logOutput, "compaction summarization failed") {
		t.Error("expected 'compaction summarization failed' warning in logs")
	}

	// Firewall should log a debug message about not compacting
	if !strings.Contains(logOutput, "compaction returned without compacting") {
		t.Error("expected 'compaction returned without compacting' debug log")
	}
}
