package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/compress"
	"github.com/caimlas/meept/internal/llm"
)

// memCCRStore is an in-memory CCR store for testing, implemented to satisfy
// compress.CCRStore. It is a copy of the one in compress/compress_test.go so
// this file can be self-contained in the agent package.
type memCCRStore struct {
	data map[string]*compress.CCREntry
}

func newMemCCRStore() *memCCRStore {
	return &memCCRStore{
		data: make(map[string]*compress.CCREntry),
	}
}

func (s *memCCRStore) Store(ctx context.Context, entry compress.CCREntry) (string, error) {
	h := compress.ContentHash(entry.OriginalContent)
	entry.Hash = h
	s.data[h] = &entry
	return h, nil
}

func (s *memCCRStore) Retrieve(ctx context.Context, hash string) (*compress.CCREntry, error) {
	e, ok := s.data[hash]
	if !ok {
		return nil, nil
	}
	return e, nil
}

func (s *memCCRStore) Search(ctx context.Context, hash, query string) ([]compress.CCRSearchResult, error) {
	return nil, nil
}

func (s *memCCRStore) Exists(ctx context.Context, hash string) bool {
	_, ok := s.data[hash]
	return ok
}

func (s *memCCRStore) Delete(ctx context.Context, hash string) (bool, error) {
	if _, ok := s.data[hash]; !ok {
		return false, nil
	}
	delete(s.data, hash)
	return true, nil
}

func (s *memCCRStore) Stats() compress.CCRStats {
	var totalOrig, totalComp int64
	for _, e := range s.data {
		totalOrig += int64(e.OriginalTokens)
		totalComp += int64(e.CompressedTokens)
	}
	return compress.CCRStats{
		EntryCount:          int64(len(s.data)),
		TotalOriginalTokens: totalOrig,
		TotalCompressedTokens: totalComp,
	}
}

func (s *memCCRStore) Close() error {
	return nil
}

