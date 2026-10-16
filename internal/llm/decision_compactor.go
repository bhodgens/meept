package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// DecisionCompactor: decision-model-only compaction lane (decision-model
// tree leaf 04, docs/plans/decision-model/04-decision-compaction.md,
// Contract E in master.md).
//
// Score-and-drop compaction: every history entry gets ONE score question
// ("does this entry still matter for the current task?" 0-4 rubric, entry
// text as State) against the DecisionClient. Entries scoring >= keepBar
// stay VERBATIM; below-bar entries are dropped — survivors are never
// rewritten. Any client error on an entry keeps that entry (fail open:
// keep context rather than lose it), logging Warn once per Compact run,
// not per entry.
//
// The lane replaces the summarizer Chatter at ContextCompressor stage 2
// ONLY when the decision model is configured AND the opt-in knob is true;
// otherwise the existing summarizer stays the default lane byte-identically
// (see selectSummarizerForCompressor). The Chatter seam is chat-shaped
// (Chat / ChatWithProgress / Config), so the compactor implements it and
// adapts the compressor's summarizeWithLLM message shape (system messages +
// one user message of "[role]: content" lines) into per-entry score
// questions, emitting the survivors as a summary-style system message so
// the compressor's stage-2 contract holds.
type DecisionCompactor struct {
	client  *DecisionClient
	keepBar int
	logger  *slog.Logger
}

// NewDecisionCompactor builds the decision-model compaction lane. A nil
// client or a client whose Available() is false degrades to keep-everything
// (fail open); keepBar <= 0 falls back to the default bar of 2.
func NewDecisionCompactor(client *DecisionClient, keepBar int, logger *slog.Logger) *DecisionCompactor {
	if keepBar <= 0 {
		keepBar = 2
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DecisionCompactor{client: client, keepBar: keepBar, logger: logger}
}

// decisionScoreOptions is the 0-4 rubric label set (leaf contract: each
// label is ONE token after render).
var decisionScoreOptions = []string{"0", "1", "2", "3", "4"}

// decisionScorePrompt is the leaf-contract score question text.
const decisionScorePrompt = "does this entry still matter for the current task? 0-4"

// decisionScoreQuestion builds the ONE score question for a history entry:
// Type "score", the leaf-contract prompt, the 0-4 rubric labels, the entry
// text as State (leaf contract, task 3).
func decisionScoreQuestion(entry ChatMessage) DecisionQuestion {
	return DecisionQuestion{
		State:   entry.Content,
		Type:    "score",
		Prompt:  decisionScorePrompt,
		Options: decisionScoreOptions,
	}
}

// Compact scores each non-system entry with ONE decision-model question and
// returns the system messages plus entries scoring >= keepBar, in original
// order, verbatim. Entries with a client error are kept (fail open). A tool
// result that survives keeps its parent assistant tool_call entry, mirroring
// keepTail's pairing rule, so the API never sees an orphaned tool result.
func (dc *DecisionCompactor) Compact(ctx context.Context, entries []ChatMessage, currentTask string) []ChatMessage {
	if len(entries) == 0 {
		return nil
	}

	// Degenerate lanes: nil/unavailable client keeps everything.
	if dc == nil || dc.client == nil || !dc.client.Available() {
		return append([]ChatMessage(nil), entries...)
	}

	type scored struct {
		idx   int
		keep  bool
		score string
	}
	scores := make([]scored, 0, len(entries))
	warned := false
	for i, entry := range entries {
		if entry.Role == RoleSystem {
			continue
		}
		res, err := dc.client.Ask(ctx, decisionScoreQuestion(entry))
		if err != nil {
			// Fail open: keep the entry. Warn ONCE per Compact run.
			if !warned {
				dc.logger.Warn("decision compaction: client error, keeping remaining entries (fail open)",
					"error", err)
				warned = true
			}
			scores = append(scores, scored{idx: i, keep: true})
			continue
		}
		var n int
		if _, scanErr := fmt.Sscanf(res.Answer, "%d", &n); scanErr != nil {
			n = 0
		}
		scores = append(scores, scored{idx: i, keep: n >= dc.keepBar, score: res.Answer})
	}

	// Keep set: system messages always; scored entries per the bar; a kept
	// tool result pulls its parent assistant tool_call entry back in
	// (keepTail D1 pairing rule).
	keepSet := make(map[int]bool, len(entries))
	for i, entry := range entries {
		if entry.Role == RoleSystem {
			keepSet[i] = true
		}
	}
	for _, s := range scores {
		if s.keep {
			keepSet[s.idx] = true
			entry := entries[s.idx]
			if entry.Role == RoleTool && entry.ToolCallID != "" {
				for j := s.idx - 1; j >= 0; j-- {
					parent := entries[j]
					if parent.Role != RoleAssistant {
						continue
					}
					for _, tc := range parent.ToolCalls {
						if tc.ID == entry.ToolCallID {
							keepSet[j] = true
							break
						}
					}
					if keepSet[j] {
						break
					}
				}
			}
		}
	}

	out := make([]ChatMessage, 0, len(entries))
	for i, entry := range entries {
		if keepSet[i] {
			// Verbatim survivor — no rewrite, ever.
			out = append(out, entry)
		}
	}

	dc.logger.Debug("decision compaction pass",
		"entries", len(entries),
		"kept", len(out),
		"keep_bar", dc.keepBar,
		"current_task", currentTask,
	)
	return out
}

// Chat satisfies the Chatter seam so the compactor can sit in the
// summarizer arg of NewContextCompressor. The compressor's summarizeWithLLM
// sends system messages plus ONE user prompt that embeds the old history as
// "[role]: content" lines; Chat extracts those lines as per-entry score
// questions and returns the survivors as the summary content, preserving
// the compressor's stage-2 message contract.
func (dc *DecisionCompactor) Chat(ctx context.Context, messages []ChatMessage, _ ...ChatOption) (*Response, error) {
	currentTask := decisionCompactionTask
	var entries []ChatMessage
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			continue
		}
		if parsed := parseHistoryEntries(msg.Content); len(parsed) > 0 {
			entries = append(entries, parsed...)
			continue
		}
		entries = append(entries, msg)
	}

	kept := dc.Compact(ctx, entries, currentTask)

	var sb strings.Builder
	sb.WriteString("[Kept context entries (score >= keep bar, verbatim)]\n")
	for _, msg := range kept {
		if msg.Role == RoleSystem {
			continue
		}
		fmt.Fprintf(&sb, "[%s]: %s\n", msg.Role, msg.Content)
	}

	return &Response{Content: sb.String()}, nil
}

