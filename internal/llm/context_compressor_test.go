package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// makeCompressorMessages builds a message slice with a system message and
// count non-system messages whose token count (heuristic) is tokensPerMsg each.
func makeCompressorMessages(count, tokensPerMsg int) []ChatMessage {
	msgs := []ChatMessage{{Role: RoleSystem, Content: "system prompt"}}
	for range count {
		msgs = append(msgs, ChatMessage{
			Role:    RoleUser,
			Content: strings.Repeat("x", tokensPerMsg*3),
		})
	}
	return msgs
}

func TestCompress_Disabled(t *testing.T) {
	cfg := CompressionConfig{Enabled: false}
	c := NewContextCompressor(cfg, nil, nil, nil)

	msgs := makeCompressorMessages(10, 100)
	result := c.Compress(context.Background(), msgs, 0.9)

	if result.Compressed {
		t.Error("expected Compressed=false when disabled")
	}
	if result.Stage != CompressionStageNone {
		t.Errorf("expected stage None, got %s", result.Stage)
	}
	if len(result.Messages) != len(msgs) {
		t.Error("messages should be unchanged when disabled")
	}
}

func TestCompress_AllStages(t *testing.T) {
	tests := []struct {
		name                 string
		utilization          float64
		wantStage            CompressionStage
		wantCompressed       bool
		wantMinMessages      int // minimum number of messages after compression
		statsWarningDelta    uint64
		statsSummarizeDelta  uint64
		statsAggressiveDelta uint64
		statsHardLimitDelta  uint64
	}{
		{
			name:              "below warning",
			utilization:       0.40,
			wantStage:         CompressionStageNone,
			wantCompressed:    false,
			wantMinMessages:   11, // unchanged (1 system + 10 user)
			statsWarningDelta: 0,
		},
		{
			name:              "warning stage",
			utilization:       0.55,
			wantStage:         CompressionStageWarning,
			wantCompressed:    false,
			wantMinMessages:   11, // unchanged
			statsWarningDelta: 1,
		},
		{
			name:                "summarize stage",
			utilization:         0.65,
			wantStage:           CompressionStageSummarize,
			wantCompressed:      true,
			wantMinMessages:     5, // 1 system + 4 kept
			statsSummarizeDelta: 1,
		},
		{
			name:                 "aggressive stage",
			utilization:          0.75,
			wantStage:            CompressionStageAggressive,
			wantCompressed:       true,
			wantMinMessages:      5, // 1 system + 4 kept
			statsAggressiveDelta: 1,
		},
		{
			name:                "hard limit stage",
			utilization:         0.85,
			wantStage:           CompressionStageHardLimit,
			wantCompressed:      true,
			wantMinMessages:     3, // 1 system + 2 kept
			statsHardLimitDelta: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := CompressionConfig{
				Enabled:               true,
				ModelContextLimit:     10000,
				Stage1WarningRatio:    DefaultWarningRatio,
				Stage2SummarizeRatio:  DefaultSummarizeRatio,
				Stage3AggressiveRatio: DefaultAggressiveRatio,
				Stage4HardLimitRatio:  DefaultHardLimitRatio,
			}
			c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

			msgs := makeCompressorMessages(10, 100) // 1 system + 10 user messages
			before := len(msgs)

			result := c.Compress(context.Background(), msgs, tc.utilization)

			if result.Stage != tc.wantStage {
				t.Errorf("stage: got %s, want %s", result.Stage, tc.wantStage)
			}
			if result.Compressed != tc.wantCompressed {
				t.Errorf("compressed: got %v, want %v", result.Compressed, tc.wantCompressed)
			}
			if len(result.Messages) != tc.wantMinMessages {
				t.Errorf("message count: got %d, want %d", len(result.Messages), tc.wantMinMessages)
			}
			if result.TokensBefore <= 0 {
				t.Error("TokensBefore should be positive")
			}

			if tc.wantCompressed {
				if result.TokensAfter >= result.TokensBefore {
					t.Errorf("TokensAfter (%d) should be less than TokensBefore (%d) when compressed",
						result.TokensAfter, result.TokensBefore)
				}
				if result.DroppedCount != before-len(result.Messages) {
					t.Errorf("DroppedCount: got %d, want %d",
						result.DroppedCount, before-len(result.Messages))
				}
			} else if result.DroppedCount != 0 {
				t.Errorf("DroppedCount should be 0 when not compressed, got %d", result.DroppedCount)
			}

			// Verify stats counters.
			stats := c.Stats()
			if stats.WarningEvents != tc.statsWarningDelta {
				t.Errorf("WarningEvents: got %d, want %d", stats.WarningEvents, tc.statsWarningDelta)
			}
			if stats.SummarizeEvents != tc.statsSummarizeDelta {
				t.Errorf("SummarizeEvents: got %d, want %d", stats.SummarizeEvents, tc.statsSummarizeDelta)
			}
			if stats.AggressiveEvents != tc.statsAggressiveDelta {
				t.Errorf("AggressiveEvents: got %d, want %d", stats.AggressiveEvents, tc.statsAggressiveDelta)
			}
			if stats.HardLimitEvents != tc.statsHardLimitDelta {
				t.Errorf("HardLimitEvents: got %d, want %d", stats.HardLimitEvents, tc.statsHardLimitDelta)
			}
		})
	}
}

