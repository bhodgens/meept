package metrics

import (
	"testing"
	"time"
)

// TestTokscaleIngestProjection pins the read surface the tokscale CLI
// parser will issue against ~/.meept/metrics.db
// (docs/plans/20260906-tokscale-ingest/master.md, Contract D). If this
// test breaks, tokscale's parser breaks — change Contract D in the
// master plan, not silently here.
func TestTokscaleIngestProjection(t *testing.T) {
	store := newLLMSchemaTestStore(t)

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	rows := []LLMCallRecord{
		// Session A, claude-test: two calls (one with cache + reasoning)
		{Timestamp: base, Provider: "anthropic", ModelID: "claude-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 1000, TokensRecv: 500, TokensCached: 200,
			ReasoningTokens: 100, CacheCreationTokens: 150},
		{Timestamp: base.Add(time.Minute), Provider: "anthropic", ModelID: "claude-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 800, TokensRecv: 300},
		// Session A, second model
		{Timestamp: base.Add(2 * time.Minute), Provider: "anthropic", ModelID: "other-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 100, TokensRecv: 40},
		// Session B
		{Timestamp: base.Add(3 * time.Minute), Provider: "openai", ModelID: "gpt-test", AgentID: "reviewer",
			SessionID: "convB", TokensSent: 50, TokensRecv: 25},
		// Error row: usage zero, must be excluded by the projection
		{Timestamp: base.Add(4 * time.Minute), Provider: "openai", ModelID: "gpt-test", AgentID: "reviewer",
			SessionID: "convB", IsError: true, ErrorMessage: "boom"},
	}
	for _, r := range rows {
		store.RecordLLMCall(r)
	}

	// Contract D projection: per (session_id, model_id) sums, error rows excluded.
	type projRow struct {
		SessionID        string `db:"session_id"`
		ModelID          string `db:"model_id"`
		InputTokens      int    `db:"input_tokens"`
		OutputTokens     int    `db:"output_tokens"`
		CacheReadTokens  int    `db:"cache_read_tokens"`
		CacheWriteTokens int    `db:"cache_write_tokens"`
		ReasoningTokens  int    `db:"reasoning_tokens"`
	}
	want := map[string]projRow{
		"convA|claude-test": {SessionID: "convA", ModelID: "claude-test",
			InputTokens: 1800, OutputTokens: 800, CacheReadTokens: 200,
			CacheWriteTokens: 150, ReasoningTokens: 100},
		"convA|other-test": {SessionID: "convA", ModelID: "other-test",
			InputTokens: 100, OutputTokens: 40},
		"convB|gpt-test": {SessionID: "convB", ModelID: "gpt-test",
			InputTokens: 50, OutputTokens: 25},
	}

	var got []projRow
	if err := store.db.Select(&got, `
		SELECT session_id, model_id,
		       SUM(tokens_sent)           AS input_tokens,
		       SUM(tokens_received)       AS output_tokens,
		       SUM(tokens_cached)         AS cache_read_tokens,
		       SUM(cache_creation_tokens) AS cache_write_tokens,
		       SUM(reasoning_tokens)      AS reasoning_tokens
		FROM llm_calls
		WHERE error = 0
		GROUP BY session_id, model_id
		ORDER BY session_id, model_id`); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("projection rows = %d, want %d: %+v", len(got), len(want), got)
	}
	for _, g := range got {
		key := g.SessionID + "|" + g.ModelID
		w, ok := want[key]
		if !ok {
			t.Fatalf("unexpected projection key %q", key)
		}
		if g != w {
			t.Fatalf("%s: got %+v, want %+v", key, g, w)
		}
	}
}