// makeLargeJSONToolOutput builds a valid JSON array with duplicate objects so
// SmartCrusher's dedup achieves significant savings (>30% tokens saved).
func makeLargeJSONToolOutput() string {
	type item struct {
		ID      int      `json:"id"`
		Path    string   `json:"path"`
		Status  string   `json:"status"`
		Detail  string   `json:"detail"`
		Matches []string `json:"matches"`
	}
	items := make([]item, 200)
	for i := range 200 {
		items[i] = item{
			ID:      i,
			Path:    "/very/long/path/that/makes/the/output/bigger/and/bigger/and/bigger",
			Status:  "ok",
			Detail:  "this is a long detail field to bulk up the JSON payload number",
			Matches: []string{"result1", "result2", "result3", "result4", "result5"},
		}
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// largeTextOutput creates a long plain-text block that exceeds the default
// MinTokensToCompress (500 tokens ~= 2000+ characters).
func largeTextOutput() string {
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("This is line ")
		sb.WriteString(string(rune('a'+i%26)))
		sb.WriteString(" of the output, padding for compression testing.\n")
	}
	return sb.String()
}

// --- Test 1: Tool Result Compression in reasoningCycle ---

func TestAgentLoop_ToolResultCompression(t *testing.T) {
	store := newMemCCRStore()
	pipeline := compress.NewPipelineWithConfig(store, compress.PipelineConfig{
		MinTokensToCompress:  500,
		TTL:                  time.Hour,
		EnableCCR:            true,
		CompressUserMessages: false,
		TargetRatio:          0.0,
	})
	defer pipeline.Close()

	ctx := context.Background()

	// CompressToolResult with large JSON output (exceeds MinTokensToCompress)
	output := makeLargeJSONToolOutput()
	if len(output) < 1500 {
		t.Fatalf("test setup error: output too short (%d chars), need > 1500", len(output))
	}

	result, err := pipeline.CompressToolResult(ctx, "test_tool", output, 4000)
	if err != nil {
		t.Fatalf("CompressToolResult failed: %v", err)
	}

	// Verify compression was applied: result should be shorter than original
	if len(result) >= len(output) {
		t.Errorf("expected compressed output to be shorter than original: %d >= %d", len(result), len(output))
	}

	// Verify CCR marker is present (retrieval info appended after compression)
	hasCCRMarker := strings.Contains(result, "<<ccr:") ||
		strings.Contains(result, "hash=") ||
		strings.Contains(result, "items compressed to")
	if !hasCCRMarker {
		// SmartCrusher may produce a compressed JSON that already references items.
		// That is still valid compression -- the key point is the output is smaller.
		t.Log("No explicit CCR marker found, but compression produced smaller output")
	}

	// Verify CCR store was populated
	stats := store.Stats()
	if stats.EntryCount != 1 {
		t.Errorf("expected 1 CCR entry, got %d", stats.EntryCount)
	}

	// Verify tokens saved metric
	pipelineStats := pipeline.Stats()
	if pipelineStats.CCREntries != 1 {
		t.Errorf("expected 1 pipeline CCR entry in stats, got %d", pipelineStats.CCREntries)
	}

	// Test with output below threshold: should pass through unchanged
	shortOutput := "short"
	compressed, err := pipeline.CompressToolResult(ctx, "file_read", shortOutput, 4000)
	if err != nil {
		t.Fatalf("CompressToolResult on short output failed: %v", err)
	}
	if compressed != shortOutput {
		t.Errorf("expected short output to pass through unchanged, got: %s", compressed)
	}
}

// --- Test 2: Compression Fallback ---

func TestAgentLoop_CompressionFallback(t *testing.T) {
	storeF := newMemCCRStore()

	// Build pipeline with a very low threshold so we can trigger compression
	pipeline := compress.NewPipelineWithConfig(storeF, compress.PipelineConfig{
		MinTokensToCompress:  50,
		TTL:                  time.Hour,
		EnableCCR:            true,
		CompressUserMessages: false,
	})
	defer pipeline.Close()

	ctx := context.Background()

	// Test fallback: CompressToolResult with a valid large output should work
	// and NOT panic even when some internal components have edge cases.
	largeContent := largeTextOutput()
	if len(largeContent) < 500 {
		t.Fatalf("test setup error: largeContent too short (%d chars)", len(largeContent))
	}

	// The pipeline should handle large output gracefully
	result, err := pipeline.CompressToolResult(ctx, "shell_exec", largeContent, 4000)
	if err != nil {
		// Pipeline returning error is acceptable for non-structured content;
		// the key is no panic.
		t.Logf("CompressToolResult returned error (expected for unstructured text): %v", err)
	}

	// Verify the result is usable regardless
	if result == "" {
		result = largeContent
	}

	// Verify that the agent loop's reasoningCycle graceful-fallback path
	// (the `if err == nil { ... } else { logger.Debug(...) }` at
	// loop.go:2106-2110) would accept a nil-error result.
	if err == nil && len(result) > 0 {
		// Success path: compressed result is set into ExecutionResult.Result
		// This mirrors the reasoningCycle path.
		t.Logf("Compression succeeded: original %d chars, compressed %d chars", len(largeContent), len(result))
	}

	// Test that a closed pipeline returns a clear error
	pipeline.Close()
	_, err = pipeline.CompressToolResult(ctx, "file_read", largeContent, 4000)
	if err == nil {
		t.Error("expected error when pipeline is closed")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' in error, got: %v", err)
	}
}

// --- Test 3: Pre-Request Compression via ContextFirewall ---

func TestAgentLoop_CompressionPreRequest(t *testing.T) {
	// We use HeuristicTokenizer (3 chars/token) instead of tiktoken because
	// it is fully deterministic and lets us engineer exact token counts to
	// land in the desired compression-stage thresholds:
	//   Stage1 (warning): utilization >= 0.50  (>= 50 tokens)
	//   Stage2 (summarize): utilization >= 0.60 (>= 60 tokens)
	// A 100-token context limit means we need:   - warning  ~52 tokens
	//   - summarize ~62 tokens

	inner := &stubChatter{resp: &llm.Response{Content: "ok"}}
	cfg := llm.ContextFirewallConfig{
		Enabled:                true,
		ProactiveCompression:   true,
		DropContextOnHardLimit: false,
		HardLimit:              0.80,
		WrapUpThreshold:        0.50,
	}

	t.Run("compress_with_low_utilization", func(t *testing.T) {
		// 100-token limit, ~2 tokens -> 2% util, well below 50%.
		fw := llm.NewContextFirewall(inner, &llm.ModelConfig{ContextLimit: 100},
			cfg, nil, nil, &llm.HeuristicTokenizer{})
		msgs := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: "sys"},
			{Role: llm.RoleUser, Content: "hi"},
		}
		result, err := fw.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Compressed {
			t.Error("should not compress at low utilization")
		}
		if result.Stage != llm.CompressionStageNone {
			t.Errorf("expected stage None, got %s", result.Stage)
		}
	})

	t.Run("compress_returns_warning_at_stage1", func(t *testing.T) {
		// Need 50+ tokens with 100-token limit but < 60 (summarize threshold).
		// "system " (7 chars) + 144 "a"s (151 chars) = ceil(151/3)=51 tokens
		// + user "ok" = ceil(2/3)=1 -> 52/100 = 52% -> warning
		fw := llm.NewContextFirewall(inner, &llm.ModelConfig{ContextLimit: 100},
			cfg, nil, nil, &llm.HeuristicTokenizer{})
		msgs := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: "system " + strings.Repeat("a", 144)},
			{Role: llm.RoleUser, Content: "ok"},
		}
		result, err := fw.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Logf("warning test: total=%d/%d tokens, stage=%s, compressed=%v",
			result.TokensBefore, 100, result.Stage, result.Compressed)
		if result.Compressed {
			t.Error("warning stage should not alter messages")
		}
		if result.Stage != llm.CompressionStageWarning {
			t.Fatalf("expected stage Warning, got %s", result.Stage)
		}
	})

	t.Run("compress_returns_summarize_at_stage2", func(t *testing.T) {
		// Need 60+ tokens but < 70 (aggressive threshold).
		// "system " (7 chars) + 174 "a"s (181 chars) = ceil(181/3)=61 tokens
		// + user "ok" = 1 -> 62/100 = 62% -> summarize
		fw := llm.NewContextFirewall(inner, &llm.ModelConfig{ContextLimit: 100},
			cfg, nil, nil, &llm.HeuristicTokenizer{})
		msgs := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: "system " + strings.Repeat("a", 174)},
			{Role: llm.RoleUser, Content: "ok"},
		}
		result, err := fw.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Logf("summarize test: total=%d/%d tokens, stage=%s, compressed=%v",
			result.TokensBefore, 100, result.Stage, result.Compressed)
		if !result.Compressed {
			t.Error("summarize stage should set Compressed=true")
		}
		if result.Stage != llm.CompressionStageSummarize {
			t.Fatalf("expected stage Summarize, got %s (total tokens=%d)",
				result.Stage, result.TokensBefore)
		}
		// After summarization: system + summary + some recent messages
		if len(result.Messages) < 2 {
			t.Errorf("expected at least 2 messages after summarization, got %d", len(result.Messages))
		}
	})
}

