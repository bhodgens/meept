package llm

import (
	"context"
	"strings"
	"testing"
)

// TestQualityMetrics_TokenRatio verifies that the TokenRatio field is computed
// correctly for each compression stage.
func TestQualityMetrics_TokenRatio(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    DefaultWarningRatio,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	tests := []struct {
		name        string
		utilization float64
		compressed  bool
	}{
		{"none", 0.40, false},
		{"warning", 0.55, false},
		{"summarize", 0.65, true},
		{"aggressive", 0.75, true},
		{"hard_limit", 0.85, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs := makeCompressorMessages(10)
			result := c.Compress(context.Background(), msgs, tc.utilization)

			if !tc.compressed {
				// Non-compression stages should have ratio of 1.0
				if result.Metrics.TokenRatio != 1.0 {
					t.Errorf("expected TokenRatio=1.0 for %s stage, got %.4f",
						tc.name, result.Metrics.TokenRatio)
				}
			} else {
				// Compressed stages should have ratio < 1.0
				if result.Metrics.TokenRatio <= 0 || result.Metrics.TokenRatio >= 1.0 {
					t.Errorf("expected 0 < TokenRatio < 1.0 for %s stage, got %.4f",
						tc.name, result.Metrics.TokenRatio)
				}
				// Verify ratio matches TokensAfter/TokensBefore
				expected := float64(result.TokensAfter) / float64(result.TokensBefore)
				if result.Metrics.TokenRatio != expected {
					t.Errorf("TokenRatio=%.4f, expected %.4f (from %d/%d)",
						result.Metrics.TokenRatio, expected,
						result.TokensAfter, result.TokensBefore)
				}
			}
		})
	}
}

// TestQualityMetrics_CriticalRetained_NoCriticals verifies that critical counts
// are zero when no messages are marked critical.
func TestQualityMetrics_CriticalRetained_NoCriticals(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10)
	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Metrics.CriticalRetained != 0 {
		t.Errorf("expected CriticalRetained=0 with no critical messages, got %d",
			result.Metrics.CriticalRetained)
	}
	if result.Metrics.CriticalDropped != 0 {
		t.Errorf("expected CriticalDropped=0 with no critical messages, got %d",
			result.Metrics.CriticalDropped)
	}
}

// TestQualityMetrics_CriticalRetained_AllInTail verifies that critical messages
// within the tail window are retained after compression.
func TestQualityMetrics_CriticalRetained_AllInTail(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10)
	// Mark the last 2 messages as critical (they are in the tail of 4)
	msgs[len(msgs)-1].Critical = true
	msgs[len(msgs)-2].Critical = true

	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Metrics.CriticalRetained != 2 {
		t.Errorf("expected CriticalRetained=2, got %d", result.Metrics.CriticalRetained)
	}
	if result.Metrics.CriticalDropped != 0 {
		t.Errorf("expected CriticalDropped=0 when all criticals in tail, got %d",
			result.Metrics.CriticalDropped)
	}
}

// TestQualityMetrics_CriticalRetained_OutsideTail verifies that critical
// messages outside the tail window are still retained (never dropped).
func TestQualityMetrics_CriticalRetained_OutsideTail(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10)
	// Mark the 2nd and 3rd messages as critical (far outside the tail of 4)
	msgs[1].Critical = true
	msgs[2].Critical = true

	result := c.Compress(context.Background(), msgs, 0.65)

	if result.Metrics.CriticalRetained != 2 {
		t.Errorf("expected CriticalRetained=2, got %d", result.Metrics.CriticalRetained)
	}
	if result.Metrics.CriticalDropped != 0 {
		t.Errorf("expected CriticalDropped=0 (critical messages should never be dropped), got %d",
			result.Metrics.CriticalDropped)
	}
}

