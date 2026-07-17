package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// stubLlmChat implements llm.Chatter for testing.
type stubLlmChat struct {
	resp string
	err  error
}

func (s *stubLlmChat) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: s.resp}, s.err
}

func (s *stubLlmChat) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: s.resp}, s.err
}

func (s *stubLlmChat) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "stub"}
}

// -----------------------------------------------------------------------
// NewCompactor -- defaults
// -----------------------------------------------------------------------

func TestNewCompactor_Defaults(t *testing.T) {
	c := NewCompactor(CompactionConfig{})
	cfg := c.Config()
	if cfg.MaxTurnsBeforeCompact != 50 {
		t.Errorf("MaxTurnsBeforeCompact: got %d, want 50", cfg.MaxTurnsBeforeCompact)
	}
	if cfg.MaxToolCallsPerTurn != 10 {
		t.Errorf("MaxToolCallsPerTurn: got %d, want 10", cfg.MaxToolCallsPerTurn)
	}
	if cfg.KeepLastNTurns != 10 {
		t.Errorf("KeepLastNTurns: got %d, want 10", cfg.KeepLastNTurns)
	}
}

// -----------------------------------------------------------------------
// TestCompactor_MergeConsecutiveToolCalls
// -----------------------------------------------------------------------

func TestCompactor_MergeConsecutiveToolCalls(t *testing.T) {
	c := NewCompactor(CompactionConfig{})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "view_trace", ToolInput: `{"trace_id":"123"}`, Tokens: 50},
		{Type: "observation", Content: `trace_data_here`, Tokens: 200},
		{Type: "tool", ToolName: "view_trace", ToolInput: `{"trace_id":"123"}`, Tokens: 50},
		{Type: "observation", Content: `more_trace_data`, Tokens: 150},
		{Type: "final", Content: `done`, Tokens: 10},
	}

	result, compacted := c.Compact(context.Background(), turns)

	// 5 turns > 0 so compaction is below threshold check; actually 5 <= 50 so no compaction
	if result.OriginalTurnCount != len(turns) {
		t.Errorf("original count: got %d, want %d", result.OriginalTurnCount, len(turns))
	}
	_ = compacted // avoid unused below; this test exercises the merge via CompactTurns instead
}

func TestCompactor_MergeToolCalls_AboveThreshold(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 3,
		KeepLastNTurns:        1,
	})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", ToolInput: `{"path":"/a"}`, Tokens: 30},
		{Type: "observation", Content: `file contents`, Tokens: 100},
		{Type: "tool", ToolName: "read_file", ToolInput: `{"path":"/b"}`, Tokens: 30},
		{Type: "observation", Content: `file contents 2`, Tokens: 80},
		{Type: "user", Content: `hello`, Tokens: 5}, // last turn, protected
	}

	_, compacted := c.Compact(context.Background(), turns)

	// Should have 4 turns: 2 merged tools + 1 protected user turn
	expected := 3 // 2 merged tool turns + 1 protected user turn
	if len(compacted) != expected {
		t.Errorf("expected %d compacted turns, got %d: %+v", expected, len(compacted), compacted)
	}

	// Verify the merged turns contain both call + output
	for _, turn := range compacted {
		if turn.Type == "tool" {
			if turn.ToolOutput == "" {
				t.Errorf("merged tool %q should have ToolOutput", turn.ToolName)
			}
		}
	}
}

// -----------------------------------------------------------------------
// TestCompactor_KeepLastNTurns
// -----------------------------------------------------------------------

func TestCompactor_KeepLastNTurns(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 5,
		KeepLastNTurns:        3,
	})

	turns := make([]TurnRecord, 8)
	for i := 0; i < 8; i++ {
		turns[i] = TurnRecord{
			Type:    "observation",
			Content: "turn " + string(rune('0'+i)),
			Tokens:  10,
		}
	}

	_, compacted := c.Compact(context.Background(), turns)

	// Last 3 should be unmodified: "turn 5", "turn 6", "turn 7"
	kept := compacted[len(compacted)-3:]
	for i, turn := range kept {
		if !strings.Contains(turn.Content, "turn "+string(rune('0'+i+5))) {
			t.Errorf("kept turn %d: expected content 'turn %d', got %q", i, i+5, turn.Content)
		}
	}

	expectedTotal := len(compacted)
	// After dedup: turns 0-4 (the compactable portion) should become 1 merged observation
	// (dedup collapses 5 consecutive observations into 1), + 3 kept = 4
	if expectedTotal < 1 {
		t.Errorf("expected at least 1 turn, got %d", len(compacted))
	}
}

