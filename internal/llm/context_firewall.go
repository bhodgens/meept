package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
)

// ContextFirewallConfig configures context budget and summarization behavior.
type ContextFirewallConfig struct {
	Enabled                    bool    // When false, firewall passes through
	SummarizeHistory           bool    // When true, old messages are summarized
	SmallModelContextThreshold int     // tokens; models below this get extra reduction
	IterationBudgetRatio       float64 // fraction of context reserved for a single iteration
	ConversationBudgetRatio    float64 // fraction for overall conversation history
	ChunkLargeInputs           bool    // When true, split oversized inputs at boundaries
	ChunkThresholdRatio        float64 // max input size relative to context limit
	// WrapUpThreshold is the "soft" limit (0.0-1.0) where wrap-up suggestions are injected
	WrapUpThreshold float64 // default 0.50
	// HardLimit is the "hard" limit (0.0-1.0) where context is dropped and reattempted
	HardLimit float64 // default 0.80
	// DropContextOnHardLimit enables context dropping when hard limit is hit
	DropContextOnHardLimit bool // default true
	// ProactiveCompression enables the multi-stage ContextCompressor inside the
	// firewall. When true, the compressor runs before the legacy
	// chunk/summarize/drop pipeline.
	ProactiveCompression bool
	// ModelContextLimit overrides the model's ContextLimit for the compressor.
	// When zero, model.ContextLimit is used.
	ModelContextLimit int
}

// ContextFirewall wraps a Chatter and enforces context budgets.
type ContextFirewall struct {
	inner        Chatter
	model        *ModelConfig
	config       ContextFirewallConfig
	summaryModel Chatter
	logger       *slog.Logger
	tokenizer    Tokenizer
	compressor   *ContextCompressor

	// Counters (atomic-safe for concurrent callers)
	summarizationFailures atomic.Uint64
	droppedMessages       atomic.Uint64
	dropEvents            atomic.Uint64
}

// FirewallStats is a snapshot of firewall counters including compression stats.
type FirewallStats struct {
	SummarizationFailures uint64
	DroppedMessages       uint64
	DropEvents            uint64
	// Compression stats (populated when ProactiveCompression is enabled)
	CompressionWarningEvents   uint64
	CompressionSummarizeEvents uint64
	CompressionAggressiveEvents uint64
	CompressionHardLimitEvents uint64
	CompressionTokensSaved     uint64
}

// Stats returns a snapshot of firewall counters. When proactive compression
// is enabled, compression counters are populated from the internal compressor.
func (f *ContextFirewall) Stats() FirewallStats {
	stats := FirewallStats{
		SummarizationFailures: f.summarizationFailures.Load(),
		DroppedMessages:       f.droppedMessages.Load(),
		DropEvents:            f.dropEvents.Load(),
	}

	if f.compressor != nil {
		cs := f.compressor.Stats()
		stats.CompressionWarningEvents = cs.WarningEvents
		stats.CompressionSummarizeEvents = cs.SummarizeEvents
		stats.CompressionAggressiveEvents = cs.AggressiveEvents
		stats.CompressionHardLimitEvents = cs.HardLimitEvents
		stats.CompressionTokensSaved = cs.TotalTokensSaved
	}

	return stats
}

// Compress runs the multi-stage compressor on messages and returns the result.
// If proactive compression is not enabled, it returns the messages unchanged
// with CompressionStageNone.
func (f *ContextFirewall) Compress(ctx context.Context, messages []ChatMessage) (CompressionResult, error) {
	if f.compressor == nil {
		tokens := f.countTokens(messages)
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: tokens,
			TokensAfter:  tokens,
			DroppedCount: 0,
		}, nil
	}

	currentTokens := f.countTokens(messages)
	utilization := float64(currentTokens) / float64(f.model.ContextLimit)
	result := f.compressor.Compress(ctx, messages, utilization)
	return result, nil
}