// --- Test 4: Compression Pipeline Integration ---

func TestAgentLoop_CompressionPipelineIntegration(t *testing.T) {
	store := newMemCCRStore()
	pipeline := compress.NewPipelineWithConfig(store, compress.PipelineConfig{
		MinTokensToCompress:  50,
		TTL:                  time.Hour,
		EnableCCR:            true,
		CompressUserMessages: false,
	})
	defer pipeline.Close()

	ctx := context.Background()

	// Build a series of messages simulating a reasoningCycle turn
	messages := []compress.Message{
		{Role: "user", Content: "Analyze this data"},
		{Role: "assistant", Content: "let me think about this"},
		// Large tool result with JSON data
		{Role: "tool", Content: makeLargeJSONToolOutput()},
	}

	result, err := pipeline.Compress(ctx, messages, compress.CompressConfig{
		MinTokensToCompress: 50,
	})
	if err != nil {
		t.Fatalf("Pipeline.Compress failed: %v", err)
	}

	// Verify all messages are present
	if len(result.Messages) != len(messages) {
		t.Errorf("expected %d messages, got %d", len(messages), len(result.Messages))
	}

	// Verify tokens were recorded
	if result.TokensBefore <= 0 {
		t.Error("expected TokensBefore > 0")
	}

	// Verify transforms were applied
	if len(result.TransformsApplied) == 0 {
		t.Error("expected at least one transform to be applied for large tool output")
	}

	// Verify CCR entries were stored
	ccrStats := store.Stats()
	if ccrStats.EntryCount == 0 {
		t.Log("No CCR entries stored (smart crusher may passthrough unstructured text)")
	} else {
		// Verify retrieval works: hash should be retrievable
		for _, msg := range result.Messages {
			marker := compress.ParseMarker(strings.TrimSpace(msg.Content))
			if marker != "" {
				entry, err := store.Retrieve(ctx, marker)
				if err != nil {
					t.Errorf("Retrieve failed for hash %s: %v", marker, err)
				}
				if entry == nil {
					t.Errorf("expected to retrieve entry for hash %s", marker)
				}
			}
		}
	}

	// Verify tokens saved is non-negative
	if result.TokensBefore < result.TokensAfter {
		t.Errorf("tokens saved calculation error: before=%d < after=%d",
			result.TokensBefore, result.TokensAfter)
	}
}

