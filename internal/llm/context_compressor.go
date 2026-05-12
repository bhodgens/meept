package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
)

// CompressionStage represents the severity of context compression applied.
type CompressionStage int

const (
	CompressionStageNone       CompressionStage = iota // No compression needed
	CompressionStageWarning                            // Context is filling up; log a warning only
	CompressionStageSummarize                          // LLM-summarize old history, keep system + summary + last 4
	CompressionStageAggressive                         // Drop low-importance messages (keep system + critical + last 4)
	CompressionStageHardLimit                          // Drop old context entirely (keep system + last 2)
)

// String returns a human-readable label for the compression stage.
func (s CompressionStage) String() string {
	switch s {
	case CompressionStageNone:
		return "none"
	case CompressionStageWarning:
		return "warning"
	case CompressionStageSummarize:
		return "summarize"
	case CompressionStageAggressive:
		return "aggressive"
	case CompressionStageHardLimit:
		return "hard_limit"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// CompressionConfig configures the multi-stage context compressor.
type CompressionConfig struct {
	Enabled               bool    // Master switch
	ModelContextLimit     int     // Total context tokens for the model
	Stage1WarningRatio    float64 // Utilization threshold for warning (default 0.50)
	Stage2SummarizeRatio  float64 // Utilization threshold for summarization (default 0.60)
	Stage3AggressiveRatio float64 // Utilization threshold for aggressive compression (default 0.70)
	Stage4HardLimitRatio  float64 // Utilization threshold for hard limit (default 0.80)
}

// Defaults.
const (
	DefaultWarningRatio    = 0.50
	DefaultSummarizeRatio  = 0.60
	DefaultAggressiveRatio = 0.70
	DefaultHardLimitRatio  = 0.80
)

// QualityMetrics tracks compression quality for a single compression pass.
type QualityMetrics struct {
	TokenRatio       float64          // tokensAfter / tokensBefore (1.0 when no compression)
	CriticalRetained int              // Count of critical messages kept
	CriticalDropped  int              // Count of critical messages dropped (should always be 0)
	SummaryLevel     int              // Highest summary level in the output messages
	CompressionStage CompressionStage // Stage that produced this result
}

// CompressionResult holds the outcome of a compression pass.
type CompressionResult struct {
	Messages     []ChatMessage
	Compressed   bool
	Stage        CompressionStage
	TokensBefore int
	TokensAfter  int
	DroppedCount int
	Metrics      QualityMetrics
}

// CompressionStats tracks cumulative compression events with atomic-safe counters.
type CompressionStats struct {
	WarningEvents     atomic.Uint64
	SummarizeEvents   atomic.Uint64
	AggressiveEvents  atomic.Uint64
	HardLimitEvents   atomic.Uint64
	TotalTokensSaved  atomic.Uint64
	TotalCompressions atomic.Uint64
	QualityScoreSum   atomic.Uint64 // Scaled by 1000 to use uint64 for float accumulation
	QualityScoreCount atomic.Uint64
}

// Snapshot returns a point-in-time copy of the stats fields as plain values.
type CompressionStatsSnapshot struct {
	WarningEvents     uint64
	SummarizeEvents   uint64
	AggressiveEvents  uint64
	HardLimitEvents   uint64
	TotalTokensSaved  uint64
	TotalCompressions uint64
	AvgQualityScore   float64 // Running average of quality scores (0.0-1.0)
}

// Snapshot reads all atomic counters and returns a plain-value snapshot.
func (s *CompressionStats) Snapshot() CompressionStatsSnapshot {
	count := s.QualityScoreCount.Load()
	var avg float64
	if count > 0 {
		avg = float64(s.QualityScoreSum.Load()) / float64(count) / 1000.0
	}
	return CompressionStatsSnapshot{
		WarningEvents:     s.WarningEvents.Load(),
		SummarizeEvents:   s.SummarizeEvents.Load(),
		AggressiveEvents:  s.AggressiveEvents.Load(),
		HardLimitEvents:   s.HardLimitEvents.Load(),
		TotalTokensSaved:  s.TotalTokensSaved.Load(),
		TotalCompressions: s.TotalCompressions.Load(),
		AvgQualityScore:   avg,
	}
}

// ContextCompressor implements multi-stage context compression based on
// utilization thresholds. It is safe for concurrent use.
type ContextCompressor struct {
	config     CompressionConfig
	stats      CompressionStats
	summarizer Chatter // Optional: when set, enables LLM-based summarization at stage 2
	logger     *slog.Logger
	tokenizer  Tokenizer
}

// NewContextCompressor creates a new compressor with the given configuration.
// If logger is nil, slog.Default() is used. If tokenizer is nil,
// HeuristicTokenizer is used. summarizer may be nil; when nil, summarization
// stages fall back to tail-keep truncation.
func NewContextCompressor(cfg CompressionConfig, logger *slog.Logger, tokenizer Tokenizer, summarizer Chatter) *ContextCompressor {
	if logger == nil {
		logger = slog.Default()
	}
	if tokenizer == nil {
		tokenizer = &HeuristicTokenizer{}
	}

	// Apply defaults for zero-valued ratios.
	if cfg.Stage1WarningRatio <= 0 {
		cfg.Stage1WarningRatio = DefaultWarningRatio
	}
	if cfg.Stage2SummarizeRatio <= 0 {
		cfg.Stage2SummarizeRatio = DefaultSummarizeRatio
	}
	if cfg.Stage3AggressiveRatio <= 0 {
		cfg.Stage3AggressiveRatio = DefaultAggressiveRatio
	}
	if cfg.Stage4HardLimitRatio <= 0 {
		cfg.Stage4HardLimitRatio = DefaultHardLimitRatio
	}

	return &ContextCompressor{
		config:     cfg,
		summarizer: summarizer,
		logger:     logger,
		tokenizer:  tokenizer,
	}
}

// Compress evaluates the current context utilization and applies the
// appropriate compression stage. The utilization parameter (0.0-1.0)
// represents the fraction of the model's context limit already consumed.
func (c *ContextCompressor) Compress(ctx context.Context, messages []ChatMessage, utilization float64) CompressionResult {
	if !c.config.Enabled {
		tokens := c.countTokens(messages)
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: tokens,
			TokensAfter:  tokens,
			DroppedCount: 0,
			Metrics:      c.buildQualityMetrics(messages, messages, tokens, tokens, CompressionStageNone),
		}
	}

	tokensBefore := c.countTokens(messages)

	// Stage 0: no action needed
	if utilization < c.config.Stage1WarningRatio {
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensBefore,
			DroppedCount: 0,
			Metrics:      c.buildQualityMetrics(messages, messages, tokensBefore, tokensBefore, CompressionStageNone),
		}
	}

	// Stage 1: warning only
	if utilization < c.config.Stage2SummarizeRatio {
		c.stats.WarningEvents.Add(1)
		c.logger.Warn("context utilization entering warning zone",
			"utilization", utilization,
			"stage", "warning",
			"tokens", tokensBefore,
		)
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageWarning,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensBefore,
			DroppedCount: 0,
			Metrics:      c.buildQualityMetrics(messages, messages, tokensBefore, tokensBefore, CompressionStageWarning),
		}
	}

	// Stage 2: summarize old history (keep system + last 4)
	if utilization < c.config.Stage3AggressiveRatio {
		c.stats.SummarizeEvents.Add(1)
		c.stats.TotalCompressions.Add(1)
		compressed := c.summarizeOldHistory(ctx, messages)
		tokensAfter := c.countTokens(compressed)
		saved := tokensBefore - tokensAfter
		if saved > 0 {
			c.stats.TotalTokensSaved.Add(uint64(saved))
		}
		c.logger.Info("context compressed via summarization",
			"utilization", utilization,
			"tokens_before", tokensBefore,
			"tokens_after", tokensAfter,
			"saved", saved,
		)
		return CompressionResult{
			Messages:     compressed,
			Compressed:   true,
			Stage:        CompressionStageSummarize,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensAfter,
			DroppedCount: len(messages) - len(compressed),
			Metrics:      c.buildQualityMetrics(messages, compressed, tokensBefore, tokensAfter, CompressionStageSummarize),
		}
	}

	// Stage 3: aggressive compression (keep system + critical + last 4)
	if utilization < c.config.Stage4HardLimitRatio {
		c.stats.AggressiveEvents.Add(1)
		c.stats.TotalCompressions.Add(1)
		compressed := c.aggressiveCompress(ctx, messages)
		tokensAfter := c.countTokens(compressed)
		saved := tokensBefore - tokensAfter
		if saved > 0 {
			c.stats.TotalTokensSaved.Add(uint64(saved))
		}
		c.logger.Warn("aggressive context compression applied",
			"utilization", utilization,
			"tokens_before", tokensBefore,
			"tokens_after", tokensAfter,
			"saved", saved,
		)
		return CompressionResult{
			Messages:     compressed,
			Compressed:   true,
			Stage:        CompressionStageAggressive,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensAfter,
			DroppedCount: len(messages) - len(compressed),
			Metrics:      c.buildQualityMetrics(messages, compressed, tokensBefore, tokensAfter, CompressionStageAggressive),
		}
	}

	// Stage 4: hard limit -- drop old context (keep system + last 2)
	c.stats.HardLimitEvents.Add(1)
	c.stats.TotalCompressions.Add(1)
	compressed := c.dropOldContext(messages)
	tokensAfter := c.countTokens(compressed)
	saved := tokensBefore - tokensAfter
	if saved > 0 {
		c.stats.TotalTokensSaved.Add(uint64(saved))
	}
	c.logger.Error("hard limit reached, old context dropped",
		"utilization", utilization,
		"tokens_before", tokensBefore,
		"tokens_after", tokensAfter,
		"saved", saved,
	)
	return CompressionResult{
		Messages:     compressed,
		Compressed:   true,
		Stage:        CompressionStageHardLimit,
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		DroppedCount: len(messages) - len(compressed),
		Metrics:      c.buildQualityMetrics(messages, compressed, tokensBefore, tokensAfter, CompressionStageHardLimit),
	}
}