func TestCompress_ExactThresholds(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    0.50,
		Stage2SummarizeRatio:  0.60,
		Stage3AggressiveRatio: 0.70,
		Stage4HardLimitRatio:  0.80,
	}

	tests := []struct {
		name        string
		utilization float64
		wantStage   CompressionStage
	}{
		{"at warning boundary", 0.50, CompressionStageWarning},
		{"at summarize boundary", 0.60, CompressionStageSummarize},
		{"at aggressive boundary", 0.70, CompressionStageAggressive},
		{"at hard limit boundary", 0.80, CompressionStageHardLimit},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)
			msgs := makeCompressorMessages(10, 100)
			result := c.Compress(context.Background(), msgs, tc.utilization)
			if result.Stage != tc.wantStage {
				t.Errorf("at utilization %.2f: got stage %s, want %s",
					tc.utilization, result.Stage, tc.wantStage)
			}
		})
	}
}

func TestCompress_SystemMessagesPreserved(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    0.50,
		Stage2SummarizeRatio:  0.60,
		Stage3AggressiveRatio: 0.70,
		Stage4HardLimitRatio:  0.80,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system 1"},
		{Role: RoleSystem, Content: "system 2"},
		{Role: RoleUser, Content: "user 1"},
		{Role: RoleAssistant, Content: "assistant 1"},
		{Role: RoleUser, Content: "user 2"},
		{Role: RoleAssistant, Content: "assistant 2"},
		{Role: RoleUser, Content: "user 3"},
		{Role: RoleAssistant, Content: "assistant 3"},
	}

	// Summarize stage: keep system + last 4 non-system
	result := c.Compress(context.Background(), msgs, 0.65)
	systemCount := 0
	for _, m := range result.Messages {
		if m.Role == RoleSystem {
			systemCount++
		}
	}
	if systemCount != 2 {
		t.Errorf("expected 2 system messages after summarize, got %d", systemCount)
	}

	// Hard limit stage: keep system + last 2 non-system
	result = c.Compress(context.Background(), msgs, 0.90)
	systemCount = 0
	for _, m := range result.Messages {
		if m.Role == RoleSystem {
			systemCount++
		}
	}
	if systemCount != 2 {
		t.Errorf("expected 2 system messages after hard limit, got %d", systemCount)
	}
}

func TestCompress_TokensSavedAccumulates(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    0.50,
		Stage2SummarizeRatio:  0.60,
		Stage3AggressiveRatio: 0.70,
		Stage4HardLimitRatio:  0.80,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10, 100)

	// First compression at summarize level.
	c.Compress(context.Background(), msgs, 0.65)
	first := c.Stats().TotalTokensSaved

	// Second compression at aggressive level.
	c.Compress(context.Background(), msgs, 0.75)
	second := c.Stats().TotalTokensSaved

	if second <= first {
		t.Errorf("TotalTokensSaved should accumulate: first=%d, second=%d", first, second)
	}
}

