package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

// CompactionConfig controls how the compactor reduces turn and tool-call data.
type CompactionConfig struct {
	// MaxTurnsBeforeCompact triggers compaction when the turn list exceeds
	// this threshold. Defaults to 50.
	MaxTurnsBeforeCompact int

	// MaxToolCallsPerTurn limits how many tool calls are preserved per turn.
	// Extra calls beyond this are discarded. Defaults to 10.
	MaxToolCallsPerTurn int

	// KeepLastNTurns is the number of most-recent turns that are never
	// compacted regardless of config. Defaults to 10.
	KeepLastNTurns int

	// LLM is an optional chat client used for intelligent summarization of
	// compacted regions. When nil, GenerateSummary produces heuristic text
	// instead.
	LLM llm.Chatter
}

// CompactionResult reports the stats and summary produced by a compaction run.
type CompactionResult struct {
	// OriginalTurnCount is the number of turns before compaction.
	OriginalTurnCount int

	// CompactedTurnCount is the number of turns after compaction.
	CompactedTurnCount int

	// CompressionRatio is the fraction of turns retained (0.0-1.0).
	// A ratio of 0.4 means 60% of turns were compacted away.
	CompressionRatio float64

	// Summary is a human-readable description of what was compacted.
	Summary string
}

// ToolCall represents a tool invocation for compaction purposes.
type ToolCall struct {
	ToolName  string
	Arguments string // JSON-encoded argument map
	Result    string
	Seq       int
}

// TurnRecord is a memory-level turn record that may contain tool calls,
// thinking content, or plain observations.
type TurnRecord struct {
	Index      int
	Type       string // "tool", "thinking", "observation", "final", "user"
	ToolName   string
	ToolInput  string // JSON-encoded
	ToolOutput string
	Content    string
	Tokens     int
	ToolCalls  []ToolCall
}

// Compactor compacts tool calls and turns for memory retention.
type Compactor struct {
	cfg CompactionConfig
	mu  sync.RWMutex
}

// NewCompactor creates a compactor with the given configuration. Zero
// values in the config are replaced by the documented defaults.
func NewCompactor(cfg CompactionConfig) *Compactor {
	if cfg.MaxTurnsBeforeCompact <= 0 {
		cfg.MaxTurnsBeforeCompact = 50
	}
	if cfg.MaxToolCallsPerTurn <= 0 {
		cfg.MaxToolCallsPerTurn = 10
	}
	if cfg.KeepLastNTurns <= 0 {
		cfg.KeepLastNTurns = 10
	}
	return &Compactor{cfg: cfg}
}

// Config returns a copy of the compactor's configuration.
func (c *Compactor) Config() CompactionConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// Compact runs the full compaction pipeline on the given turns and returns
// both a result report and the compacted turn list.
//
// Pipeline:
//
//	1. If len(turns) <= MaxTurnsBeforeCompact, return unchanged (ratio 1.0).
//	2. Reserve the last KeepLastNTurns turns unconditionally.
//	3. Merge consecutive tool + observation pairs into single calls.
//	4. Deduplicate adjacent observation turns (longer content wins).
//	5. Trim each turn's ToolCalls slice to MaxToolCallsPerTurn.
//	6. When the compactable region is large, replace its middle with a summary turn.
func (c *Compactor) Compact(ctx context.Context, turns []TurnRecord) (*CompactionResult, []TurnRecord) {
	c.mu.RLock()
	cfg := c.cfg
	c.mu.RUnlock()

	if len(turns) <= cfg.MaxTurnsBeforeCompact {
		return &CompactionResult{
			OriginalTurnCount:  len(turns),
			CompactedTurnCount: len(turns),
			CompressionRatio:   1.0,
			Summary:            "no compaction needed below threshold",
		}, copyTurns(turns)
	}

	// Reserve last N turns.
	protected := cfg.KeepLastNTurns
	if protected >= len(turns) {
		protected = len(turns)
	}

	compactable := make([]TurnRecord, len(turns)-protected)
	copy(compactable, turns[:len(turns)-protected])
	kept := make([]TurnRecord, protected)
	copy(kept, turns[len(turns)-protected:])

	// Phase 3: trim tool-calls arrays first (cheap, before any merges).
	for i := range compactable {
		compactable[i].ToolCalls = trimmedCalls(compactable[i].ToolCalls, cfg.MaxToolCallsPerTurn)
	}

	// Phase 1: merge consecutive tool + observation pairs.
	compactable = mergePairs(compactable)

	// Phase 2: deduplicate adjacent observation turns.
	compactable = dedupObservations(compactable)

	// Phase 4: replace the middle of a large compacted region with a summary.
	if len(compactable) > 5 {
		compactable = summarizeMiddle(ctx, compactable, c)
	}

	final := append(compactable, kept...)

	result := &CompactionResult{
		OriginalTurnCount:  len(turns),
		CompactedTurnCount: len(final),
		Summary: fmt.Sprintf("compacted %d turns (%.0f%% reduction), kept last %d",
			len(turns)-len(final),
			float64(len(turns)-len(final))/float64(len(turns))*100,
			protected),
	}
	result.CompressionRatio = float64(result.CompactedTurnCount) / float64(result.OriginalTurnCount)

	return result, final
}

// copyTurns returns a shallow copy of the slice.
func copyTurns(src []TurnRecord) []TurnRecord {
	out := make([]TurnRecord, len(src))
	copy(out, src)
	return out
}