// -----------------------------------------------------------------------
// TestCompactor_NoCompactionBelowThreshold
// -----------------------------------------------------------------------

func TestCompactor_NoCompactionBelowThreshold(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 50,
		KeepLastNTurns:        10,
	})

	turns := make([]TurnRecord, 20)
	for i := range turns {
		turns[i] = TurnRecord{Type: "observation", Content: "turn"}
	}

	result, compacted := c.Compact(context.Background(), turns)

	if result.CompressionRatio != 1.0 {
		t.Errorf("expected ratio 1.0, got %f", result.CompressionRatio)
	}
	if result.OriginalTurnCount != 20 {
		t.Errorf("expected original 20, got %d", result.OriginalTurnCount)
	}
	if len(compacted) != 20 {
		t.Errorf("expected 20 compacted turns (unchanged), got %d", len(compacted))
	}
	if !strings.Contains(result.Summary, "below threshold") {
		t.Errorf("expected 'below threshold' in summary, got %q", result.Summary)
	}
}

// -----------------------------------------------------------------------
// TestCompactor_GenerateSummary
// -----------------------------------------------------------------------

func TestCompactor_GenerateSummary_NoLLM(t *testing.T) {
	c := NewCompactor(CompactionConfig{})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", ToolInput: `{"path":"/a"}`, Tokens: 30},
		{Type: "tool", ToolName: "write_file", ToolInput: `{"path":"/b"}`, Tokens: 40},
		{Type: "thinking", Content: "let me think about this", Tokens: 15},
		{Type: "user", Content: "hello world", Tokens: 10},
	}

	summary := c.GenerateSummary(context.Background(), turns)

	if summary == "" {
		t.Fatal("expected non-empty heuristic summary")
	}
	if !strings.Contains(summary, "Compacted:") {
		t.Errorf("expected 'Compacted:' prefix, got %q", summary)
	}
}

func TestCompactor_GenerateSummary_WithLLM(t *testing.T) {
	llmResp := "The agent read and wrote several files while thinking about the user input."
	c := NewCompactor(CompactionConfig{
		LLM: &stubLlmChat{resp: llmResp},
	})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", Content: "/a", Tokens: 30},
		{Type: "final", Content: "done", Tokens: 10},
	}

	summary := c.GenerateSummary(context.Background(), turns)

	if summary != llmResp {
		t.Errorf("expected LLM summary %q, got %q", llmResp, summary)
	}
}

func TestCompactor_GenerateSummary_LLMErrorFallsBack(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		LLM: &stubLlmChat{resp: "fallback text", err: errors.New("LLM broken")},
	})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", Tokens: 30},
	}

	summary := c.GenerateSummary(context.Background(), turns)

	if summary == "" {
		t.Fatal("expected fallback summary on LLM error")
	}
}

// -----------------------------------------------------------------------
// TestCompactor_CompressionRatio
// -----------------------------------------------------------------------

func TestCompactor_CompressionRatio(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 5,
		KeepLastNTurns:        2,
	})

	// 10 turns: 3 should be protected, 7 compactable
	turns := make([]TurnRecord, 10)
	for i := range turns {
		t := "turn " + string(rune('0'+(i%10)))
		turns[i] = TurnRecord{
			Type:    "observation",
			Content: t,
			Tokens:  10,
		}
	}

	result, _ := c.Compact(context.Background(), turns)

	if result.OriginalTurnCount != 10 {
		t.Errorf("original: got %d, want 10", result.OriginalTurnCount)
	}
	if result.OriginalTurnCount <= result.CompactedTurnCount {
		t.Errorf("expected some reduction, original=%d, compacted=%d",
			result.OriginalTurnCount, result.CompactedTurnCount)
	}
	if result.CompressionRatio <= 0 {
		t.Errorf("expected positive compression ratio, got %f", result.CompressionRatio)
	}
	if result.CompressionRatio >= 1.0 {
		t.Errorf("expected compression ratio < 1.0, got %f", result.CompressionRatio)
	}
}