// countTokens sums the token counts of all messages using the configured tokenizer.
func (c *ContextCompressor) countTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += c.tokenizer.CountTokens(msg.Content)
	}
	return total
}

// countCritical returns the number of critical messages in the slice.
func countCritical(messages []ChatMessage) int {
	n := 0
	for _, msg := range messages {
		if msg.Critical {
			n++
		}
	}
	return n
}

// maxSummaryLevel returns the highest SummaryLevel among the messages.
func maxSummaryLevel(messages []ChatMessage) int {
	max := 0
	for _, msg := range messages {
		if msg.SummaryLevel > max {
			max = msg.SummaryLevel
		}
	}
	return max
}

// buildQualityMetrics constructs a QualityMetrics for the compression pass.
func (c *ContextCompressor) buildQualityMetrics(before, after []ChatMessage, tokensBefore, tokensAfter int, stage CompressionStage) QualityMetrics {
	var ratio float64
	if tokensBefore > 0 {
		ratio = float64(tokensAfter) / float64(tokensBefore)
	}

	criticalBefore := countCritical(before)
	criticalAfter := countCritical(after)

	dropped := max(criticalBefore-criticalAfter,
		// Compression can only drop messages, not add critical ones
		0)

	qm := QualityMetrics{
		TokenRatio:       ratio,
		CriticalRetained: criticalAfter,
		CriticalDropped:  dropped,
		SummaryLevel:     maxSummaryLevel(after),
		CompressionStage: stage,
	}

	// Record quality score for running average. The quality score is the
	// token ratio weighted by critical retention: if any critical messages
	// were dropped the score is 0, otherwise it's the token ratio.
	var score float64
	if qm.CriticalDropped == 0 {
		score = ratio
	}
	c.stats.QualityScoreSum.Add(uint64(score * 1000))
	c.stats.QualityScoreCount.Add(1)

	return qm
}

