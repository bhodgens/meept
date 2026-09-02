# Universal Turn Park/Resume - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root of tree 03 in the llm-resilience-forest)
- **Children:** 4 leaf documents under this node
- **Scope:** Every turn type parks and resumes on provider failure waits
  (quota AND throttle); no turn ever hangs on a dead connection; parked
  turns release their agent/model slot.

Read `../SHARED-CONVENTIONS.md` (§4.5 is THIS tree's contract) and
`../DECISIONS.md` (D8, D9). Depends on tree 02 (PolicyVerdict,
ThrottleBackoffError, BackoffPlan) reaching COMPLETE for leaves 02+03.

## Goal

Meept is an electric machine: on (working) or off (broken). A turn
blocked on a provider wait is ON — it must be parked, releasing its
slot, and resumed mechanically. Today only chat turns park
(QuotaResumeWatcher, internal/agent/quota_resume.go). Goal-loop turns,
specialist-agent turns, and queue jobs hit the same waits and either
error out or hang. D9: park/resume for ALL turn types.

This tree delivers:

1. **Generalized park** — one ParkedTurnRecord + one watcher serving
   every failure class; QuotaResumeWatcher becomes a thin wrapper whose
   chat-quota behavior stays byte-identical.
2. **Throttle parking** — the agent loop's ThrottleBackoffError branch
   (tree 02 leaf 03 planted it) parks with BackoffPlan scheduling
   (1h poll floor, 24h cap; D8); quota parking keeps its reset-based
   schedule.
3. **Coverage** — goal-loop assessment/reflect turns, specialist agent
   turns, and queue-job execution all park + resume on
   QuotaResetError / ErrAllModelsQuotaBlocked / ThrottleBackoffError.
4. **State machine + visibility** — new agent states (or reuse of
   quota_wait semantics, leaf's choice, documented) transition through
   SafeTransition; TUI/GUI show parked count + next resume (parity).
5. **Give-up semantics (D8)** — at the horizon the turn fails with a
   clear user-facing error; the queue/goal-loop then applies ITS OWN
   retry policy (queue backoff / goal-loop next-episode rules) — no
   silent drop, no infinite park.

## Architecture

`internal/agent/parked_turn.go` generalizes quota_resume.go: the watcher
loop, MaxWait soft-stop, and resume callback are class-agnostic; the
CLASS (from tree 02) only parameterizes ResumeAt (reset time vs
BackoffPlan). Park sites: agent loop quota branch (exists), agent loop
throttle branch (new), goal-loop reflector error path (new), queue
worker execution (new). Resume re-enqueues the SAME turn payload through
its original entry path — chat via ChatHandler, goal loop via its
scheduler, jobs via the queue (requeue, not bypass).

## Interface Contracts

### Contract 1: ParkedTurnRecord + watcher (frozen; SHARED-CONVENTIONS §4.5)

```go
// internal/agent/parked_turn.go (frozen; SHARED-CONVENTIONS §4.5)
type ParkedTurnRecord struct {
    ConversationID string
    SessionID      string
    AgentID        string
    Class          llm.FailureClass // quota | throttle (server classes only)
    ResumeAt       time.Time
    Attempt        int
    MaxAttempts    int
    TurnPayload    json.RawMessage  // class-specific original request
}
// NAME GUARD: package agent already declares ParkedTurn (budget_resume.go,
// budget watcher). The generalized record is ParkedTurnRecord; the frozen
// contract in SHARED-CONVENTIONS §4.5 says the same.
type TurnParker struct{ /* generalized watcher; QuotaResumeWatcher delegates */ }
func (p *TurnParker) Park(turn ParkedTurnRecord) bool   // false = over MaxWait
func (p *TurnParker) Pending() int
func (p *TurnParker) Next(class llm.FailureClass) (time.Time, bool)
```

- Owner: 01-generalize-parker.md
- Consumers: 02-throttle-parking.md, 03-goalloop-queue-coverage.md

### Contract 2: Loop branch → park

```go
// internal/agent/loop.go throttle branch (planted by tree 02 leaf 03 —
// client-unification, whose Task 4 adds it; tree 02 leaf 04 touches only
// the resolver):
// case *llm.ThrottleBackoffError:
//     plan := llm.DefaultBackoffPlan(...)  // attempt already carried on err
//     parked := loop.parker.Park(ParkedTurnRecord{Class: llm.FailureThrottle,
//         ResumeAt: plan.NextAttempt(now, err.Attempt, err.RetryAt), ...})
//     if !parked → surface give-up error (D8)
```

- Owner: 02-throttle-parking.md
- Consumers: none (terminal behavior)

### Contract 3: Coverage parity

```go
// internal/employee/goal_loop.go: reflector.Chat error path
// internal/queue/: worker execution error path
// Both: IsQuotaResetError || ErrAllModelsQuotaBlocked → park(quota, resetAt)
//       AsThrottleBackoffError       → park(throttle, plan.NextAttempt)
// Neither path may: rotate models on ThrottleBackoffError, retry inline
// beyond existing semantics, or swallow the error when Park returns false.
```

- Owner: 03-goalloop-queue-coverage.md
- Consumers: none

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-generalize-parker.md | leaf | tree 02 leaf 01 (types only) | 65K | A |
| 02 | 02-throttle-parking.md | leaf | 01, tree 02 leaf 03 COMPLETE | 60K | B |
| 03 | 03-goalloop-queue-coverage.md | leaf | 01 | 75K | B |
| 04 | 04-states-visibility-docs.md | leaf | 01, 02, 03 | 60K | C |

**Concurrency groups:** A first. B = {02, 03} parallel AFTER 01 (02
needs tree 02 fully landed; 03 needs only 01). C last (touches surfaces
from both B leaves).

## Dispatch Protocol

- Leaf 01: verify `go test ./internal/agent/ -run 'TestQuotaResume|TestParked' -v`
  — legacy watcher tests green UNCHANGED plus new generalized tests.
  Commit: `feat(agent): generalize turn parking across failure classes (tree 03 leaf 01)`.
- Leaf 02: verify the loop parks on scripted ThrottleBackoffError and
  resumes. Commit: `feat(agent): park turns on provider throttle waits (tree 03 leaf 02)`.
- Leaf 03: verify goal-loop + queue park/resume tests; run
  `go test ./internal/employee/... ./internal/queue/... ./internal/agent/... -count=1`.
  Commit: `feat(agent): park goal-loop and queue turns on provider waits (tree 03 leaf 03)`.
- Leaf 04: verify state transitions, TUI+GUI surfaces, docs, `make graphs`.
  Commit: `feat(agent): parked-turn states, surfaces, and docs (tree 03 leaf 04)`.

In-session review per leaf before commit; max 3 re-dispatch cycles.

## Review Checklist

- [ ] Leaf tasks complete; tests pass; contracts satisfied
- [ ] Chat-quota parking byte-identical (legacy tests untouched + green)
- [ ] No parked turn holds an agent slot, goroutine, or model concurrency token while waiting
- [ ] D8 give-up: clear error surfaces; queue/goal-loop own their next step; no silent drop
- [ ] New states go through SafeTransition with a transition-table entry
- [ ] WS classification: park/resume events → agent_progress, never chat_message
- [ ] gofmt/vet/analyzers clean; no artifacts; AGENTS.md reviewed at final commit

Output: APPROVED or specific gaps.

## Coding Conventions

Pass `../SHARED-CONVENTIONS.md` §1-§3 and §5 (injected clock — park/
resume tests never sleep). Bus payloads: two-value map assertions.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-generalize-parker.md | COMPLETE | 1 | 285f67fe; dispatched by THIS orchestrator (group A); legacy quota tests untouched+green (8 before = 8 after); ParkedTurnRecord naming per §4.5; persistence memory-only (finding stated); deviations: payload superset (source_client/parked_at omitempty), wrapper preserves over-MaxWait-false, panic recovery in drainDue |
| 02-throttle-parking.md | COMPLETE | 0 | 1d94e2da (parallel session; my group-B implementer stood down on collision, then co-verified); both pass-through sites park-or-give-up; D8 ThrottleGiveUpError; safeTransition→StateQuotaWait "throttle_wait" verified through Transition table; quota tests untouched+green; attempt growth via context |
| 03-goalloop-queue-coverage.md | COMPLETE | 0 | 4739c151 (parallel session; my group-B implementer verified it); coverage matrix both sites × both classes tested; queue requeue via next_retry_at WITHOUT RetryCount consumption; interactive column explicitly preserved in requeue UPDATE; give-up → legacy paths byte-identical |
| 04-states-visibility-docs.md | COMPLETE | 0 | 3eb6a3e3; dispatched by THIS orchestrator (parallel session's leaf-04 dispatch never materialized); WaitInfo + ParkTurnEvent on existing agent.quota_wait topic; WS classification test-pinned agent_progress; TUI≡GUI labels byte-identical; flutter test 378/378; make graphs regenerated; AGENTS.md no-turn-hangs invariant same-commit |

Tree 03 COMPLETE — 4/4 leaves. Shared-checkout note: a parallel orchestrator
session on this tree committed leaves 02/03 mid-run; verified independently
against the leaf contracts by this orchestrator before acceptance. Tracking
reconciled by 04919c01 (rows 01-03) and the leaf-04 row above.

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./... && go test ./internal/agent/... ./internal/employee/... ./internal/queue/... -count=1`.
2. Cross-class E2E (scripted httptest providers): (a) quota 429 with
   2h reset on a chat turn → parks, resumes at reset; (b) bare 429 ×N on
   a specialist turn → parks on plan schedule, resumes, succeeds;
   (c) goal-loop reflect call hitting ErrAllModelsQuotaBlocked → episode
   parks, resumes; (d) queue job hitting ThrottleBackoffError past
   horizon → job fails with clear error, then queue's own retry policy
   applies (dead-letter path intact).
3. Restart resilience: daemon restart with N parked turns → records
   reloaded (existing QuotaResumeWatcher persistence semantics, if any —
   leaf 01 must state what holds; if none exists today, note the gap in
   Deviations rather than inventing persistence).
4. `make graphs` clean; analyzers clean.

## Structural Completeness Check (Before Dispatch)

`python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep 03-turn-lifecycle`

## Notes

- Tree 02 dependency: leaf 01 needs only the PolicyVerdict types (can
  start once tree 02 leaf 01 merges); leaves 02/03 need the wired
  errors — gate their dispatch on tree 02 COMPLETE.
- The documented deviation "task-checkpoint-level deferral" from the
  quota plan stays a deviation; this tree does not retro-fit it (scope
  guard — cite D9's turn-level scope if asked).
- Parked-turn persistence across daemon restarts: mirror whatever
  QuotaResumeWatcher does today. If it is memory-only, so is the
  generalization; document in leaf 01's Deviations.
