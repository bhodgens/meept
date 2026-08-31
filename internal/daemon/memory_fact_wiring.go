package daemon

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/memory"
)

// wireMemoryFactExtraction registers the leaf 12 (C6) session-end fact
// extractor on the agent loop. After each turn closes, the turn's dialogue
// (user input + final assistant text) runs through the conservative v1
// heuristic extractor and the resulting typed facts are upserted into the
// FactStore (last-write-wins on OwnerID+Kind+Key). Daemon-owner facts carry
// an empty owner_id; multiuser owner attribution arrives with session
// ownership wiring.
//
// Extraction is advisory: any error is logged and swallowed — a failed
// extraction must never fail the turn. Runs synchronously in the session-end
// hook chain (sqlite upserts are local and fast); facts never block or
// corrupt the turn's response.
func wireMemoryFactExtraction(agentLoop *agent.AgentLoop, factStore *memory.FactStore, logger *slog.Logger) {
	if agentLoop == nil || factStore == nil {
		return
	}

	hr := agentLoop.HookRegistry()
	if hr == nil {
		return
	}

	hook := &memoryFactExtractHook{store: factStore, logger: logger.With("hook", "memory-fact-extract")}
	hr.RegisterSessionEndHook("memory_fact_extract", agent.HookPriorityMonitor+1, hook)
	logger.Info("memory fact extraction wired")
}

// memoryFactExtractHook implements agent.SessionEndHook for leaf 12 fact
// extraction.
type memoryFactExtractHook struct {
	store  *memory.FactStore
	logger *slog.Logger
}

func (h *memoryFactExtractHook) OnSessionEnd(ctx context.Context, state agent.SessionLifecycleState, result agent.SessionLifecycleResult) error {
	if h.store == nil || result.UserMessage == "" {
		return nil
	}

	msgs := []string{result.UserMessage}
	if result.AssistantMessage != "" {
		msgs = append(msgs, result.AssistantMessage)
	}

	facts := memory.ExtractFactsFromMessages(msgs)
	if len(facts) == 0 {
		return nil
	}

	memory.StampFacts(facts, "", state.SessionID, result.EndTime)

	for _, f := range facts {
		if err := h.store.Upsert(ctx, f); err != nil {
			h.logger.Warn("fact upsert failed",
				"session", state.SessionID,
				"kind", f.Kind,
				"key", f.Key,
				"error", err,
			)
		}
	}
	h.logger.Debug("facts extracted", "session", state.SessionID, "count", len(facts))
	return nil
}
