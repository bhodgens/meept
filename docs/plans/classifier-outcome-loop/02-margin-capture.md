# Margin Capture - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below using TDD. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back
> to verify — write once and stop.

## Meta

- **Parent:** docs/plans/classifier-outcome-loop/master.md
- **Scope:** Capture the Door-1 kNN margin at dispatch time — routed,
  suppressed, AND abstained — via a verdict observer on the prefilter.
- **Dependencies:** 01-persist-privacy.md (margin column exists)
- **Estimated Context:** ~50K
- **Concurrency Group:** B (parallel with 03-outcome-capture.md —
  disjoint files)

## Goal

Today Match (internal/agent/embedding_prefilter.go:306) returns nil on
abstain and the kNN vote (kNNVote at :220-225, margin computed :309) is
discarded — exactly the rows the near-miss harvest needs. This leaf
forwards every verdict (routed/suppressed/abstained) to an observer so
the dispatcher persists margin on all Door-1 activity.

## Context

The prefilter block in the dispatcher (dispatcher.go:767-798) handles
four outcomes: direct route, H6-gate suppression (:783-797), assert-only
(:769-776), abstain. Only the routed case logs margin today (:363-365)
and none persist it. The margin column now exists (leaf 01).

Key files:
- internal/agent/embedding_prefilter.go — Match (:306), kNNVote (:220)
- internal/agent/dispatcher.go — prefilter block (:767-798)
- internal/agent/allotment.go — pattern reference for new-file style

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/embedding_prefilter.go
type PrefilterVerdict struct {
    Routed         bool
    AssertedIntent string  // winning intent; "" when index empty
    Confidence     float64 // winning similarity
    Margin         float64 // kNNVote margin (top1 - top2)
    Suppressed     bool    // H6 gate or quickplan cue-guard suppression
}
func (p *EmbeddingPrefilter) SetVerdictObserver(fn func(PrefilterVerdict))
// Setter nil-guarded (project invariant). Observer invoked on EVERY
// Match return path; observer panics are NOT caught (observer is
// in-process, trusted).
```

### What This Leaf Consumes

```go
// internal/metrics/store.go (leaf 01): DispatchEntry.Margin *float64
// internal/agent/dispatcher.go prefilter block (:767-798)
```

## Tasks

### Task 1: PrefilterVerdict + SetVerdictObserver

**Objective:** struct + nil-guarded setter on EmbeddingPrefilter.

Failing test: construct an EmbeddingPrefilter (existing test helpers),
SetVerdictObserver(nil) must not panic; setting a collector func then
calling it records the verdict.

### Task 2: Match emits verdicts on every path

**Objective:** instrument all return paths of Match.

Failing tests (use existing prefilter test scaffolding — fakes with a
loaded index; grep embedding_prefilter_test.go for index fixtures):
1. Routed: Match returns an Intent AND observer got
   {Routed: true, AssertedIntent: <intent>, Confidence: >0, Margin: >=0}.
2. Abstain (below threshold): Match returns nil AND observer got
   {Routed: false, Margin: >=0} — the margin that used to be discarded.
3. Cue-guard suppression (input = a quickplan-classified text with no
   orchestration cue): Match returns nil AND observer got
   {Routed: false, Suppressed: true}.
4. Empty index: Match returns nil immediately AND observer got
   {Routed: false, AssertedIntent: ""}.
5. Nil observer: no panic, Match behaves exactly as before.

Implementation notes: Match already computes v (kNNVote) before the
return; build the Verdict at each return site. The H6 suppression and
cue guard live in the DISPATCHER (:783-797) not Match — so for those,
the observer fires from the dispatcher block with Suppressed: true (see
Task 3). Match itself only knows routed/abstain/empty.

### Task 3: Dispatcher wires observer + persists margin

**Objective:** the prefilter block registers one observer (wired where
the prefilter is constructed — SetMetricsStore precedent) and
recordDispatch receives margin.

Implementation: the dispatcher stores the last verdict per dispatch
(thread-local via the existing call flow — the prefilter block runs
synchronously before recordDispatch; store the verdict in a local
variable captured by a closure). recordDispatch sets
DispatchEntry.Margin = &margin when a verdict exists for this dispatch
(nil otherwise). The log line on the prefilter branch gains
`margin=` fields (both direct-route and abstain variants).

Test: dispatcher-level test with a wired metrics store — a Door-1
direct-route dispatch persists a non-nil margin; an abstain dispatch
persists a non-nil margin with the row's intent_type from the chain
method (llm), NOT the prefilter.

### Task 4: Log line fields

The prefilter-branch log line (the "Dispatched request (prefilter
direct route)" call at dispatcher.go:806 and the abstain debug line)
gains margin= and asserted= keys. Assert via log capture in the
existing log-test pattern if present; otherwise assert via the observer
collectors.

## Self-Verification Checklist

- [ ] All 5 verdict paths emit (routed/abstain/cue-suppressed/H6/
      empty index)
- [ ] Nil observer = zero behavior change (panic-free, same returns)
- [ ] Margin persisted for BOTH routed and abstained Door-1 matches
- [ ] No changes outside embedding_prefilter.go + dispatcher.go
      prefilter regions
- [ ] gofmt/vet clean; ASCII

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** document any.

## Review Checklist (For Review Agent)

- [ ] Observer on every Match path incl. empty index
- [ ] Nil-guarded setter (project invariant)
- [ ] Margin column populated for abstained rows (the whole point)
- [ ] Dispatcher thread-local verdict capture is race-free
      (synchronous block — verify no goroutine hop between Match and
      recordDispatch)
- [ ] Direct-mode / non-plan paths untouched

Output: APPROVED or specific gaps.

## Notes

- design.md S3(b) is authoritative: storing margin at dispatch time is
  the only complete option — abstentions discard the vote today.
- The observer is called synchronously inside Match; keep it allocation-
  light (Verdict is a value type).
- Do NOT touch the cue-guard regex or H6 gate logic — only observe.