// NewContextFirewall creates a new context firewall.
// summaryModel may be nil; in that case, inner is used for summaries.
// tokenizer may be nil; in that case, a heuristic tokenizer is used.
func NewContextFirewall(
	inner Chatter,
	model *ModelConfig,
	cfg ContextFirewallConfig,
	summaryModel Chatter,
	logger *slog.Logger,
	tokenizer Tokenizer,
) *ContextFirewall {
	if logger == nil {
		logger = slog.Default()
	}

	// Defaults
	if cfg.SmallModelContextThreshold <= 0 {
		cfg.SmallModelContextThreshold = 32768
	}
	if cfg.IterationBudgetRatio <= 0 {
		cfg.IterationBudgetRatio = 0.30
	}
	if cfg.ConversationBudgetRatio <= 0 {
		cfg.ConversationBudgetRatio = 0.50
	}
	if cfg.ChunkThresholdRatio <= 0 {
		cfg.ChunkThresholdRatio = 0.25
	}
	if cfg.WrapUpThreshold <= 0 {
		cfg.WrapUpThreshold = 0.50
	}
	if cfg.HardLimit <= 0 {
		cfg.HardLimit = 0.80
	}

	if summaryModel == nil {
		summaryModel = inner
	}

	if tokenizer == nil {
		tokenizer = &HeuristicTokenizer{}
	}

	// Optionally initialise the proactive multi-stage compressor.
	var compressor *ContextCompressor
	if cfg.ProactiveCompression {
		contextLimit := cfg.ModelContextLimit
		if contextLimit <= 0 && model != nil {
			contextLimit = model.ContextLimit
		}
		compressorCfg := CompressionConfig{
			Enabled:              true,
			ModelContextLimit:    contextLimit,
			Stage1WarningRatio:   DefaultWarningRatio,
			Stage2SummarizeRatio: DefaultSummarizeRatio,
			Stage3AggressiveRatio: DefaultAggressiveRatio,
			Stage4HardLimitRatio:  DefaultHardLimitRatio,
		}
		compressor = NewContextCompressor(compressorCfg, logger, tokenizer)
	}

	return &ContextFirewall{
		inner:        inner,
		model:        model,
		config:       cfg,
		summaryModel: summaryModel,
		logger:       logger,
		tokenizer:    tokenizer,
		compressor:   compressor,
	}
}


// Chat sends a request through context filtering.
func (f *ContextFirewall) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	// Validate context size before processing
	if err := f.ValidateContextSize(messages); err != nil {
		return nil, err
	}

	processed, err := f.processMessages(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("context firewall: %w", err)
	}

	resp, err := f.inner.Chat(ctx, processed, opts...)
	if err == nil && f.logger != nil {
		util := f.ContextUtilization(processed)
		f.logger.Debug("context utilization", "ratio", util)
	}
	return resp, err
}

// ChatWithProgress sends a request with progress reporting through context filtering.
func (f *ContextFirewall) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	// Validate context size before processing
	if err := f.ValidateContextSize(messages); err != nil {
		return nil, err
	}

	processed, err := f.processMessages(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("context firewall: %w", err)
	}

	resp, err := f.inner.ChatWithProgress(ctx, processed, progress, opts...)
	if err == nil && f.logger != nil {
		util := f.ContextUtilization(processed)
		f.logger.Debug("context utilization", "ratio", util)
	}
	return resp, err
}

// DerivedIterationBudget returns the iteration (per-turn) token budget.
func (f *ContextFirewall) DerivedIterationBudget() int {
	if f.model == nil || f.model.ContextLimit == 0 {
		// Fallback to a reasonable default if no model configured
		return 4096
	}
	budget := int(float64(f.model.ContextLimit) * f.config.IterationBudgetRatio)
	if f.model.ContextLimit < f.config.SmallModelContextThreshold {
		// Small model: apply extra reduction
		budget = int(float64(budget) * 0.7)
	}
	return budget
}

// DerivedConversationBudget returns the total conversation history budget.
func (f *ContextFirewall) DerivedConversationBudget() int {
	if f.model == nil || f.model.ContextLimit == 0 {
		return 8192
	}
	budget := int(float64(f.model.ContextLimit) * f.config.ConversationBudgetRatio)
	if f.model.ContextLimit < f.config.SmallModelContextThreshold {
		budget = int(float64(budget) * 0.7)
	}
	return budget
}

// ContextUtilization returns the token usage as a fraction of the context limit.
func (f *ContextFirewall) ContextUtilization(messages []ChatMessage) float64 {
	if f.model == nil || f.model.ContextLimit == 0 {
		return 0
	}
	tokens := f.countTokens(messages)
	return float64(tokens) / float64(f.model.ContextLimit)
}