// summarizeOldHistory keeps the system messages plus the last 4 non-system
// messages. When a compactor is available, it uses it for smart summarization.
// Falls back to LLM summarizer, then to tail-keep truncation.
func (c *ContextCompressor) summarizeOldHistory(ctx context.Context, messages []ChatMessage) []ChatMessage {
	if c.summarizer != nil {
		summarized, err := c.summarizeWithLLM(ctx, messages)
		if err != nil {
			c.logger.Warn("LLM summarization failed, falling back to tail-keep", "error", err)
		} else {
			return summarized
		}
	}
	return keepTail(messages, 4)
}

// summarizeWithLLM performs real LLM-based summarization. It separates system
// messages, sends the older non-system messages to the summarizer, and
// returns system messages + summary + tail of 4 recent messages.
func (c *ContextCompressor) summarizeWithLLM(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
	var systemMsgs, nonSystemMsgs []ChatMessage
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	keepCount := 4
	if len(nonSystemMsgs) <= keepCount {
		return messages, nil
	}

	tailStart := len(nonSystemMsgs) - keepCount
	toSummarize := nonSystemMsgs[:tailStart]
	tail := nonSystemMsgs[tailStart:]

	var sb strings.Builder
	sb.WriteString("Summarize the following conversation history into a concise summary preserving:\n")
	sb.WriteString("- Key decisions made\n- Important file paths mentioned\n- Unresolved questions\n- Current task status\n\n")
	for _, msg := range toSummarize {
		fmt.Fprintf(&sb, "[%s]: %s\n", msg.Role, msg.Content)
	}

	summaryPrompt := ChatMessage{Role: RoleUser, Content: sb.String()}
	resp, err := c.summarizer.Chat(ctx, append(systemMsgs, summaryPrompt))
	if err != nil {
		return nil, fmt.Errorf("summarizer chat failed: %w", err)
	}

	summaryMsg := ChatMessage{
		Role:     RoleSystem,
		Content:  fmt.Sprintf("[Conversation Summary]\n%s", resp.Content),
		Critical: true,
	}

	result := make([]ChatMessage, 0, len(systemMsgs)+1+len(tail))
	result = append(result, systemMsgs...)
	result = append(result, summaryMsg)
	result = append(result, tail...)
	return result, nil
}

