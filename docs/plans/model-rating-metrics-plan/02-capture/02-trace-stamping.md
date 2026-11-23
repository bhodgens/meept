# Step-Scoped Trace Stamping (Serving-Model Join) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests EXIST -
> make them pass; never weaken them. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Step-scoped trace id on implementer LLM calls + blame resolution wiring
- **Dependencies:** 00T-capture TESTS_VALIDATED; branch 01 COMPLETE; leaf 01
  COMMITTED (shared file serialization)
- **Estimated Context:** 45K
- **Concurrency Group:** C (runs AFTER 01-verdict-emission; shared review_manager.go)

## Goal

Make `llm_calls` answer "which model wrote this step's code": every implementer LLM
call for a step carries trace id `stp-<stepID>`, and the verdict row records that id
so `ResolveReviewedModel(traceID, agentID)` returns the implementer's final successful
model - excluding the judge loop's own calls by agent_id (C3).

## Context

The turn-level mint lives at handler.go:910-917 (`id.TraceID()` ->
`WithTraceContext`). Steps execute under the loop with that turn ctx
(loop.go:7405-7406 stamps it onto every call). This leaf DERIVES the step scope at
the step-job boundary: when a step job dispatches with a step id, wrap its ctx with
`WithTraceContext(ctx, "stp-"+stepID)` - the same request-scoped channel, one more
producer. The judge loop (`reviewerLoop`, review_manager.go:112) runs under its own
ctx (turn trace or none) - never the step trace - so its calls stay excluded by
agent_id even before filtering. Rotation/failover: several model_ids may appear for
(trace, agent); the blame rule picks the FINAL SUCCESSFUL row
(`ResolveReviewedModel`, branch 01) - the emitter (leaf 01) calls it when composing
the payload's `reviewed_model_id`, and stores the trace id on every row.

## Verified Anchors

- `internal/agent/handler.go:910-917` - turn trace mint seam (pattern to mirror)
- `internal/agent/loop.go:7405-7406` - stamp site (reads TraceIDFromContext)
- `internal/agent/tracectx.go:11-33` - context channel (empty => ctx unchanged)
- `internal/agent/review_manager.go:112` - reviewerLoop (its ctx must NOT carry the step trace)
- `internal/metrics/review_verdicts.go` - ResolveReviewedModel (branch 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- Step-job ctx derivation: trace id `"stp-" + stepID` stamped at the step-job
  dispatch boundary so loop.go:7405-7406 propagates it to every implementer call.
  Exact stamp site: the step-job execution entry (where the step id is bound), found
  via the existing step execution path from tactical.go / loop.go - the leaf greps
  `executeStep|step job` to pin the exact line and records it in the completion report.
- Emitter wiring: leaf 01's payload composition now calls
  `Store.ResolveReviewedModel(traceID, agentID)` for `reviewed_model_id` instead of
  guessing from metadata (falls back to "" when the trace has no rows).

### What This Leaf Consumes

- C3 blame rule + C4 payload keys (leaf 01 landed).
- tracectx channel (existing, unmodified).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Step-scoped stamp at the step-job boundary | Modify: the step-job execution entry file (grep-pinned; likely internal/agent/loop.go or tactical.go) | `go test ./internal/agent/ -run TJ-STAMP` | no step-scoped stamp | GREEN | Given a step job with step id S; When LLM calls dispatch; Then TraceIDFromContext on the call path reads "stp-S"; empty step id => ctx unchanged (mirror tracectx guard) |
| T2 | Judge loop exclusion | Modify: internal/agent/review_manager.go (reviewerLoop ctx - assert no step trace) | `go test ./internal/agent/ -run TJ-EXCLUDE` | judge ctx carries step trace | GREEN | Given the judge loop for step S; When its calls dispatch; Then their trace != "stp-S" (turn trace or "") |
| T3 | Blame wiring in emitter | Modify: internal/agent/review_manager.go (payload composition from leaf 01) | `go test ./internal/agent/ -run TJ-ROTATION` (in metrics pkg: internal/metrics/review_persist_gate_test.go) | reviewed_model_id not resolved from trace | GREEN | Given llm_calls rows [error(m1), success(m2), success(m3)] for ("stp-S", implementer); When the payload composes; Then reviewed_model_id = m3 (final successful); no rows => "" |
| T4 | Full package green | Verify only | `go test ./internal/agent/ ./internal/metrics/ -p 2` | - | all green | Given both packages; When full run; Then no regressions |

## Self-Verification Checklist

- [ ] Gate tests pass; gate files unmodified
- [ ] No change to tracectx.go or llm.WithTraceID semantics
- [ ] review_tools.go / pair_manager.go untouched
- [ ] gofmt clean; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched (+ the exact pinned stamp-site line); 2. gate + package tails;
3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Stamp is request-scoped (ctx), never a loop field
- [ ] Judge exclusion holds structurally (different ctx), not by post-filtering alone
- [ ] Blame rule: final SUCCESSFUL row; error rows never blamed
- [ ] No regressions in loop/trace tests

Output: APPROVED or specific gaps.

## Notes

Do not touch the OTLP hop-span producer (otlp_trace_producer.go) - it keys off the
same trace id and will simply see step-scoped ids for step calls; that is additive
and expected.
