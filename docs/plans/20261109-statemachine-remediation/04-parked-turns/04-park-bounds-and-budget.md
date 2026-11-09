# Park Bounds + Budget Persistence - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** M-4-park (unbounded quota re-park loop) + F5 (budget parker memory-only, invisible on quota_wait topic).
- **Dependencies:** 01-parker-identity.md
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** F4 (unbounded quota re-park), F5 (budget persistence + events)

## Goal

Every park class has a bounded give-up horizon; budget-parked turns
survive restarts and are visible to clients.

## Context

- internal/agent/handler.go:2668-2689 (resumeQuotaParkedTurn re-parks with no attempt counter); quota_resume.go:98-114 (quotaTurnToRecord sets neither Attempt nor MaxAttempts); parker never reads MaxAttempts (grep-verified)
- internal/agent/budget_resume.go:76-86 (no ParkPersistence seam), :112-124 (drop-on-stop warn), no park events; handler.go:1215-1228 budget park site
- Throttle's bounded shape (loop_park.go:210-238 attempt-grown backoff -> ThrottleGiveUpError) is the model.

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Quota re-park bound:
// quotaTurnToRecord sets Attempt = prior Attempt+1 on re-park; parker honors MaxAttempts
// (TurnParker gains give-up on rec.MaxAttempts>0 && rec.Attempt>=rec.MaxAttempts -> give-up callback -> handler marks turn failed terminal, same shape as quota give-up handler.go:1256-1258).
// Default quota MaxAttempts: 10 (config-adjacent constant beside QuotaRetry.MaxWait; no new config key unless a queue config section exists).
// Budget persistence + events:
// BudgetResumeWatcher gains SetParkPersistence(store, ParkKindChat) + owner "budget" + SetParkEventBus (park/resume events on agent.quota_wait with class=budget).
```

### What This Leaf Consumes

- C-K1 owner semantics.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Attempt growth on re-park | Modify: handler.go resume path + quota_resume.go payload | gate TestQuotaReparkAttemptCap GREEN (partially) | no counter | GREEN: Attempt increments across re-parks | Given quota re-park; When re-parked again; Then Attempt+1 persisted |
| T2 | Give-up at cap | Modify: parked_turn.go (drain honors MaxAttempts) + handler.go give-up terminal | gate test GREEN | parker ignores MaxAttempts | GREEN: at cap -> failed terminal (client informed), row deleted | Given Attempt>=MaxAttempts; When drain fires; Then give-up terminal, no LLM call |
| T3 | Budget persistence | Modify: budget_resume.go + handler.go:1215 site + components.go wiring | gate TestBudgetParkPersists GREEN | no seam | GREEN: budget park row survives restart, owner="budget" | Given budget-parked turn; When parkers restart; Then resumed by budget watcher only |
| T4 | Budget events | Modify: budget_resume.go (SetParkEventBus) | gate TestBudgetParkEmitsEvent GREEN | silent | GREEN: park + resume emit on agent.quota_wait with budget class; TUI wait label shows | - |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] Give-up horizon finite for ALL park classes now (quota=budget=throttle=bounded; gate=approval-driven release per leaf 03)
- [ ] No payload reshape (budget event reuses quota_wait schema with class field)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Cap constant placement matches repo config conventions (constant, not config key, unless section exists)
- [ ] Budget watcher Stop() no longer silently drops rows (persisted)

## Notes

The give-up terminal must go through the SAME handler path as the existing
quota give-up (handler.go:1256-1258) - reuse, don't fork.