// processMessages applies the context firewall filtering pipeline.
// It implements threshold-based handling:
// - At wrapUpThreshold (50%): logs warning for potential wrap-up
// - At hardLimit (80%): drops old context if DropContextOnHardLimit is enabled
func (f *ContextFirewall) processMessages(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
	if !f.config.Enabled || f.model == nil || f.model.ContextLimit == 0 {
		return messages, nil
	}

	result := append([]ChatMessage{}, messages...)

	// Estimate current token usage using tokenizer
	currentTokens := f.countTokens(result)
	utilization := float64(currentTokens) / float64(f.model.ContextLimit)

	// Proactive compression: run the multi-stage compressor before the
	// legacy pipeline so that the more granular thresholds can reduce
	// context pressure early.
	if f.compressor != nil {
		cr := f.compressor.Compress(ctx, result, utilization)
		if cr.Compressed {
			f.logger.Debug("proactive compression applied",
				"stage", cr.Stage.String(),
				"tokens_before", cr.TokensBefore,
				"tokens_after", cr.TokensAfter,
				"dropped", cr.DroppedCount,
			)
			result = cr.Messages
			currentTokens = cr.TokensAfter
			utilization = float64(currentTokens) / float64(f.model.ContextLimit)
		}
	}

	// Check Hard Limit first - may force context drop
	if utilization >= f.config.HardLimit {
		if f.config.DropContextOnHardLimit {
			f.logger.Warn("context exceeded hard limit, dropping old context",
				"utilization", utilization,
				"hard_limit", f.config.HardLimit,
			)
			result = f.dropOldContext(result)
			currentTokens = f.countTokens(result)
			utilization = float64(currentTokens) / float64(f.model.ContextLimit)
		} else {
			f.logger.Warn("context exceeded hard limit but DropContextOnHardLimit is disabled",
				"utilization", utilization,
				"hard_limit", f.config.HardLimit,
			)
		}
	}

	// Check Wrap-Up Threshold - log warning for potential wrap-up
	if utilization >= f.config.WrapUpThreshold && utilization < f.config.HardLimit {
		f.logger.Info("context exceeded wrap-up threshold, consider wrapping up",
			"utilization", utilization,
			"wrap_up_threshold", f.config.WrapUpThreshold,
		)
	}

	// Step 1: Chunk large input if configured
	if f.config.ChunkLargeInputs && len(result) > 0 {
		threshold := int(float64(f.model.ContextLimit) * f.config.ChunkThresholdRatio)
		lastMsg := &result[len(result)-1]
		lastMsgTokens := f.tokenizer.CountTokens(lastMsg.Content)
		if lastMsgTokens > threshold {
			chunks := f.chunkMessage(lastMsg.Content, threshold)
			if len(chunks) > 1 {
				f.logger.Debug("chunking large input", "chunks", len(chunks))
				// Replace last message with first chunk
				result[len(result)-1].Content = chunks[0]
				// Append remaining chunks as new messages
				for _, chunk := range chunks[1:] {
					result = append(result, ChatMessage{
						Role:    RoleUser,
						Content: chunk,
					})
				}
				currentTokens = f.countTokens(result)
			}
		}
	}

	// Step 2: Summarize old history if too much context is used
	// Use hard limit as the summarization threshold
	if f.config.SummarizeHistory && currentTokens > int(float64(f.model.ContextLimit)*f.config.HardLimit) {
		summarized, err := f.summarizeOldHistory(ctx, result)
		if err != nil {
			f.summarizationFailures.Add(1)
			f.logger.Warn("summarization failed, continuing without summarization",
				"error", err,
				"failures_total", f.summarizationFailures.Load(),
			)
			// Continue without summarization
		} else {
			result = summarized
			currentTokens = f.countTokens(result)
			f.logger.Debug("summarized history", "tokens", currentTokens)
		}
	}

	return result, nil
}

// dropOldContext removes old messages, keeping only system prompt and last 2 messages.
// This is used when the hard limit is exceeded to quickly free up context space.
func (f *ContextFirewall) dropOldContext(messages []ChatMessage) []ChatMessage {
	if len(messages) <= 3 {
		return messages // Already minimal
	}

	// Find system message(s) to keep
	var systemMsgs []ChatMessage
	var nonSystemMsgs []ChatMessage

	for _, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	// Keep system + last 2 non-system messages
	result := make([]ChatMessage, 0, len(systemMsgs)+2)
	result = append(result, systemMsgs...)

	// Keep last 2 messages
	if len(nonSystemMsgs) > 2 {
		result = append(result, nonSystemMsgs[len(nonSystemMsgs)-2:]...)
	} else {
		result = append(result, nonSystemMsgs...)
	}

	dropped := len(messages) - len(result)
	if dropped > 0 {
		f.droppedMessages.Add(uint64(dropped))
		f.dropEvents.Add(1)
	}
	f.logger.Warn("dropped old context",
		"original_count", len(messages),
		"new_count", len(result),
		"dropped_count", dropped,
		"dropped_total", f.droppedMessages.Load(),
	)

	return result
}

