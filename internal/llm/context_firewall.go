package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
)

// structuredSummaryPromptTemplate is the prompt used for content-aware
// summarization. It asks the LLM to return a structured response with
// labeled sections that can be parsed via parseStructuredSummary.
const structuredSummaryPromptTemplate = `Please summarize the following conversation, extracting structured information:

DECISIONS:
- [list key decisions made, one per line, prefixed with "- "]

FILES:
- [list all file paths mentioned, one per line, prefixed with "- "]

QUESTIONS:
- [list unresolved questions remaining, one per line, prefixed with "- "]

STATUS:
[one-line current task status]

FINDINGS:
- [list important discoveries, one per line, prefixed with "- "]

SUMMARY:
[A 3-sentence narrative summary of the conversation]

<conversation>
%s
</conversation>`

// parseStructuredSummary extracts a SummaryExtract from the LLM response
// text produced by structuredSummaryPromptTemplate. It uses section headers
// to split the text and then parses bullet items from each section.
// If parsing fails for any section, that field is left empty rather than
// causing an overall failure.
func parseStructuredSummary(raw string) SummaryExtract {
	var ext SummaryExtract

	sections := splitStructuredSections(raw)

	ext.Decisions = parseBulletItems(sections["DECISIONS"])
	ext.FilePaths = parseBulletItems(sections["FILES"])
	ext.UnresolvedQuestions = parseBulletItems(sections["QUESTIONS"])
	ext.TaskState = strings.TrimSpace(sections["STATUS"])
	ext.KeyFindings = parseBulletItems(sections["FINDINGS"])

	return ext
}

// sectionRe matches section headers like "DECISIONS:", "FILES:", etc.
var sectionRe = regexp.MustCompile(`(?m)^(DECISIONS|FILES|QUESTIONS|STATUS|FINDINGS|SUMMARY)\s*:\s*$`)

// splitStructuredSections splits the raw response into named sections based
// on the header pattern. Each header line starts a new section; text between
// two headers belongs to the preceding header.
func splitStructuredSections(raw string) map[string]string {
	result := make(map[string]string)
	matches := sectionRe.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return result
	}

	for i, m := range matches {
		name := raw[m[2]:m[3]]
		start := m[1] // end of the header line
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(raw)
		}
		result[name] = raw[start:end]
	}

	return result
}

// bulletItemRe matches lines starting with "- " (with optional leading whitespace).
var bulletItemRe = regexp.MustCompile(`(?m)^\s*-\s+(.+)$`)