func TestCompress_ShortMessageList(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    0.50,
		Stage2SummarizeRatio:  0.60,
		Stage3AggressiveRatio: 0.70,
		Stage4HardLimitRatio:  0.80,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	// Only 1 system + 1 user -- fewer than any keep count.
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "hello"},
	}

	result := c.Compress(context.Background(), msgs, 0.90)
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages (nothing to drop), got %d", len(result.Messages))
	}
	if result.DroppedCount != 0 {
		t.Errorf("expected DroppedCount=0, got %d", result.DroppedCount)
	}
}

func TestCompress_DefaultRatiosApplied(t *testing.T) {
	cfg := CompressionConfig{Enabled: true} // all ratios zero
	c := NewContextCompressor(cfg, nil, nil, nil)

	if c.config.Stage1WarningRatio != DefaultWarningRatio {
		t.Errorf("Stage1WarningRatio: got %f, want %f", c.config.Stage1WarningRatio, DefaultWarningRatio)
	}
	if c.config.Stage2SummarizeRatio != DefaultSummarizeRatio {
		t.Errorf("Stage2SummarizeRatio: got %f, want %f", c.config.Stage2SummarizeRatio, DefaultSummarizeRatio)
	}
	if c.config.Stage3AggressiveRatio != DefaultAggressiveRatio {
		t.Errorf("Stage3AggressiveRatio: got %f, want %f", c.config.Stage3AggressiveRatio, DefaultAggressiveRatio)
	}
	if c.config.Stage4HardLimitRatio != DefaultHardLimitRatio {
		t.Errorf("Stage4HardLimitRatio: got %f, want %f", c.config.Stage4HardLimitRatio, DefaultHardLimitRatio)
	}
}

func TestCompressionStage_String(t *testing.T) {
	tests := []struct {
		stage CompressionStage
		want  string
	}{
		{CompressionStageNone, "none"},
		{CompressionStageWarning, "warning"},
		{CompressionStageSummarize, "summarize"},
		{CompressionStageAggressive, "aggressive"},
		{CompressionStageHardLimit, "hard_limit"},
		{CompressionStage(99), "unknown(99)"},
	}
	for _, tc := range tests {
		if got := tc.stage.String(); got != tc.want {
			t.Errorf("CompressionStage(%d).String() = %q, want %q", tc.stage, got, tc.want)
		}
	}
}

// mockChatter is a test mock that implements Chatter for LLM summarization tests.
type mockChatter struct {
	response *Response
	err      error
	called   bool
}

func (m *mockChatter) Chat(_ context.Context, messages []ChatMessage, _ ...ChatOption) (*Response, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockChatter) ChatWithProgress(_ context.Context, messages []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(context.Background(), messages)
}

func (m *mockChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// TestSummarizeOldHistory_WithLLM verifies that when a summarizer is available,
// summarizeOldHistory produces a summary message plus the tail of recent messages.
func TestSummarizeOldHistory_WithLLM(t *testing.T) {
	mock := &mockChatter{
		response: &Response{Content: "The user discussed file parsing and decided to use tree-sitter."},
	}
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, mock)

	// 1 system + 8 user messages
	msgs := makeCompressorMessages(8, 100)
	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Stage != CompressionStageSummarize {
		t.Fatalf("expected Summarize stage, got %s", result.Stage)
	}
	if !mock.called {
		t.Error("expected summarizer Chat to be called")
	}

	// Result should be: system msgs + summary msg + last 4 user msgs = 6
	if len(result.Messages) != 6 {
		t.Errorf("expected 6 messages (1 system + 1 summary + 4 tail), got %d", len(result.Messages))
	}

	// Find the summary message (should be system role, marked critical)
	foundSummary := false
	for _, msg := range result.Messages {
		if msg.Role == RoleSystem && msg.Critical && strings.Contains(msg.Content, "[Conversation Summary]") {
			foundSummary = true
			if !strings.Contains(msg.Content, "tree-sitter") {
				t.Error("summary content should contain the LLM response text")
			}
		}
	}
	if !foundSummary {
		t.Error("expected to find a summary system message marked critical")
	}

	// Last 4 messages should be the original user messages
	lastFour := result.Messages[len(result.Messages)-4:]
	for i, msg := range lastFour {
		if msg.Role != RoleUser {
			t.Errorf("tail message %d: expected user role, got %s", i, msg.Role)
		}
	}
}