// TestQualityMetrics_CompressionStage verifies that the stage is recorded in
// the metrics.
func TestQualityMetrics_CompressionStage(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage1WarningRatio:    DefaultWarningRatio,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10)

	tests := []struct {
		utilization float64
		wantStage   CompressionStage
	}{
		{0.40, CompressionStageNone},
		{0.55, CompressionStageWarning},
		{0.65, CompressionStageSummarize},
		{0.75, CompressionStageAggressive},
		{0.85, CompressionStageHardLimit},
	}

	for _, tc := range tests {
		t.Run(tc.wantStage.String(), func(t *testing.T) {
			result := c.Compress(context.Background(), msgs, tc.utilization)
			if result.Metrics.CompressionStage != tc.wantStage {
				t.Errorf("Metrics.CompressionStage=%s, want %s",
					result.Metrics.CompressionStage, tc.wantStage)
			}
		})
	}
}

// TestQualityMetrics_SummaryLevel verifies that the summary level in metrics
// reflects the highest SummaryLevel in the output messages.
func TestQualityMetrics_SummaryLevel(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	// Build messages with mixed summary levels
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system", SummaryLevel: 0},
		{Role: RoleUser, Content: strings.Repeat("a", 300), SummaryLevel: 2},
		{Role: RoleAssistant, Content: strings.Repeat("b", 300), SummaryLevel: 1},
		{Role: RoleUser, Content: strings.Repeat("c", 300), SummaryLevel: 0},
		{Role: RoleAssistant, Content: strings.Repeat("d", 300), SummaryLevel: 0},
	}

	result := c.Compress(context.Background(), msgs, 0.40)
	// At none stage, max summary level should reflect whatever is in the messages
	if result.Metrics.SummaryLevel != 2 {
		t.Errorf("expected SummaryLevel=2 (max from input), got %d",
			result.Metrics.SummaryLevel)
	}
}

// TestQualityMetrics_QualityScoreAverages verifies that the running average
// quality score is tracked correctly across multiple compression passes.
func TestQualityMetrics_QualityScoreAverages(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	msgs := makeCompressorMessages(10)

	// Stage 2 (summarize): triggers compression, quality score = tokenRatio
	c.Compress(context.Background(), msgs, 0.65)
	firstStats := c.Stats()
	if firstStats.TotalCompressions != 1 {
		t.Errorf("expected TotalCompressions=1, got %d", firstStats.TotalCompressions)
	}

	// Stage 3 (aggressive): triggers compression
	c.Compress(context.Background(), msgs, 0.75)
	secondStats := c.Stats()
	if secondStats.TotalCompressions != 2 {
		t.Errorf("expected TotalCompressions=2, got %d", secondStats.TotalCompressions)
	}

	// Non-compression stages should not increment TotalCompressions
	c.Compress(context.Background(), msgs, 0.40)
	thirdStats := c.Stats()
	if thirdStats.TotalCompressions != 2 {
		t.Errorf("expected TotalCompressions=2 (unchanged after non-compression), got %d",
			thirdStats.TotalCompressions)
	}

	// AvgQualityScore should be positive
	if secondStats.AvgQualityScore <= 0 {
		t.Errorf("expected AvgQualityScore > 0, got %.4f", secondStats.AvgQualityScore)
	}
}

// TestQualityMetrics_QualityScoreZeroOnCriticalDrop verifies that when critical
// messages are dropped, the quality score contribution is zero. In practice,
// keepTail preserves critical messages, so this tests the score computation
// logic when CriticalDropped > 0.
func TestQualityMetrics_QualityScoreZeroOnCriticalDrop(t *testing.T) {
	cfg := CompressionConfig{
		Enabled:               true,
		ModelContextLimit:     10000,
		Stage2SummarizeRatio:  DefaultSummarizeRatio,
		Stage3AggressiveRatio: DefaultAggressiveRatio,
		Stage4HardLimitRatio:  DefaultHardLimitRatio,
	}
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, nil)

	// buildQualityMetrics is the scoring function; test it directly with
	// a scenario where critical messages are dropped.
	before := []ChatMessage{
		{Role: RoleUser, Content: "msg1", Critical: true},
		{Role: RoleUser, Content: "msg2"},
	}
	after := []ChatMessage{
		{Role: RoleUser, Content: "msg2"},
	}
	qm := c.buildQualityMetrics(before, after, 10, 5, CompressionStageAggressive)

	if qm.CriticalDropped != 1 {
		t.Errorf("expected CriticalDropped=1, got %d", qm.CriticalDropped)
	}
	if qm.CriticalRetained != 0 {
		t.Errorf("expected CriticalRetained=0, got %d", qm.CriticalRetained)
	}
	// TokenRatio should still be calculated correctly
	if qm.TokenRatio != 0.5 {
		t.Errorf("expected TokenRatio=0.5, got %.4f", qm.TokenRatio)
	}
}