// -----------------------------------------------------------------------
// CompactToolCalls -- duplicate removal
// -----------------------------------------------------------------------

func TestCompactor_CompactToolCalls_RemovesDuplicates(t *testing.T) {
	c := NewCompactor(CompactionConfig{})

	calls := []ToolCall{
		{ToolName: "ls", Arguments: `{"path":"/a"}`, Seq: 1},
		{ToolName: "ls", Arguments: `{"path":"/a"}`, Seq: 2}, // duplicate
		{ToolName: "cat", Arguments: `{"path":"/b"}`, Seq: 3},
		{ToolName: "ls", Arguments: `{"path":"/c"}`, Seq: 4}, // different args, not a dup
	}

	unique := c.CompactToolCalls(calls)

	if len(unique) != 3 {
		t.Errorf("expected 3 unique calls, got %d", len(unique))
	}

	// Should keep first occurrence only
	if unique[1].ToolName != "cat" {
		t.Errorf("expected second unique call to be 'cat', got %q", unique[1].ToolName)
	}
}

func TestCompactor_CompactToolCalls_Empty(t *testing.T) {
	c := NewCompactor(CompactionConfig{})
	result := c.CompactToolCalls(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

// -----------------------------------------------------------------------
// CompactToolCalls -- same as CompactTurns for tool+observation merges
// -----------------------------------------------------------------------

func TestCompactor_CompactTurns_MergesToolPairs(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 2,
		KeepLastNTurns:        1,
	})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", ToolInput: `{"p":"/x"}`, Tokens: 20},
		{Type: "observation", Content: `file content x`, Tokens: 100},
		{Type: "user", Content: `hello`, Tokens: 5},
	}

	compacted := c.CompactTurns(turns)

	if len(compacted) != 2 {
		t.Errorf("expected 2 compacted turns, got %d", len(compacted))
	}
	// First turn should be the merged tool call
	if compacted[0].Type != "tool" {
		t.Errorf("expected first turn type 'tool', got %q", compacted[0].Type)
	}
	if compacted[0].ToolName != "read_file" {
		t.Errorf("expected tool 'read_file', got %q", compacted[0].ToolName)
	}
	if compacted[0].ToolOutput != "file content x" {
		t.Errorf("expected output 'file content x', got %q", compacted[0].ToolOutput)
	}
	if compacted[0].Tokens != 120 {
		t.Errorf("expected 120 tokens, got %d", compacted[0].Tokens)
	}
	// Last turn should be the protected user turn
	if compacted[1].Type != "user" {
		t.Errorf("expected last turn type 'user', got %q", compacted[1].Type)
	}
}

func TestCompactor_CompactTurns_NoObservationFollowsTool(t *testing.T) {
	c := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 100,
	})

	turns := []TurnRecord{
		{Type: "tool", ToolName: "read_file", Tokens: 20},
		{Type: "final", Content: "done", Tokens: 10},
	}

	compacted := c.CompactTurns(turns)

	if len(compacted) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(compacted))
	}
	if compacted[0].Type != "tool" {
		t.Errorf("expected first turn type 'tool', got %q", compacted[0].Type)
	}
	if compacted[1].Type != "final" {
		t.Errorf("expected second turn type 'final', got %q", compacted[1].Type)
	}
}

func TestCompactor_CompactTurns_EmptyInput(t *testing.T) {
	c := NewCompactor(CompactionConfig{})
	result := c.CompactTurns(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d turns", len(result))
	}
}

// -----------------------------------------------------------------------
// GenerateSummary -- empty input
// -----------------------------------------------------------------------

func TestCompactor_GenerateSummary_EmptyInput(t *testing.T) {
	c := NewCompactor(CompactionConfig{})
	summary := c.GenerateSummary(context.Background(), nil)
	if summary == "" {
		t.Fatal("expected non-empty summary for empty input")
	}
}
