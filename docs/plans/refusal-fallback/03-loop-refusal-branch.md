# Agent Loop Refusal Branch - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** One-hop refusal fallback in the agent loop error path.
- **Dependencies:** 01-llm-refusal-error.md (RefusalError), 02-refusal-model-slot.md (slot)
- **Estimated Context:** 70K
- **Concurrency Group:** B (dispatch after A is REVIEWED; before 04)

## Goal

When the serving model returns *llm.RefusalError, the loop re-dispatches the
same turn to the configured refusal model exactly once, publishing a bus
event. If the slot is empty (feature off) or the fallback itself refuses,
the original error surfaces unchanged. A refusal NEVER reaches
Resolver.RecordAliasFailure — the model is healthy; it declined.

## Context

The loop's error path is a chain of typed-error branches in
`internal/agent/loop.go` (around line 5752-5880): quota branch (errors.As
*llm.QuotaResetError -> track, block, rotate), then the generic branch
(RecordAliasFailure + RotateToNextModel). The refusal branch slots BETWEEN
them: after quota, before generic, so the generic branch never sees a
refusal.

The single-retry mechanism: the loop already re-runs its LLM call in a
loop (`attempt` variable, backoff budget). The refusal branch does NOT
rotate the alias — it pins the refusal model for the retry via the same
request-scoped override seam the verification-escalation path uses
(`SetPersistentModelOverride` / `llm.WithResolvedModel`; see
internal/agent/verification_escalation.go:49-51 SetOverrideApplier wiring
and internal/agent/loop.go:2125 for the existing escalation-event
publisher wiring).

Key files to understand before implementing:
- internal/agent/loop.go:5752-5830 - quota branch (mirror structure)
- internal/agent/loop.go:5825-5880 - generic branch (refusal must precede)
- internal/agent/verification_escalation.go - DecideEscalation +
  EventPublisher + SetOverrideApplier seams