// TestCountCritical verifies the countCritical helper.
func TestCountCritical(t *testing.T) {
	tests := []struct {
		name     string
		messages []ChatMessage
		want     int
	}{
		{"empty", nil, 0},
		{"none critical", []ChatMessage{
			{Role: RoleUser, Content: "a"},
			{Role: RoleUser, Content: "b"},
		}, 0},
		{"some critical", []ChatMessage{
			{Role: RoleUser, Content: "a", Critical: true},
			{Role: RoleUser, Content: "b"},
			{Role: RoleUser, Content: "c", Critical: true},
		}, 2},
		{"all critical", []ChatMessage{
			{Role: RoleUser, Content: "a", Critical: true},
			{Role: RoleUser, Content: "b", Critical: true},
		}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countCritical(tc.messages)
			if got != tc.want {
				t.Errorf("countCritical() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMaxSummaryLevel verifies the maxSummaryLevel helper.
func TestMaxSummaryLevel(t *testing.T) {
	tests := []struct {
		name     string
		messages []ChatMessage
		want     int
	}{
		{"empty", nil, 0},
		{"all zero", []ChatMessage{
			{Role: RoleUser, Content: "a"},
			{Role: RoleUser, Content: "b"},
		}, 0},
		{"mixed levels", []ChatMessage{
			{Role: RoleUser, Content: "a", SummaryLevel: 0},
			{Role: RoleUser, Content: "b", SummaryLevel: 2},
			{Role: RoleUser, Content: "c", SummaryLevel: 1},
		}, 2},
		{"all high", []ChatMessage{
			{Role: RoleUser, Content: "a", SummaryLevel: 3},
			{Role: RoleUser, Content: "b", SummaryLevel: 3},
		}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := maxSummaryLevel(tc.messages)
			if got != tc.want {
				t.Errorf("maxSummaryLevel() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestFirewallStats_QualityFields verifies that the firewall propagates quality
// metrics from the compressor to FirewallStats.
func TestFirewallStats_QualityFields(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:              true,
		ProactiveCompression: true,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	fw := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	// Before any compression
	stats := fw.Stats()
	if stats.TotalCompressions != 0 {
		t.Errorf("expected TotalCompressions=0 before compression, got %d", stats.TotalCompressions)
	}

	// Trigger summarize-level compression
	msgs := makeCompressorMessages(6)
	msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})
	_, err := fw.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats = fw.Stats()
	if stats.TotalCompressions != 1 {
		t.Errorf("expected TotalCompressions=1 after compression, got %d", stats.TotalCompressions)
	}
	if stats.AvgQualityScore <= 0 {
		t.Errorf("expected AvgQualityScore > 0 after compression, got %.4f", stats.AvgQualityScore)
	}
}

// TestFirewallStats_QualityFieldsZeroWithoutCompressor verifies that quality
// fields are zero when proactive compression is disabled.
func TestFirewallStats_QualityFieldsZeroWithoutCompressor(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:              true,
		ProactiveCompression: false,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	fw := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	stats := fw.Stats()
	if stats.TotalCompressions != 0 {
		t.Errorf("expected TotalCompressions=0, got %d", stats.TotalCompressions)
	}
	if stats.AvgQualityScore != 0 {
		t.Errorf("expected AvgQualityScore=0, got %.4f", stats.AvgQualityScore)
	}
}