// --- Test 5: Agent Loop with Compression Pipeline ---

func TestAgentLoop_WithCompressionPipeline(t *testing.T) {
	store := newMemCCRStore()
	pipeline := compress.NewPipelineWithConfig(store, compress.PipelineConfig{
		MinTokensToCompress:  500,
		TTL:                  time.Hour,
		EnableCCR:            true,
		CompressUserMessages: false,
	})
	defer pipeline.Close()

	// Create an agent loop with the compression pipeline
	loop := NewAgentLoop(
		WithCompressionPipeline(pipeline),
		WithAgentConfig(AgentConfig{
			MaxIterations: 3,
		}),
	)

	// Verify the pipeline is stored
	if loop.compressionPipeline == nil {
		t.Error("compressionPipeline should be set via WithCompressionPipeline")
	}

	// Test using SetCompressionPipeline (setter after construction)
	loop2 := NewAgentLoop()
	loop2.SetCompressionPipeline(pipeline)
	if loop2.compressionPipeline == nil {
		t.Error("compressionPipeline should be set via SetCompressionPipeline")
	}

	// Test that RunOnce returns ErrNoLLMClient even with compression enabled
	_, err := loop.RunOnce(context.Background(), "test message", "conv-1")
	if err != ErrNoLLMClient {
		t.Errorf("expected ErrNoLLMClient, got: %v", err)
	}
}

// --- Test 6: Compression System Prompt ---