- internal/llm/errors_refusal.go - the error type (from leaf 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/loop_refusal.go (new)
package agent

// refusalFallbackRef resolves the configured fallback: ModelsConfig slot
// value (provider/id or bare alias name). Resolution uses
// Resolver.ResolveEscalationRef (existing seam) so bare alias names work.
// Returns "" when unconfigured/unresolvable.
func (l *AgentLoop) refusalFallbackRef() string

// handleRefusal implements the one-hop policy. Called from the loop's
// error path when errors.As(err, &refusalErr) matches.
//   already on the fallback model  => return err unchanged (give up)
//   slot empty / unresolvable      => return err unchanged (feature off)
//   otherwise                      => pin override to the fallback ref,
//                                     publish agent.model_escalated with
//                                     reason "refusal_fallback", return
//                                     retry=true (loop retries with the
//                                     pinned override; the pin clears
//                                     after the turn like the escalation
//                                     override does).
func (l *AgentLoop) handleRefusal(err *llm.RefusalError) (retry bool, err error)
```

Hook site (modify internal/agent/loop.go, one errors.As block between the
quota branch and the generic RecordAliasFailure branch):

```go
var refusalErr *llm.RefusalError
if errors.As(err, &refusalErr) {
    retry, err := l.handleRefusal(refusalErr)
    if retry {
        continue // existing retry loop; override is pinned
    }
    return nil, err
}
```

### What This Leaf Consumes

```go
// From 01: llm.RefusalError, errors.As-able, NonRetryable.
// From 02: ModelsConfig.RefusalModel slot (reached via the loop's config
// accessor the same way verification escalation reaches its settings).
// Existing seams: Resolver.ResolveEscalationRef, SetPersistentModelOverride,
// EventPublisher closure pattern (loop.go:2125).
```

## Tasks

### Task 1: handleRefusal decision logic (pure, table-tested)

**Objective:** The decision function with all four exits, unit-tested
without a live loop where possible (mirror how verification_escalation.go
tests DecideEscalation with a fake resolver).

**Files:**
- Create: `internal/agent/loop_refusal.go`
- Test: `internal/agent/loop_refusal_test.go`

**Step 1: Write failing test**

Table cases:
1. slot empty => (false, original err), no event published.
2. slot unresolvable (fake resolver errors) => (false, original err).
3. slot resolves, current model != fallback => (true, nil), event published
   with keys {agent_id, from_model, to_model, reason: "refusal_fallback"}.
4. current model == fallback model => (false, original err) — one-hop rule.

Fake resolver: implement the ModelResolver interface the same way
verification_escalation_test.go does (find it via search_files for
"ResolveEscalationRef" in internal/agent/*_test.go).

**Step 2: Run to verify failure.**

**Step 3: Implement.**

**Step 4: Run to verify pass** — `go test -p 2 ./internal/agent/ -run Refusal -v`

### Task 2: Loop hook site + override pinning

**Objective:** Wire handleRefusal into the error-path chain between quota
and generic branches; retry with the pinned override; clear after turn.

**Files:**
- Modify: `internal/agent/loop.go` (the errors.As block per contract;
  override pinning via the loop's existing persistent-override machinery,
  mirroring SetOverrideApplier usage at loop.go:2125)
- Test: `internal/agent/loop_refusal_test.go` (append)

**Step 1: Write failing test** — drive the loop's error-path function with
a fake LLM client that refuses on call 1 and succeeds on call 2 with a
different model asserted via the request's resolved-model capture.
(Simplify if the loop surface resists faking: test the branch ordering by
asserting RecordAliasFailure is NOT called on a fake resolver when the
error is a RefusalError — that assertion is the invariant that matters.)

**Step 2: Run to verify failure.**

**Step 3: Implement** the hook + pin + clear-after-turn.

**Step 4: Run to verify pass** — `go test -p 2 ./internal/agent/ -short`

### Task 3: Invariant guard test

**Objective:** Pin the two invariants as tests so future edits cannot
silently break them.

**Files:**
- Test: `internal/agent/loop_refusal_test.go` (append)

**Step 1: Write tests**

1. A RefusalError on the error path never calls
   resolver.RecordAliasFailure (fake resolver records calls).
2. A refusal while the fallback ALSO refuses returns the RefusalError and
   makes exactly 2 model calls (no third attempt, no rotation).

**Step 2: Run to verify.** (These may pass against Task 1+2 code; if so
they are regression pins — keep them. If they fail, fix Tasks 1-2.)

**Step 3: Run full package** — `go test -p 2 ./internal/agent/ -short`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] The refusal branch sits BETWEEN the quota branch and the generic
      RecordAliasFailure branch in loop.go (verify by reading the function)
- [ ] No RecordAliasFailure / BlockQuota / RotateToNextModel call anywhere
      in the refusal path
- [ ] Event payload keys exactly {agent_id, from_model, to_model, reason,
      fix_loops} with reason "refusal_fallback", fix_loops 0
- [ ] gofmt clean; `go vet ./internal/agent/` passes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Branch ordering verified in-loop.go source (quota -> refusal -> generic)
- [ ] One-hop rule enforced (fallback refusing surfaces, no loop)
- [ ] Override clears after the turn (no sticky fallback)
- [ ] Event published on the fallback path only (not on give-up paths)
- [ ] Alias health untouched
- [ ] No debug artifacts, no TODOs
- [ ] Tests cover all four decision exits + the two invariants

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The retry loop's backoff budget applies to the refusal retry too — keep
  using llmBackoff.NextDelay() on the continue if the surrounding loop
  expects it; do NOT add a new budget.
- If the loop structure makes "pin the override for one retry" cleaner via
  llm.WithResolvedModel on the next call than via persistent override,
  prefer the request-scoped option (AGENTS.md: selection is request-scoped;
  never mutate shared manager state).
- Streaming chat turns: the refusal arrives at stream end; the retry is a
  full re-dispatch. Acceptable for phase 1 (the leaf-05 e2e documents it).
