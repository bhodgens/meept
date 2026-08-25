# Scheduler Crash Safety (Claim + Coalesce) - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Claim-before-deliver tick semantics; missed ticks coalesce instead of accumulating.
- **Deps:** none | **Context:** 45K | **Group:** A

## Goal

Daemon crash between tick-fire and job-completion loses or duplicates work depending on store state. prime-agent semantics: claim each tick atomically BEFORE delivery; a claim row surviving a crash marks the tick done (no re-run) — and missed ticks COALESCE to the latest per job per wake, so a daemon down for 6 hours of every-5-min jobs wakes to ONE run, not seventy-two.

## Context

internal/scheduler — find store interface + tick loop via search_files ("func.*Scheduler", "tick"). SQLite persistence exists. This leaf adds a claimed_ticks table + loop changes.

Key files: internal/scheduler/*.go, its store, tests.

## Interface Contracts (From Parent)

```go
func (s *Scheduler) ClaimTick(jobID string, tick time.Time) (claimed bool, err error)
// INSERT INTO scheduler_claimed_ticks(job_id, tick_time) VALUES(?,?);
// unique(job_id,tick_time); constraint violation -> claimed=false,nil.
// Table created in existing migration path.
```

Loop behavior:
- On wake: compute due ticks since lastWake. Group by job; take MAX(tick). Claim it; if claimed -> enqueue once with metadata missed_count=N (N = count skipped) logged info-level.
- On normal fire: ClaimTick before dispatch; false -> skip (already delivered pre-crash).
- Retention: prune claims older than 7d on startup (configurable [scheduler] claim_retention_days).
- Config [scheduler]: coalesce_missed=true default; false restores accumulate-each behavior (documented).

## Tasks
1. Failing tests: double-claim second false; crash-sim (claim then "restart" new Scheduler same DB -> no re-delivery); coalescing (3 due ticks -> one enqueue, missed_count=2); coalesce=false -> three enqueues; retention prune.
2. Implement table+method+loop changes.
3. Docs paragraph in docs/workflows/job-scheduling.md.

## Self-Verification Checklist
- [ ] -race green internal/scheduler
- [ ] No busy-loop when nothing due
- [ ] Existing job CRUD tests untouched/passing

## Review Checklist
- [ ] UNIQUE constraint actually in DDL
- [ ] Time handling UTC-consistent with existing code
- [ ] Errors %w

Output: APPROVED or gaps. Notes: this changes observable behavior (fewer catch-up runs) — call it out in docs changelog note.