// TestSummarizeOldHistory_FallbackOnLLMError verifies that when the summarizer
// returns an error, summarizeOldHistory falls back to keepTail(4).
func TestSummarizeOldHistory_FallbackOnLLMError(t *testing.T) {
	mock := &mockChatter{
		err: fmt.Errorf("LLM unavailable"),
	}
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, mock)

	msgs := makeCompressorMessages(8, 100)
	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Stage != CompressionStageSummarize {
		t.Fatalf("expected Summarize stage, got %s", result.Stage)
	}
	if !mock.called {
		t.Error("expected summarizer Chat to be attempted")
	}

	// Fallback to keepTail(4): 1 system + 4 user = 5
	if len(result.Messages) != 5 {
		t.Errorf("expected 5 messages on fallback (1 system + 4 tail), got %d", len(result.Messages))
	}
}

// TestSummarizeOldHistory_NilSummarizer verifies that when summarizer is nil,
// summarizeOldHistory falls back to keepTail(4) without attempting an LLM call.
func TestSummarizeOldHistory_NilSummarizer(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(8, 100)
	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Stage != CompressionStageSummarize {
		t.Fatalf("expected Summarize stage, got %s", result.Stage)
	}

	// keepTail(4): 1 system + 4 user = 5
	if len(result.Messages) != 5 {
		t.Errorf("expected 5 messages (1 system + 4 tail), got %d", len(result.Messages))
	}
}

// TestAggressiveCompress_PreservesCritical verifies that aggressiveCompress
// keeps critical messages that fall outside the tail window.
func TestAggressiveCompress_PreservesCritical(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10, 100)
	// Mark messages outside the tail of 4 as critical
	msgs[1].Critical = true
	msgs[2].Critical = true

	result := c.Compress(context.Background(), msgs, 0.75)

	if result.Stage != CompressionStageAggressive {
		t.Fatalf("expected Aggressive stage, got %s", result.Stage)
	}

	// Should have: 1 system + 2 critical outside tail + 4 tail = 7
	if len(result.Messages) != 7 {
		t.Errorf("expected 7 messages (1 system + 2 critical + 4 tail), got %d", len(result.Messages))
	}

	// Verify critical messages are retained
	criticalCount := 0
	for _, msg := range result.Messages {
		if msg.Critical {
			criticalCount++
		}
	}
	if criticalCount != 2 {
		t.Errorf("expected 2 critical messages retained, got %d", criticalCount)
	}
}

// TestCompressionStages_Differentiate verifies that stages 2 (summarize),
// 3 (aggressive), and 4 (hard limit) produce different output sizes.
func TestCompressionStages_Differentiate(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    DefaultWarningRatio,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10, 100) // 1 system + 10 user = 11

	// Stage 2: summarize -> keepTail(4) = 1 system + 4 user = 5
	r2 := c.Compress(context.Background(), msgs, 0.65)
	// Stage 3: aggressive -> keepTail(4) = 1 system + 4 user = 5
	r3 := c.Compress(context.Background(), msgs, 0.75)
	// Stage 4: hard limit -> keepTail(2) = 1 system + 2 user = 3
	r4 := c.Compress(context.Background(), msgs, 0.85)

	if r2.Stage != CompressionStageSummarize {
		t.Errorf("stage 2: expected Summarize, got %s", r2.Stage)
	}
	if r3.Stage != CompressionStageAggressive {
		t.Errorf("stage 3: expected Aggressive, got %s", r3.Stage)
	}
	if r4.Stage != CompressionStageHardLimit {
		t.Errorf("stage 4: expected HardLimit, got %s", r4.Stage)
	}

	// Stages 2 and 3 keep the same count (both use keepTail(4)) but stage 4
	// keeps fewer (keepTail(2)).
	if len(r2.Messages) != 5 {
		t.Errorf("stage 2: expected 5 messages, got %d", len(r2.Messages))
	}
	if len(r3.Messages) != 5 {
		t.Errorf("stage 3: expected 5 messages, got %d", len(r3.Messages))
	}
	if len(r4.Messages) != 3 {
		t.Errorf("stage 4: expected 3 messages, got %d", len(r4.Messages))
	}

	// Stage 4 should produce fewer messages than stage 3
	if len(r4.Messages) >= len(r3.Messages) {
		t.Error("hard limit should produce fewer messages than aggressive")
	}
}