// chunkMessage splits a message at paragraph or sentence boundaries.
func (f *ContextFirewall) chunkMessage(content string, maxTokens int) []string {
	// Estimate max characters based on 3 chars per token
	maxChars := maxTokens * 3

	// Try to split at paragraph boundaries first
	paragraphs := strings.Split(content, "\n\n")
	if len(paragraphs) > 1 {
		return f.greedyChunk(paragraphs, maxChars, "\n\n")
	}

	// Fall back to sentence boundaries
	sentences := strings.Split(content, ". ")
	if len(sentences) > 1 {
		return f.greedyChunk(sentences, maxChars, ". ")
	}

	// Fall back to word boundaries
	words := strings.Fields(content)
	if len(words) > 1 {
		return f.greedyChunk(words, maxChars, " ")
	}

	// As a last resort, split at character boundaries
	var chunks []string
	for i := 0; i < len(content); i += maxChars {
		end := i + maxChars
		if end > len(content) {
			end = len(content)
		}
		chunks = append(chunks, content[i:end])
	}
	return chunks
}

// greedyChunk greedily combines parts until hitting the size limit.
func (f *ContextFirewall) greedyChunk(parts []string, maxChars int, sep string) []string {
	var chunks []string
	var current strings.Builder

	for _, part := range parts {
		testStr := current.String()
		if current.Len() > 0 {
			testStr += sep + part
		} else {
			testStr = part
		}

		if len(testStr) <= maxChars {
			if current.Len() > 0 {
				current.WriteString(sep)
			}
			current.WriteString(part)
		} else {
			// Current chunk is full
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
			}
			current.Reset()
			current.WriteString(part)
		}
	}

	// Don't forget the last chunk
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// countTokens counts tokens in a message slice using the configured tokenizer.
func (f *ContextFirewall) countTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += f.tokenizer.CountTokens(msg.Content)
	}
	return total
}

// summarizeOldHistory summarizes old messages, keeping the system prompt and last few messages.
func (f *ContextFirewall) summarizeOldHistory(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
	// Keep: system prompt + last 4 messages
	keepCount := 4 + 1 // +1 for system

	if len(messages) <= keepCount {
		return messages, nil
	}

	var result []ChatMessage
	var toSummarize []ChatMessage

	for i, msg := range messages {
		if msg.Role == RoleSystem {
			result = append(result, msg)
		} else if i >= len(messages)-keepCount {
			result = append(result, msg)
		} else {
			toSummarize = append(toSummarize, msg)
		}
	}

	if len(toSummarize) == 0 {
		return messages, nil
	}

	// Build summarization request
	summaryPrompt := "Please summarize the following conversation concisely:\n\n"
	for _, msg := range toSummarize {
		summaryPrompt += fmt.Sprintf("%s: %s\n", msg.Role, msg.Content)
	}

	summaryResp, err := f.summaryModel.Chat(ctx, []ChatMessage{
		{
			Role:    RoleUser,
			Content: summaryPrompt,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	// Prepend summary to the kept messages
	final := append([]ChatMessage{
		{
			Role:    RoleSystem,
			Content: "[Conversation summary]: " + summaryResp.Content,
		},
	}, result...)

	return final, nil
}

// ValidateContextSize checks if the context size exceeds the model limit.
// Returns a ContextSizeExceeded error if the limit is exceeded.
func (f *ContextFirewall) ValidateContextSize(messages []ChatMessage) error {
	if f.model == nil || f.model.ContextLimit == 0 {
		return nil // No limit configured
	}

	estimated := f.countTokens(messages)
	modelLimit := f.model.ContextLimit

	if estimated > modelLimit {
		return &ContextSizeExceeded{
			Estimated:  estimated,
			ModelLimit: modelLimit,
			Suggestions: []string{
				"Reduce conversation history length",
				"Use summarization for old messages",
				"Split large inputs into smaller chunks",
				"Clear unnecessary context",
			},
		}
	}

	// Warning zone: 80%+ utilization
	if estimated > int(float64(modelLimit)*0.8) {
		f.logger.Warn("context size approaching model limit",
			"estimated", estimated,
			"limit", modelLimit,
			"utilization", float64(estimated)/float64(modelLimit),
		)
	}

	return nil
}

// Ensure ContextFirewall implements Chatter
var _ Chatter = (*ContextFirewall)(nil)
