# Capture: Emission + Trace Stamping - Branch Orchestrator

> **For the executing agent:** Orchestrator for this branch. Dispatch, review,
> re-dispatch, track. Do NOT implement code yourself. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Commits in the worktree after review, by you.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaf documents (00T-capture, 01-verdict-emission, 02-trace-stamping)
- **Scope:** Grade verdicts at the review producers, resolve the reviewed model via
  step-scoped trace ids, persist rows through the collector.

## Goal

Turn existing review results into persisted verdict rows: producers attach
`modelrating.Verdict` values + judge/reviewed identities to the enriched
`step.review_completed` payload; the collector writes them; a step-scoped trace id
makes `llm_calls` answer "which model wrote the code".

## Architecture

Two seams, one payload. Emission (01): `ReviewResult` gains `Verdicts` (additive,
omitempty) and `publishReviewEvent` (internal/agent/review_manager.go:909-946) adds
the verdict + identity keys. Collector (01): `recordReviewMetrics`
(collector.go:408-420) unmarshals the new keys and calls `Store.RecordReviewVerdict`
per verdict. Stamping (02): the step-job ctx carries `WithTraceContext(ctx,
"stp-"+stepID)` so the loop's existing stamp site (loop.go:7405-7406) puts the
step-scoped id on every implementer LLM call; the verdict producer records the same
id on the row. Judge attribution: the ReviewManager path resolves the judge agent id
(`ReviewerID`) and judge model id through the agent->model precedence chain
(`modelRefForAgent`, registry.go); unknown = "" never guessed.

## Interface Contracts

C3 and C4 frozen in ../master.md. Branch obligations:

- Payload keys (frozen): `verdicts` (array of {rubric_id, scope, category, severity,
  detail, confidence}), `reviewed_model_id`, `judge_agent_id`, `judge_model_id`,
  `orchestrator_agent_id`, `orchestrator_model_id`, `trace_id`, `cycle`, `is_final`.
- Producer-site rule: ReviewManager + review_evidence_gate.go EMIT;
  review_tools.go + pair_manager.go EXCLUDED in v1 (reason stated in docs by 04-ship).
- Both leaves' write-targets touch `internal/agent` - see file-serialization below.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | [00T-capture.md] | leaf (test gate) | branch 01 COMPLETE | 30K | A |
| 01 | [01-verdict-emission.md] | leaf | 00 TESTS_VALIDATED; 01-foundations COMPLETE | 60K | B |
| 02 | [02-trace-stamping.md] | leaf | 00 TESTS_VALIDATED; 01-foundations COMPLETE | 45K | C |

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch 00T-capture (A). Validate in-session vs the matrix; commit; TESTS_VALIDATED.

### Phase 1: Groups B then C (SERIALIZED - shared file)

01 and 02 both modify `internal/agent/review_manager.go` (01: publishReviewEvent;
02: reviewerLoop ctx) and both add tests there. This is a shared-file serialization
override: dispatch 01 (B), review, commit; THEN dispatch 02 (C), review, commit.
Never parallel.

### Phase 2: Review and Commit

Master.md Review Checklist in-session; orchestrator commits per leaf.

### Phase 3: Integration

`go build ./...`; `go test ./internal/agent/... ./internal/metrics/...` green in the
worktree; emit-and-persist round trip pinned by a gate test; COMPLETE to master.

## Review Checklist

- [ ] Payload keys byte-identical to master.md C4
- [ ] Producer-site rule: only ReviewManager + evidence gate emit; no other producer touched
- [ ] Naming contract: judge_* keys; existing reviewer identifiers untouched
- [ ] Step-scoped trace: format "stp-"+stepID; empty step => no stamp (mirror tracectx nil-guard)
- [ ] TUI/ChatHandler subscribers unaffected (additive payload only)
- [ ] Gate diffs empty; no artifacts; no line-number corruption

Output: APPROVED or specific gaps.

## Coding Conventions

Master.md conventions; Go-only leaves. No new bus topics - additive payload keys on
the existing `step.review_completed` topic; `make graphs-check` stays green.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-capture | COMPLETE | 1 | gate tests landed with their implementation leaves |
| 01-verdict-emission | COMPLETE | 1 | 334fd9c7; post-merge: EM-PASS-1 pass-row emission ported from the glm53f subagent run |
| 02-trace-stamping | COMPLETE | 1 | e57a7309 |

## Integration Test Plan

In the worktree: `go test ./internal/agent/ -run 'EM|TJ' -v` green;
`go test ./internal/metrics/ -run 'VR'` still green; end-to-end in-package test:
build a ReviewResult with verdicts, publish through a bus test double, collect, assert
one model_review_verdicts row per verdict with identities resolved.

## Test Gate

Test leaf: 00T-capture.md. Matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F3 | Emission from review producers | master.md C4 | EM-PAYLOAD-1 (keys present), EM-PRODUCER-1 (ReviewManager+gate emit; others untouched), EM-PERSIST-1 (collector -> rows) | e2e capture scenario (04-ship) | PENDING |
| F4 | Step-scoped trace join | master.md C3 | TJ-STAMP-1 ("stp-" id on implementer calls), TJ-EXCLUDE-1 (judge calls excluded by agent_id), TJ-ROTATION-1 (final successful row wins) | e2e join proof (04-ship) | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: (empty).

## Frozen Documents

Leaf documents freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- review_evidence_gate.go constructs ReviewResults at :162-221 - verdicts attach there
  with the same judge identity resolution; its three construction sites all emit.
- revision semantics C9 (issue #168): ReviewStep increments `cycle` per revision pass;
  `is_final` true on the verdicts attached to the pass whose status is terminal
  (approved final => category pass rows OR defect rows with severity, per judge output).
- The collector's bus subscription already filters `step.review_completed`
  (collector.go:260) - no subscription change.