// parseBulletItems extracts individual bullet items from a section body.
func parseBulletItems(section string) []string {
	if section == "" {
		return nil
	}
	matches := bulletItemRe.FindAllStringSubmatch(section, -1)
	if matches == nil {
		return nil
	}
	items := make([]string, 0, len(matches))
	for _, m := range matches {
		item := strings.TrimSpace(m[1])
		if item != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// formatStructuredSummary builds a human-readable summary string from a
// SummaryExtract. The output is compact but parseable, designed to fit
// within a context window as a replacement for raw history.
func formatStructuredSummary(level int, ext SummaryExtract, narrative string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[Conversation summary level %d]:", level)

	if ext.TaskState != "" {
		fmt.Fprintf(&b, " status: %s.", ext.TaskState)
	}

	if len(ext.Decisions) > 0 {
		b.WriteString(" decisions:")
		for _, d := range ext.Decisions {
			b.WriteString(" ")
			b.WriteString(d)
			b.WriteString(";")
		}
	}

	if len(ext.FilePaths) > 0 {
		b.WriteString(" files:")
		for _, f := range ext.FilePaths {
			b.WriteString(" ")
			b.WriteString(f)
			b.WriteString(";")
		}
	}

	if len(ext.UnresolvedQuestions) > 0 {
		b.WriteString(" open questions:")
		for _, q := range ext.UnresolvedQuestions {
			b.WriteString(" ")
			b.WriteString(q)
			b.WriteString(";")
		}
	}

	if len(ext.KeyFindings) > 0 {
		b.WriteString(" findings:")
		for _, f := range ext.KeyFindings {
			b.WriteString(" ")
			b.WriteString(f)
			b.WriteString(";")
		}
	}

	narrative = strings.TrimSpace(narrative)
	if narrative != "" {
		b.WriteString(" ")
		b.WriteString(narrative)
	}

	return b.String()
}

// extractNarrative returns the SUMMARY section text from the raw LLM response.
func extractNarrative(raw string) string {
	sections := splitStructuredSections(raw)
	return strings.TrimSpace(sections["SUMMARY"])
}

// marshalExtractAsJSON is a helper for logging/debugging.
func marshalExtractAsJSON(ext SummaryExtract) string {
	b, err := json.Marshal(ext)
	if err != nil {
		return "{}"
	}
	return string(b)
}

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
	// HierarchicalSummarization enables recursive summarization where
	// summaries that exceed SummaryLevelThreshold tokens are themselves
	// summarized at the next level up to MaxSummaryLevel.
	HierarchicalSummarization bool
	// MaxSummaryLevel is the maximum recursion depth for hierarchical
	// summarization (default 3).
	MaxSummaryLevel int
	// SummaryLevelThreshold is the token count at which a summary is
	// re-summarized at the next level (default 500).
	SummaryLevelThreshold int
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
	compactor    *ContextCompactor
	// compactionTriggerRatio is the utilization threshold at which the
	// compactor is invoked as the primary context reduction strategy.
	// Default 0.60 (60%). When zero, compaction only runs through the
	// compressor pipeline (if ProactiveCompression is enabled).
	compactionTriggerRatio float64

	// Counters (atomic-safe for concurrent callers)
	summarizationFailures  atomic.Uint64
	droppedMessages        atomic.Uint64
	dropEvents             atomic.Uint64
	compactionEvents       atomic.Uint64
	compactionTokensSaved  atomic.Uint64
	compactionFallbacks    atomic.Uint64
}

// FirewallStats is a snapshot of firewall counters including compression stats.
type FirewallStats struct {
	SummarizationFailures uint64
	DroppedMessages       uint64
	DropEvents            uint64
	// Compaction stats (populated when compactor is set)
	CompactionEvents      uint64
	CompactionTokensSaved uint64
	CompactionFallbacks   uint64
	// Compression stats (populated when ProactiveCompression is enabled)
	CompressionWarningEvents    uint64
	CompressionSummarizeEvents  uint64
	CompressionAggressiveEvents uint64
	CompressionHardLimitEvents  uint64
	CompressionTokensSaved      uint64
	// Quality metrics (populated when ProactiveCompression is enabled)
	AvgQualityScore   float64 // Running average quality score across compressions
	TotalCompressions uint64  // Total number of compression passes applied
}

// Stats returns a snapshot of firewall counters. When proactive compression
// is enabled, compression counters are populated from the internal compressor.
// When a compactor is set, compaction counters are populated.
func (f *ContextFirewall) Stats() FirewallStats {
	stats := FirewallStats{
		SummarizationFailures: f.summarizationFailures.Load(),
		DroppedMessages:       f.droppedMessages.Load(),
		DropEvents:            f.dropEvents.Load(),
		CompactionEvents:      f.compactionEvents.Load(),
		CompactionTokensSaved: f.compactionTokensSaved.Load(),
		CompactionFallbacks:   f.compactionFallbacks.Load(),
	}

	if f.compressor != nil {
		cs := f.compressor.Stats()
		stats.CompressionWarningEvents = cs.WarningEvents
		stats.CompressionSummarizeEvents = cs.SummarizeEvents
		stats.CompressionAggressiveEvents = cs.AggressiveEvents
		stats.CompressionHardLimitEvents = cs.HardLimitEvents
		stats.CompressionTokensSaved = cs.TotalTokensSaved
		stats.AvgQualityScore = cs.AvgQualityScore
		stats.TotalCompressions = cs.TotalCompressions
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
	if cfg.MaxSummaryLevel <= 0 {
		cfg.MaxSummaryLevel = 3
	}
	if cfg.SummaryLevelThreshold <= 0 {
		cfg.SummaryLevelThreshold = 500
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
			Enabled:               true,
			ModelContextLimit:     contextLimit,
			Stage1WarningRatio:    DefaultWarningRatio,
			Stage2SummarizeRatio:  DefaultSummarizeRatio,
			Stage3AggressiveRatio: DefaultAggressiveRatio,
			Stage4HardLimitRatio:  DefaultHardLimitRatio,
		}
		compressor = NewContextCompressor(compressorCfg, logger, tokenizer, summaryModel)
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

// SetCompactor sets the ContextCompactor for smart summarization and
// configures the trigger ratio for direct compaction in processMessages.
// When triggerRatio > 0, the compactor is invoked at that utilization level
// as the primary context reduction strategy. When triggerRatio is 0, the
// compactor is only used through the compressor pipeline.
func (f *ContextFirewall) SetCompactor(compactor *ContextCompactor, triggerRatio float64) {
	if compactor == nil {
		return
	}
	f.compactor = compactor
	f.compactionTriggerRatio = triggerRatio
	// When triggerRatio > 0, processMessages (Layer 1) handles compaction
	// directly. Wiring the compactor into the compressor would cause double
	// compaction. When triggerRatio == 0, the compactor is only invoked
	// through the compressor pipeline (Layer 2).
	if triggerRatio == 0 && f.compressor != nil {
		f.compressor.SetCompactor(compactor)
	}
}

// Chat sends a request through context filtering.
// Validation runs AFTER the reduction pipeline so that salvageable requests
// are given a chance through compaction, compression, summarization, and
// hard-limit context dropping. Only if the reduced context still exceeds the
// model limit is the request rejected.
func (f *ContextFirewall) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	processed := f.processMessages(ctx, messages)

	// Validate context size after reduction
	if err := f.ValidateContextSize(processed); err != nil {
		return nil, err
	}

	resp, err := f.inner.Chat(ctx, processed, opts...)
	if err == nil && f.logger != nil {
		util := f.ContextUtilization(processed)
		f.logger.Debug("context utilization", "ratio", util)
	}
	return resp, err
}

// ChatWithProgress sends a request with progress reporting through context filtering.
// Validation runs AFTER the reduction pipeline (same as Chat).
func (f *ContextFirewall) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	processed := f.processMessages(ctx, messages)

	// Validate context size after reduction
	if err := f.ValidateContextSize(processed); err != nil {
		return nil, err
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
func (f *ContextFirewall) processMessages(ctx context.Context, messages []ChatMessage) []ChatMessage {
	if !f.config.Enabled || f.model == nil || f.model.ContextLimit == 0 {
		return messages
	}

	result := append([]ChatMessage{}, messages...)

	// Estimate current token usage using tokenizer
	currentTokens := f.countTokens(result)
	utilization := float64(currentTokens) / float64(f.model.ContextLimit)

	// Layer 1: LLM-based compaction (primary strategy). When a compactor
	// is configured with a trigger ratio, invoke it when utilization exceeds
	// that threshold. This runs before the compressor so the smarter
	// summarization gets first shot at reducing context pressure.
	if f.compactor != nil && f.compactionTriggerRatio > 0 && utilization >= f.compactionTriggerRatio {
		cr := f.compactor.Compact(ctx, result)
		if cr.Compacted {
			f.logger.Info("context compaction applied",
				"tokens_before", cr.TokensBefore,
				"tokens_after", cr.TokensAfter,
				"utilization_before", utilization,
			)
			f.compactionEvents.Add(1)
			saved := cr.TokensBefore - cr.TokensAfter
			if saved > 0 {
				f.compactionTokensSaved.Add(uint64(saved))
			}
			result = cr.Messages
			currentTokens = cr.TokensAfter
			utilization = float64(currentTokens) / float64(f.model.ContextLimit)
		} else {
			f.logger.Warn("compaction returned without compacting, falling back to compressor",
				"utilization", utilization,
			)
			f.compactionFallbacks.Add(1)
		}
	}

	// Layer 2: Proactive compression: run the multi-stage compressor before the
	// legacy pipeline so that the more granular thresholds can reduce
	// context pressure early.
	if f.compressor != nil {
		cr := f.compressor.Compress(ctx, result, utilization)
		if cr.Compressed {
			f.logger.Info("proactive compression applied",
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
	// Uses the configured summaryModel (a real LLM) to produce a content-aware
	// structured summary. Fallback on failure is to continue without
	// summarization – this is a deliberate scope limitation (the firewall
	// does not perform offline/local summarization). The caller must accept
	// that context pressure remains elevated when summarization fails.
	//
	// LLM-12 FIX: Documented that this stage is scope-limited: on failure
	// the firewall silently continues with the unsummarized context rather
	// than escalating or blocking. This means the subsequent request may
	// still hit the hard limit.
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

	return result
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
		end := min(i+maxChars, len(content))
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
// Includes ToolCalls (function name + arguments) and ToolCallID fields which
// are serialized to the API and consume tokens.
func (f *ContextFirewall) countTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += f.countMessageTokens(msg)
	}
	return total
}

// countMessageTokens returns the token count for a single ChatMessage,
// accounting for Content, ToolCalls (function name + arguments),
// ToolCallID, and Name fields.
func (f *ContextFirewall) countMessageTokens(msg ChatMessage) int {
	total := f.tokenizer.CountTokens(msg.Content)
	for _, tc := range msg.ToolCalls {
		total += f.tokenizer.CountTokens(tc.Function.Name)
		total += f.tokenizer.CountTokens(tc.Function.Arguments)
	}
	if msg.ToolCallID != "" {
		total += f.tokenizer.CountTokens(msg.ToolCallID)
	}
	if msg.Name != "" {
		total += f.tokenizer.CountTokens(msg.Name)
	}
	return total
}

// summarizeOldHistory summarizes old messages, keeping the system prompt and last few messages.
// When HierarchicalSummarization is enabled, the summary is recursively re-summarized
// if it exceeds SummaryLevelThreshold tokens, up to MaxSummaryLevel depth.
func (f *ContextFirewall) summarizeOldHistory(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
	return f.summarizeWithLevel(ctx, messages, 1)
}

// summarizeWithLevel performs summarization at the given level. Level 1 is the
// initial summarization pass. If the resulting summary exceeds
// SummaryLevelThreshold tokens and level < MaxSummaryLevel, the summary is
// recursively re-summarized at level+1.
//
// At every level the method uses content-aware summarization: the prompt asks
// the LLM to return structured sections (DECISIONS, FILES, QUESTIONS, STATUS,
// FINDINGS, SUMMARY). The raw response is parsed into a SummaryExtract and
// then formatted as a compact, information-dense summary message.
func (f *ContextFirewall) summarizeWithLevel(ctx context.Context, messages []ChatMessage, level int) ([]ChatMessage, error) {
	if f.compactor != nil && level == 1 {
		cr := f.compactor.Compact(ctx, messages)
		if cr.Compacted {
			return cr.Messages, nil
		}
		f.logger.Warn("compactor returned without compacting, falling back to legacy")
	}

	// Keep: system prompt + last 4 messages
	keepCount := 4 + 1 // +1 for system

	if len(messages) <= keepCount {
		return messages, nil
	}

	var result []ChatMessage
	var toSummarize []ChatMessage

	for i, msg := range messages {
		switch {
		case msg.Role == RoleSystem && level == 1:
			// Only preserve original system messages at level 1.
			// At higher levels, system-tagged summaries are fair game for re-summarization.
			result = append(result, msg)
		case i >= len(messages)-keepCount:
			result = append(result, msg)
		default:
			toSummarize = append(toSummarize, msg)
		}
	}

	if len(toSummarize) == 0 {
		return messages, nil
	}

	// Build content-aware summarization request
	var conversationText strings.Builder
	for _, msg := range toSummarize {
		fmt.Fprintf(&conversationText, "%s: %s\n", msg.Role, msg.Content)
	}
	summaryPrompt := fmt.Sprintf(structuredSummaryPromptTemplate, conversationText.String())

	summaryResp, err := f.summaryModel.Chat(ctx, []ChatMessage{
		{
			Role:    RoleUser,
			Content: summaryPrompt,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	// Parse the structured response
	extract := parseStructuredSummary(summaryResp.Content)
	narrative := extractNarrative(summaryResp.Content)

	// When the LLM response doesn't contain structured sections (e.g. stub
	// responses or LLMs that ignore the template), fall back to using the
	// raw response content as the narrative so the summary still carries
	// the information through.
	if narrative == "" && len(extract.Decisions) == 0 && len(extract.FilePaths) == 0 && len(extract.KeyFindings) == 0 {
		narrative = summaryResp.Content
	}

	summaryContent := formatStructuredSummary(level, extract, narrative)

	f.logger.Debug("structured summary extracted",
		"level", level,
		"decisions", len(extract.Decisions),
		"file_paths", len(extract.FilePaths),
		"unresolved", len(extract.UnresolvedQuestions),
		"findings", len(extract.KeyFindings),
		"extract_json", marshalExtractAsJSON(extract),
	)

	summaryMsg := ChatMessage{
		Role:         RoleSystem,
		Content:      summaryContent,
		SummaryLevel: level,
	}

	// Hierarchical summarization: if the summary itself exceeds the threshold,
	// and we haven't hit max depth, re-summarize.
	if f.config.HierarchicalSummarization && level < f.config.MaxSummaryLevel {
		summaryTokens := f.tokenizer.CountTokens(summaryContent)
		if summaryTokens > f.config.SummaryLevelThreshold {
			f.logger.Debug("hierarchical summarization: re-summarizing at next level",
				"current_level", level,
				"next_level", level+1,
				"summary_tokens", summaryTokens,
				"threshold", f.config.SummaryLevelThreshold,
			)

			// Build a synthetic message list from the summary + kept messages
			// so the recursive call can summarize the summary itself.
			subMessages := make([]ChatMessage, 0, 1+len(result))
			subMessages = append(subMessages, summaryMsg)
			subMessages = append(subMessages, result...)

			return f.summarizeWithLevel(ctx, subMessages, level+1)
		}
	}

	// Prepend summary to the kept messages
	final := append([]ChatMessage{summaryMsg}, result...)

	return final, nil
}

// ValidateContextSize checks if the context size exceeds the model limit.
// Returns a ContextSizeExceededError error if the limit is exceeded.
func (f *ContextFirewall) ValidateContextSize(messages []ChatMessage) error {
	if f.model == nil || f.model.ContextLimit == 0 {
		return nil // No limit configured
	}

	estimated := f.countTokens(messages)
	modelLimit := f.model.ContextLimit

	if estimated > modelLimit {
		return &ContextSizeExceededError{
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

// Config returns the model configuration of the inner chatter.
func (f *ContextFirewall) Config() *ModelConfig {
	if f.model != nil {
		return f.model
	}
	return f.inner.Config()
}

// Ensure ContextFirewall implements Chatter
var _ Chatter = (*ContextFirewall)(nil)