// CompactTurns is a convenience wrapper: compacts turns and returns only the list.
func (c *Compactor) CompactTurns(turns []TurnRecord) []TurnRecord {
	_, result := c.Compact(context.Background(), turns)
	return result
}

// CompactToolCalls removes duplicate tool calls, keeping only the first
// occurrence of each (ToolName, Arguments) pair.
func (c *Compactor) CompactToolCalls(calls []ToolCall) []ToolCall {
	seen := make(map[string]struct{}, len(calls))
	out := calls[:0]
	for _, tc := range calls {
		key := tc.ToolName + "\x00" + tc.Arguments
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tc)
	}
	return out
}

// GenerateSummary returns a human-readable summary of the given turns.
// When the compactor's LLM config is set it is used; otherwise a
// heuristic summary is produced.
func (c *Compactor) GenerateSummary(ctx context.Context, turns []TurnRecord) string {
	c.mu.RLock()
	llmClient := c.cfg.LLM
	c.mu.RUnlock()

	if llmClient != nil {
		if s, err := summarizeWithLLM(ctx, llmClient, turns); err == nil {
			return s
		}
	}
	return heuristicSummary(turns)
}

// -----------------------------------------------------------------------
// Private helpers
// -----------------------------------------------------------------------

// mergePairs collapses tool + observation pairs into a single turn.
func mergePairs(turns []TurnRecord) []TurnRecord {
	var out []TurnRecord
	i := 0
	for i < len(turns) {
		t := turns[i]
		if t.Type == "tool" && i+1 < len(turns) && turns[i+1].Type == "observation" {
			out = append(out, TurnRecord{
				Type:       "tool",
				Index:      t.Index,
				ToolName:   t.ToolName,
				ToolInput:  t.ToolInput,
				ToolOutput: turns[i+1].Content,
				Tokens:     t.Tokens + turns[i+1].Tokens,
			})
			i += 2 // skip both tool and observation
		} else {
			out = append(out, t)
			i++
		}
	}
	return out
}

// dedupObservations collapses adjacent observation turns, keeping the longer
// content and summing token counts.
func dedupObservations(turns []TurnRecord) []TurnRecord {
	if len(turns) == 0 {
		return turns
	}
	out := []TurnRecord{turns[0]}
	for i := 1; i < len(turns); i++ {
		prev := &out[len(out)-1]
		if prev.Type == "observation" && turns[i].Type == "observation" {
			if len(turns[i].Content) > len(prev.Content) {
				prev.Content = turns[i].Content
			}
			prev.Tokens += turns[i].Tokens
			continue
		}
		out = append(out, turns[i])
	}
	return out
}

func trimmedCalls(calls []ToolCall, max int) []ToolCall {
	if len(calls) <= max {
		return calls
	}
	return calls[:max]
}

// summarizeMiddle replaces the middle of a large compacted region with a
// summary TurnRecord.
func summarizeMiddle(ctx context.Context, turns []TurnRecord, comp *Compactor) []TurnRecord {
	const headerKeep = 3
	const footerKeep = 2

	if len(turns) <= headerKeep+footerKeep {
		return turns
	}

	header := turns[:headerKeep]
	footer := turns[len(turns)-footerKeep:]
	region := turns[headerKeep : len(turns)-footerKeep]

	summary := comp.GenerateSummary(ctx, region)
	summaryTurn := TurnRecord{
		Type:    "summary",
		Content: summary,
		Tokens:  1,
	}

	result := make([]TurnRecord, 0, len(header)+1+len(footer))
	result = append(result, header...)
	result = append(result, summaryTurn)
	result = append(result, footer...)
	return result
}

// summarizeWithLLM asks the LLM for a one-paragraph summary of turns.
func summarizeWithLLM(ctx context.Context, chatter llm.Chatter, turns []TurnRecord) (string, error) {
	var lines []string
	for _, t := range turns {
		line := fmt.Sprintf("  [%s]", t.Type)
		if t.ToolName != "" {
			line += " tool=" + t.ToolName
		}
		snippet := t.Content
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		if snippet != "" {
			line += " content=" + snippet
		}
		lines = append(lines, line)
	}

	prompt := "Summarize these agent turns into one concise paragraph:\n" + strings.Join(lines, "\n")

	resp, err := chatter.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are a memory compaction assistant. Return only a short paragraph."},
		{Role: llm.RoleUser, Content: prompt},
	})
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("empty LLM response")
	}
	return strings.TrimSpace(resp.Content), nil
}

// heuristicSummary builds a summary without calling any external service.
func heuristicSummary(turns []TurnRecord) string {
	if len(turns) == 0 {
		return "No turns to summarize."
	}

	counts := make(map[string]int)
	totalTokens := 0
	var samples []string

	for _, t := range turns {
		counts[t.Type]++
		totalTokens += t.Tokens
		if len(samples) < 3 {
			snippet := t.Content
			if snippet == "" && t.ToolName != "" {
				snippet = fmt.Sprintf("tool:%s", t.ToolName)
			}
			if snippet != "" {
				if len(snippet) > 120 {
					snippet = snippet[:120]
				}
				samples = append(samples, snippet)
			}
		}
	}

	parts := []string{fmt.Sprintf("%d turns covering %d tokens", len(turns), totalTokens)}
	for tp, n := range counts {
		parts = append(parts, fmt.Sprintf("%dx %s", n, tp))
	}
	parts = append(parts, samples...)

	return "Compacted: " + strings.Join(parts, " | ")
}
