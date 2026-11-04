package agent

import (
	"github.com/caimlas/meept/internal/memory"
)

// Recall outcome telemetry (leaf 04, contract C5): which memory items a
// turn INJECTED into its prompt, so the turn.terminal subscriber can write
// one recall_outcomes.jsonl row per item stating whether the final reply
// cited it.

// RecalledSnapshotItems converts prompt-facing recall results into the C5
// snapshot rows (one per injected item; id, query, salient tokens, token
// estimate). The render-site callers stage these so the terminal path can
// consume them pre-render.
func RecalledSnapshotItems(results []memory.MemoryResult, query string) []memory.RecallOutcomeRecord {
	if len(results) == 0 {
		return nil
	}
	items := make([]memory.RecallOutcomeRecord, 0, len(results))
	for _, r := range results {
		if r.Memory.ID == "" || r.Memory.Content == "" {
			continue // unidentifiable item: no row (conservative)
		}
		items = append(items, memory.RecallOutcomeRecord{
			MemoryID: r.Memory.ID,
			Query:    query,
			Tokens:   memory.TokenEstimate(r.Memory.Content),
			Salient:  memory.SalientTokens(r.Memory.Content),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// recallSnapshotFromItems converts assembled loop-side memoryRecallItems
// into the C5 snapshot rows (identifiable items only).
func recallSnapshotFromItems(items []memoryRecallItem) []memory.RecallOutcomeRecord {
	if len(items) == 0 {
		return nil
	}
	out := make([]memory.RecallOutcomeRecord, 0, len(items))
	for _, item := range items {
		if item.id == "" || item.text == "" {
			continue
		}
		out = append(out, memory.RecallOutcomeRecord{
			MemoryID: item.id,
			Query:    item.query,
			Tokens:   memory.TokenEstimate(item.text),
			Salient:  memory.SalientTokens(item.text),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DrainRecallOutcomeSnapshot drains the staged injected-memory snapshot for
// a conversation across the given loops (leaf 04 T3). The daemon's
// turn-terminal wiring passes its loop sources (singleton + per-session
// manager loops); the first loop holding a conversation with a staged
// snapshot wins. The drain is consuming (the conversation clears on read),
// so exactly one terminal event consumes each turn's injection set. Loops
// whose conversation holds nothing are untouched.
func DrainRecallOutcomeSnapshot(conversationID string, loops ...*AgentLoop) []memory.RecallOutcomeRecord {
	if conversationID == "" {
		return nil
	}
	for _, loop := range loops {
		if loop == nil {
			continue
		}
		conv := loop.SessionConversation(conversationID)
		if conv == nil {
			continue
		}
		if items := conv.RecallOutcomeSnapshot(); len(items) > 0 {
			return items
		}
	}
	return nil
}

// recallOutcomeSnapshot stages the injected-item snapshot onto the executing
// loop's conversation for the turn (T2). RouteToAgent calls this AFTER the
// per-agent re-scope resolved the final prompt-facing set and BEFORE
// buildContextMessage renders it — so what is staged is exactly what the
// model will see. The turn.terminal capture subscriber drains it. Empty
// recall explicitly clears the conversation's staged snapshot (no rows may
// be fabricated for context the model never saw).
func (d *Dispatcher) recallOutcomeSnapshot(result *DispatchResult, loop *AgentLoop, conversationID string, results []memory.MemoryResult) {
	if d == nil || loop == nil || conversationID == "" {
		return
	}
	conv := loop.SessionConversation(conversationID)
	if conv == nil {
		return
	}
	conv.SetRecallOutcomeSnapshot(RecalledSnapshotItems(results, result.OriginalInput))
}