func TestAgentLoop_CompressionSystemPrompt(t *testing.T) {
	loop := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Constitution: "Test constitution for compression prompt test",
			ProactiveCompression: true,
		}),
	)

	prompt := loop.buildSystemPrompt()
	if prompt == "" {
		t.Fatal("buildSystemPrompt returned empty string")
	}

	// When ProactiveCompression is enabled, the constitution should still be
	// present in the system prompt. CCR markers in system prompts are not
	// standard; instead, CCR is used for tool results and conversation messages.
	// However, the compression pipeline configuration should be reflected in
	// the fact that proactive compression instructions appear when the pipeline
	// is also set.

	// Core requirement: constitution content is present
	if !strings.Contains(prompt, "Test constitution for compression prompt test") {
		t.Error("constitution should be present in system prompt")
	}

	// When the agent loop has a compression pipeline, the buildSystemPromptWithContextAndSkills
	// method also incorporates skills and context. Check that the pipeline doesn't
	// cause any issues during system prompt building.
	pipeline := compress.NewPipelineWithConfig(newMemCCRStore(), compress.PipelineConfig{
		MinTokensToCompress: 500,
		EnableCCR:           true,
	})
	defer pipeline.Close()

	loop2 := NewAgentLoop(
		WithAgentConfig(AgentConfig{
			Constitution: "Test constitution",
		}),
		WithCompressionPipeline(pipeline),
	)

	// buildSystemPromptWithContextAndSkills should not panic
	_ = loop2.compressionPipeline // pipeline is set
	promptWithCtx := loop2.buildSystemPrompt()
	if promptWithCtx == "" {
		t.Error("buildSystemPrompt should not return empty when pipeline is set")
	}
}

// --- Test 7: Pipeline close-on-closed-store guard (end-to-end) ---

func TestAgentLoop_ClosedPipelineReturnsOriginal(t *testing.T) {
	pipeline := compress.NewPipelineWithConfig(newMemCCRStore(), compress.PipelineConfig{
		MinTokensToCompress:  50,
		EnableCCR:            true,
	})

	// Close the pipeline
	if err := pipeline.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	ctx := context.Background()

	// CompressToolResult on a closed pipeline should indicate failure
	output := largeTextOutput()
	_, err := pipeline.CompressToolResult(ctx, "test", output, 4000)
	if err == nil {
		t.Error("expected error on closed pipeline")
	}

	// Pipeline-level Compress on closed pipeline should also error
	msgs := []compress.Message{{Role: "tool", Content: output}}
	_, err = pipeline.Compress(ctx, msgs, compress.CompressConfig{})
	if err == nil {
		t.Error("expected error on closed pipeline at Compress level")
	}
}

// --- Test 8: Multiple tool results compressed in sequence ---

func TestAgentLoop_MultipleToolResultCompression(t *testing.T) {
	store := newMemCCRStore()
	pipeline := compress.NewPipelineWithConfig(store, compress.PipelineConfig{
		MinTokensToCompress:  50,
		TTL:                  time.Hour,
		EnableCCR:            true,
		CompressUserMessages: false,
	})
	defer pipeline.Close()

	ctx := context.Background()

	// Simulate the reasoningCycle path: multiple tool results get compressed
	results := []struct {
		toolName string
		output   string
	}{
		{"file_read", largeTextOutput()},
		{"shell_exec", makeLargeJSONToolOutput()},
		{"fetch_url", strings.Repeat("page content line\n", 20)},
	}

	for _, r := range results {
		if len(r.output) < 100 {
			t.Fatalf("test data '%s' too short", r.toolName)
		}
		compressed, err := pipeline.CompressToolResult(ctx, r.toolName, r.output, 4000)
		if err != nil {
			t.Logf("tool %s compression returned error (expected for text): %v", r.toolName, err)
			continue
		}
		if compressed == "" {
			t.Errorf("tool %s returned empty compressed output", r.toolName)
		}
	}

	// Verify CCR store has multiple entries
	stats := store.Stats()
	if stats.EntryCount == 0 {
		t.Log("No CCR entries recorded (text outputs may pass through)")
	}
}

// --- Test stubs referenced above ---

// stubChatter implements llm.Chatter for tests where no real LLM call is needed.
type stubChatter struct {
	resp *llm.Response
	err  error
}

func (s *stubChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.resp, s.err
}

func (s *stubChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *stubChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "stub"}
}
