package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
)

// CompressionStage represents the severity of context compression applied.
type CompressionStage int

const (
	CompressionStageNone       CompressionStage = iota // No compression needed
	CompressionStageWarning                             // Context is filling up; log a warning only
	CompressionStageSummarize                           // Summarize old history (keep system + last 4)
	CompressionStageAggressive                          // Aggressively compress (keep system + last 2)
	CompressionStageHardLimit                           // Drop old context entirely (keep system + last 2)
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
	Enabled                bool    // Master switch
	ModelContextLimit      int     // Total context tokens for the model
	Stage1WarningRatio     float64 // Utilization threshold for warning (default 0.50)
	Stage2SummarizeRatio   float64 // Utilization threshold for summarization (default 0.60)
	Stage3AggressiveRatio  float64 // Utilization threshold for aggressive compression (default 0.70)
	Stage4HardLimitRatio   float64 // Utilization threshold for hard limit (default 0.80)
}

// Defaults.
const (
	DefaultWarningRatio    = 0.50
	DefaultSummarizeRatio  = 0.60
	DefaultAggressiveRatio = 0.70
	DefaultHardLimitRatio  = 0.80
)

// CompressionResult holds the outcome of a compression pass.
type CompressionResult struct {
	Messages     []ChatMessage
	Compressed   bool
	Stage        CompressionStage
	TokensBefore int
	TokensAfter  int
	DroppedCount int
}

// CompressionStats tracks cumulative compression events with atomic-safe counters.
type CompressionStats struct {
	WarningEvents    atomic.Uint64
	SummarizeEvents  atomic.Uint64
	AggressiveEvents atomic.Uint64
	HardLimitEvents  atomic.Uint64
	TotalTokensSaved atomic.Uint64
}

// Snapshot returns a point-in-time copy of the stats fields as plain values.
type CompressionStatsSnapshot struct {
	WarningEvents    uint64
	SummarizeEvents  uint64
	AggressiveEvents uint64
	HardLimitEvents  uint64
	TotalTokensSaved uint64
}

// Snapshot reads all atomic counters and returns a plain-value snapshot.
func (s *CompressionStats) Snapshot() CompressionStatsSnapshot {
	return CompressionStatsSnapshot{
		WarningEvents:    s.WarningEvents.Load(),
		SummarizeEvents:  s.SummarizeEvents.Load(),
		AggressiveEvents: s.AggressiveEvents.Load(),
		HardLimitEvents:  s.HardLimitEvents.Load(),
		TotalTokensSaved: s.TotalTokensSaved.Load(),
	}
}

// ContextCompressor implements multi-stage context compression based on
// utilization thresholds. It is safe for concurrent use.
type ContextCompressor struct {
	config    CompressionConfig
	stats     CompressionStats
	logger    *slog.Logger
	tokenizer Tokenizer
}

// NewContextCompressor creates a new compressor with the given configuration.
// If logger is nil, slog.Default() is used. If tokenizer is nil,
// HeuristicTokenizer is used.
func NewContextCompressor(cfg CompressionConfig, logger *slog.Logger, tokenizer Tokenizer) *ContextCompressor {
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
		config:    cfg,
		logger:    logger,
		tokenizer: tokenizer,
	}
}

// Compress evaluates the current context utilization and applies the
// appropriate compression stage. The utilization parameter (0.0-1.0)
// represents the fraction of the model's context limit already consumed.
func (c *ContextCompressor) Compress(ctx context.Context, messages []ChatMessage, utilization float64) CompressionResult {
	if !c.config.Enabled {
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: c.countTokens(messages),
			TokensAfter:  c.countTokens(messages),
			DroppedCount: 0,
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
		}
	}

	// Stage 2: summarize old history (keep system + last 4)
	if utilization < c.config.Stage3AggressiveRatio {
		c.stats.SummarizeEvents.Add(1)
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
		}
	}

	// Stage 3: aggressive compression (keep system + last 2)
	if utilization < c.config.Stage4HardLimitRatio {
		c.stats.AggressiveEvents.Add(1)
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
		}
	}

	// Stage 4: hard limit -- drop old context (keep system + last 2)
	c.stats.HardLimitEvents.Add(1)
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

// summarizeOldHistory keeps the system messages plus the last 4 non-system
// messages. It does not perform LLM summarization itself -- callers that want
// a true summary should wrap this in an LLM call.
func (c *ContextCompressor) summarizeOldHistory(_ context.Context, messages []ChatMessage) []ChatMessage {
	return keepTail(messages, 4)
}

// aggressiveCompress keeps system messages plus the last 2 non-system messages.
func (c *ContextCompressor) aggressiveCompress(_ context.Context, messages []ChatMessage) []ChatMessage {
	return keepTail(messages, 2)
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

// keepTail is the shared helper that preserves all system messages and the
// last n non-system messages from the input slice.
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

	result := make([]ChatMessage, 0, len(systemMsgs)+n)
	result = append(result, systemMsgs...)

	if len(nonSystemMsgs) > n {
		result = append(result, nonSystemMsgs[len(nonSystemMsgs)-n:]...)
	} else {
		result = append(result, nonSystemMsgs...)
	}

	return result
}
