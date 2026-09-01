# Interactive Queue Priority - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root of tree 04 in the llm-resilience-forest)
- **Children:** 3 leaf documents under this node
- **Scope:** When the user is interacting, work originating from that
  session wins the queue and model-concurrency contention over
  background agent work.

Read `../SHARED-CONVENTIONS.md` (§4.4 is THIS tree's contract) and
`../DECISIONS.md` (D11, Q1). Independent of trees 01/02/03/05 — no
cross-tree dependency (uses only its own contract §4.4).

## Goal

Agents interacting upstream of the user (planning, chat-adjacent work)
should not wait behind bulk background jobs when the user is actively
typing. D11 defines "interactive": a recent user message on the
originating session (Q1 default window: 5 minutes) OR the session's
foreground-session flag. (WS presence was considered and NOT ratified.)

This tree delivers:

1. **Session interactivity signal** — a small session-service helper
   answering IsInteractive(sessionID) from the two ratified inputs.
2. **Job-level flag** — jobs carry Interactive (SHARED-CONVENTIONS
   §4.4); the enqueue site stamps it from the originating session.
3. **Queue ordering** — ClaimNextForAgent orders by (interactive DESC,
   priority DESC, created_at ASC) — the claim SQL (internal/queue/
   store.go:379/390, 624, 684 already orders priority DESC) gains the
   leading term + index.
4. **Model-slot fairness (the "concurrent connection per model" half).**
   MaxConcurrency semaphores (internal/llm/client.go:269-270, also :249 via WithConcurrencyLimit) are FIFO;
   under contention an interactive turn must not queue behind every
   background waiter. The CLIENT SEMAPHORE PATH (acquireConcurrencyLimit,
   client.go:1760) gains a priority-aware
   acquire: interactive acquires jump the wait list. (inuse.go is
   boot-time startup gating only — not a gate site.) Scope: implement
   via a two-lane semaphore replacing the raw channel; if the audit
   finds the acquires are pure channel sends (no queue — expected),
   build the slotGate wait list (leaf 03's Outcome 2), or
   document that interactive priority applies at the QUEUE layer only
   and note the semaphore gap explicitly (do not fake it).

## Architecture

Signal: session service already tracks LastActivity (internal/session/
session.go:71) and AttachedClients; the foreground flag is a session
field set by the client (TUI/GUI) — leaf 01 adds `Foreground bool`
persistence + the IsInteractive helper with the Q1 window from config
(`queue.interactive_window`, default 5m). Stamp: the QueueService enqueue
path carries session_id today (services/queue_service.go:50-52) — leaf 02
stamps Interactive there; other producers (tactical planner/analyst jobs)
carry NO session provenance yet and need StepJobPayload extended first
(see leaf 02 Task 3).
Order: one SQL term + one index (leaf 02). Fairness: leaf 03 audits the
semaphore and either implements the two-lane acquire or documents the
gap — its Deviations decide.

## Interface Contracts

### Contract 1: Interactivity signal

```go
// internal/session/interactive.go (new)
// IsInteractive reports whether the session had a user message within
// the configured window or holds the foreground flag.
func IsInteractive(s *Session, now time.Time, window time.Duration) bool
// Config: queue.interactive_window (duration, default "5m") —
// internal/config/schema.go + defaults + docs.
```

- Owner: 01-session-signal.md
- Consumers: 02-queue-priority.md

### Contract 2: Job flag + claim ordering (frozen §4.4)

```go
// internal/queue/job.go: Interactive bool `json:"interactive,omitempty"`
// internal/queue/store.go: migration adds the column (0/1) +
//   idx_jobs_claim on (state, interactive DESC, priority DESC, created_at)
// Claim SQL ORDER BY: interactive DESC, priority DESC, created_at ASC
// Enqueue stamp: interactive = session.IsInteractive(originSession, now, window)
```

- Owner: 02-queue-priority.md
- Consumers: 03-model-slot-fairness.md (context only)

### Contract 3: Model-slot acquisition (honest-or-real)

```go
// internal/llm/inuse.go or client.go semaphore path:
// EITHER Acquire(ctx, cfg, priority bool) — interactive skips ahead of
//   waiting background acquires (two-lane),
// OR a documented audit finding: no wait list exists; queue-layer-only
//   priority; gap recorded in Deviations + docs.
```

- Owner: 03-model-slot-fairness.md
- Consumers: none (terminal)

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-session-signal.md | leaf | none | 50K | A |
| 02 | 02-queue-priority.md | leaf | 01 | 70K | B |
| 03 | 03-model-slot-fairness.md | leaf | 02 | 60K | C |

**Concurrency groups:** strictly sequential A → B → C.

## Dispatch Protocol

- Leaf 01: verify `go test ./internal/session/ -run TestIsInteractive -v`
  + config defaults. Commit: `feat(session): interactivity signal (tree 04 leaf 01)`.
- Leaf 02: verify claim-ordering tests (interactive job beats older
  higher-priority background job); migration tested. Commit:
  `feat(queue): interactive-first claim ordering (tree 04 leaf 02)`.
- Leaf 03: verify semaphore audit outcome — tests for two-lane acquire,
  or the documented gap. Commit:
  `feat(llm): interactive model-slot acquisition (tree 04 leaf 03)` or
  `docs(llm): model-slot priority gap audit (tree 04 leaf 03)`.
In-session review per leaf; max 3 re-dispatch cycles.

## Review Checklist

- [ ] Leaf tasks complete; tests pass; contracts satisfied
- [ ] Foreground flag default false; multi-user semantics untouched (OwnerID filtering intact)
- [ ] Claim ordering migration reversible and tested (fresh DB + migrated DB)
- [ ] No new bus topics; `make graphs` clean
- [ ] Docs updated (config knob, ordering semantics)
- [ ] gofmt/vet/analyzers clean; no artifacts

Output: APPROVED or specific gaps.

## Coding Conventions

Pass `../SHARED-CONVENTIONS.md` §1-§3. SQL: parameterized only (the
store's existing style); migrations follow store.go's pattern.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-session-signal.md | PENDING | 0 | |
| 02-queue-priority.md | PENDING | 0 | |
| 03-model-slot-fairness.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./... && go test ./internal/session/... ./internal/queue/... ./internal/llm/... -count=1`.
2. E2E: seed queue with a background job (priority high, old) + an
   interactive job (priority normal, new, interactive session) →
   ClaimNextForAgent returns the INTERACTIVE job first.
3. Window expiry: the same job claimed after the session goes quiet
   (window passed, enqueue-time flag already set) → still interactive
   (flag is stamped at enqueue — assert and document this choice).
4. Semaphore: 3 background + 1 interactive waiting on MaxConcurrency=1
   → interactive acquires next (or the audit gap is documented).
5. `make analyzers`; `make graphs`; AGENTS.md review in final commit.

## Structural Completeness Check (Before Dispatch)

`python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep 04-scheduling`

## Notes

- The flag-stamped-at-enqueue choice (Integration Test 3) means a job
  keeps interactive status for its whole life even if the session goes
  quiet. This matches "agents interacting upstream" semantics (the WORK
  was user-adjacent when created) and avoids re-classification churn.
  If the user wants expiry, that is a follow-up decision.
- Chat turns themselves bypass the queue (direct loop dispatch) — this
  tree covers QUEUED work (planner/analyst/project tasks). State this
  scope in the docs leaf so users do not expect chat reordering.