// ChatWithProgress satisfies Chatter; progress reporting is not applicable
// to a typed-decision lane.
func (dc *DecisionCompactor) ChatWithProgress(ctx context.Context, messages []ChatMessage, _ ProgressCallback, opts ...ChatOption) (*Response, error) {
	return dc.Chat(ctx, messages, opts...)
}

// Config satisfies Chatter; the lane rides the decision model's resolver
// config, so it reports an empty ModelConfig (the compressor never reads it).
func (dc *DecisionCompactor) Config() *ModelConfig { return &ModelConfig{} }

// decisionCompactionTask is the current-task label recorded in Compact's
// debug log when the compressor drives the lane through Chat (the task text
// is not carried by the Chatter seam).
const decisionCompactionTask = "context compaction"

// parseHistoryEntries splits the compressor's "[role]: content" history
// block back into per-entry messages. Line breaks inside an entry's content
// keep attaching to the current entry; only a line starting a new
// "[role]: " prefix begins the next entry.
func parseHistoryEntries(content string) []ChatMessage {
	var out []ChatMessage
	var cur *ChatMessage
	for _, line := range strings.Split(content, "\n") {
		if role, text, ok := splitHistoryLine(line); ok {
			out = append(out, ChatMessage{Role: role, Content: text})
			cur = &out[len(out)-1]
			continue
		}
		if cur != nil {
			cur.Content += "\n" + line
		}
	}
	return out
}

// splitHistoryLine parses one "[role]: content" line from the compressor's
// rendered history block.
func splitHistoryLine(line string) (Role, string, bool) {
	if !strings.HasPrefix(line, "[") {
		return "", "", false
	}
	close := strings.Index(line, "]")
	if close < 0 {
		return "", "", false
	}
	role := Role(line[1:close])
	rest := line[close+1:]
	if !strings.HasPrefix(rest, ": ") {
		return "", "", false
	}
	return role, rest[2:], true
}

// selectSummarizerForCompressor picks the stage-2 summarizer at
// ContextCompressor construction: the DecisionCompactor lane ONLY when the
// opt-in knob is on AND the decision client is available; otherwise the
// existing summarizer argument passes through unchanged (byte-identical
// legacy path). Exposed for the firewall constructor and for tests pinning
// the lane selection.
func selectSummarizerForCompressor(cfg ContextFirewallConfig, summarizer Chatter) Chatter {
	if cfg.DecisionCompaction && cfg.DecisionClient.Available() {
		return NewDecisionCompactor(cfg.DecisionClient, cfg.DecisionKeepBar, nil)
	}
	return summarizer
}