// aggressiveCompress keeps system messages, critical messages outside the tail,
// plus the last 4 non-system messages. This is differentiated from
// dropOldContext (which keeps only the last 2).
func (c *ContextCompressor) aggressiveCompress(_ context.Context, messages []ChatMessage) []ChatMessage {
	return keepTail(messages, 4)
}

// dropOldContext keeps system messages plus the last 2 non-system messages.
// It mirrors aggressiveCompress in retention but is semantically distinct:
// dropOldContext is the last-resort stage triggered at the hard limit.
func (c *ContextCompressor) dropOldContext(messages []ChatMessage) []ChatMessage {
	return keepTail(messages, 2)
}

// Stats returns a snapshot of the cumulative compression statistics.
func (c *ContextCompressor) Stats() CompressionStatsSnapshot {
	return c.stats.Snapshot()
}

// keepTail is the shared helper that preserves all system messages, all
// critical messages, and the last n non-system messages from the input slice.
// Critical messages that would otherwise fall outside the tail window are
// retained in their original order.
func keepTail(messages []ChatMessage, n int) []ChatMessage {
	var systemMsgs []ChatMessage
	var nonSystemMsgs []ChatMessage

	for _, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	// Determine which non-system messages to keep: the tail of n plus any
	// critical messages that fall outside the tail window.
	keepSet := make(map[int]bool)
	tailStart := max(len(nonSystemMsgs)-n, 0)
	for i := tailStart; i < len(nonSystemMsgs); i++ {
		keepSet[i] = true
	}
	// Mark critical messages outside the tail for retention.
	for i, msg := range nonSystemMsgs {
		if msg.Critical {
			keepSet[i] = true
		}
	}

	result := make([]ChatMessage, 0, len(systemMsgs)+len(keepSet))
	result = append(result, systemMsgs...)
	for i, msg := range nonSystemMsgs {
		if keepSet[i] {
			result = append(result, msg)
		}
	}

	return result
}
