# Loop Guards: No-Progress Ladder, Search Rollback, Reasoning Watchdog - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Extend existing loop safety: no-progress veto ladder, duplicate-web-search rollback w/ free re-sample, reasoning-only watchdog.
- **Deps:** none | **Context:** 60K | **Group:** B

## Goal

features.md documents a cycle detector (SHA256 tool+args) and convergence detector. Three failure shapes remain uncovered: (a) near-identical-but-not-byte-identical calls looping (no progress); (b) identical web_search re-fired burning budget — rollback the turn and re-sample without counting an iteration; (c) reasoning-only streaks (extended thinking) exceeding token/time budgets with no tool/text output.

## Context

internal/agent/loop.go: cycle detector + convergence detector sites (locate via search_files "cycle" / "convergence" in loop.go); iteration budget handling; reasoning content arrives via native Anthropic driver (llm package). AGENTS.md conventions.

Key files: internal/agent/loop.go (+_test), config [agent.guards].

## Interface Contracts (From Parent)

```go
// internal/agent/guards.go (new):
type GuardConfig struct {
    NoProgressWarnAt    int  `json:"no_progress_warn_at"`    // default 3
    NoProgressVetoAt    int  `json:"no_progress_veto_at"`    // default 5
    GracefulAfterVetoes int  `json:"graceful_after_vetoes"`  // default 3
    DuplicateSearchRollback bool `json:"duplicate_search_rollback"` // default FALSE until proven
    RollbackWindow      int  `json:"rollback_window"`        // turns, default 10
    ReasoningTokenCap   int  `json:"reasoning_token_cap"`    // per-streak, default 16384
    ReasoningStreakTurns int `json:"reasoning_streak_turns"` // default 3
}

type NoProgressLadder struct{ /* normalized hashes of tool name+normalized args (whitespace-collapsed JSON keys sorted) */ }
// Track(warnAt,vetoAt): returns "ok"|"warn"|"veto".
type SearchRollback struct{ window ring of search-arg hashes }
// ShouldRollback(argsHash) bool
```

Loop integration:
- veto -> inject system nudge "no measurable progress; change approach"; after GracefulAfterVetoes consecutive vetoes -> terminate turn gracefully w/ partial results summary (reuse existing graceful wrap-up path).
- rollback (when enabled): on pre-exec detection of duplicate web_search within window -> pop last assistant+tool pair from conversation, re-sample same iteration (iteration counter NOT incremented), log info. MUST preserve message-tree consistency (session store parent chain) — if pop is unsafe mid-store, alternative: mark pair compacted-away via existing compaction entry mechanism; choose whichever composes cleanly, document choice.
- watchdog: track consecutive assistant turns w/ reasoning tokens but empty text+no tool calls; exceed caps -> inject nudge forcing textual/tool response; second breach terminates gracefully.

Config [agent.guards] all values overridable; zero-values = defaults above via normalize func.

## Tasks
1. Failing unit tests ladder (hash normalization: reordered JSON keys equal), rollback ring, watchdog streak counting.
2. Failing loop-integration tests using existing loop test harness patterns (fake LLM client emitting scripted responses): scripted 6x near-same tool call -> warn@3 nudge injected, veto@5, graceful end after 3rd veto; scripted duplicate search w/ flag on -> iteration count unchanged after rollback; reasoning-only script -> nudge then termination.
3. Implement guards.go + loop wiring at existing detector sites (compose, don't fork).
4. Config plumbing + docs section appended to features.md agent-loop area or workflows page.

## Self-Verification Checklist
- [ ] -race green internal/agent
- [ ] Default flags keep current behavior EXCEPT ladder+watchdog (on-by-default at conservative thresholds) — document
- [ ] Session store consistency proven by existing persistence tests still passing

## Review Checklist
- [ ] Normalized hashing cannot collide distinct calls trivially (test)
- [ ] Nudges use existing system-injection path (anchor-message style), not raw prompt concat
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: rollback is the riskiest piece — behind flag off by default; ship ladder/watchdog first mentally even though one leaf.
