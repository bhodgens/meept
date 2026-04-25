package llm

import (
	"context"
	"strings"
	"testing"
)

// makeCompressorMessages builds a message slice with a system message and
// count non-system messages whose token count (heuristic) is tokensPerMsg each.
func makeCompressorMessages(count, tokensPerMsg int) []ChatMessage {
	msgs := []ChatMessage{{Role: RoleSystem, Content: "system prompt"}}
	for i := 0; i < count; i++ {
		msgs = append(msgs, ChatMessage{
			Role:    RoleUser,
			Content: strings.Repeat("x", tokensPerMsg*3),
		})
	}
	return msgs
}

func TestCompress_Disabled(t *testing.T) {
	cfg := CompressionConfig{Enabled: false}
	c := NewContextCompressor(cfg, nil, nil)

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
		name                  string
		utilization           float64
		wantStage             CompressionStage
		wantCompressed        bool
		wantMinMessages       int // minimum number of messages after compression
		statsWarningDelta     uint64
		statsSummarizeDelta   uint64
		statsAggressiveDelta  uint64
		statsHardLimitDelta   uint64
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
			name:               "warning stage",
			utilization:        0.55,
			wantStage:          CompressionStageWarning,
			wantCompressed:     false,
			wantMinMessages:    11, // unchanged
			statsWarningDelta:  1,
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
			wantMinMessages:      3, // 1 system + 2 kept
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
				Enabled:              true,
				ModelContextLimit:    10000,
				Stage1WarningRatio:   DefaultWarningRatio,
				Stage2SummarizeRatio: DefaultSummarizeRatio,
				Stage3AggressiveRatio: DefaultAggressiveRatio,
				Stage4HardLimitRatio:  DefaultHardLimitRatio,
			}
			c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{})

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
			} else {
				if result.DroppedCount != 0 {
					t.Errorf("DroppedCount should be 0 when not compressed, got %d", result.DroppedCount)
				}
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
			c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{})
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
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{})

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
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{})

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
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{})

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
	c := NewContextCompressor(cfg, nil, nil)

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
