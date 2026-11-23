# Capture Gate Tests - Test-Gate Leaf (00T)

> **For the implementing agent:** TESTS ONLY, no implementation. RED for
> missing-implementation reasons. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Gate tests EM-* (emission + persistence) and TJ-* (trace join)
- **Dependencies:** 01-foundations COMPLETE (C1/C2 symbols exist)
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Executable acceptance for the capture seams before either implementation leaf runs.

## Context

Tests compile against C1-C4 symbols. Emission tests need a bus test double and the
collector; trace tests need an in-memory llm_calls fixture (house style:
`internal/metrics/llm_calls_test.go`).

## Verified Anchors

- `internal/agent/review_manager.go:909-946` - publishReviewEvent payload construction
- `internal/agent/review_manager.go:211` - ReviewStep entry
- `internal/agent/review_evidence_gate.go:162-221` - gate-constructed ReviewResults
- `internal/metrics/collector.go:260` - collector subscription case
- `internal/metrics/collector.go:408-420` - recordReviewMetrics unmarshal
- `internal/metrics/llm_calls_test.go` - llm_calls fixture style
- `internal/agent/tracectx.go:11-33` - WithTraceContext / TraceIDFromContext

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/agent/verdict_emission_gate_test.go` - EM-PAYLOAD-1, EM-PRODUCER-1
- `internal/agent/trace_stamping_gate_test.go` - TJ-STAMP-1, TJ-EXCLUDE-1
- `internal/metrics/review_persist_gate_test.go` - EM-PERSIST-1, TJ-ROTATION-1

### What This Leaf Consumes

- C1-C4 symbols per master.md (Verdicts field, payload keys, ResolveReviewedModel).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Emission payload gate | Create: internal/agent/verdict_emission_gate_test.go | `go test ./internal/agent/ -run EM-` | Verdicts field / payload keys missing | GREEN after 01 | Given a ReviewResult with verdicts; When publishReviewEvent runs; Then payload carries verdicts + judge/reviewed/cycle/is_final keys with exact frozen names |
| T2 | Producer-scope gate | Modify: internal/agent/verdict_emission_gate_test.go | `go test ./internal/agent/ -run EM-PRODUCER` | gate doesn't emit verdicts | GREEN after 01 | Given the evidence-gate path; When it constructs a ReviewResult; Then verdicts attach; Given review_tools.go/pair_manager.go paths; Then no verdicts (v1 exclusion pinned) |
| T3 | Persistence gate | Create: internal/metrics/review_persist_gate_test.go | `go test ./internal/metrics/ -run 'EM-PERSIST\|TJ-ROTATION'` | collector ignores new keys | GREEN after 01+02 | Given an enriched step.review_completed message; When the collector handles it; Then one model_review_verdicts row per verdict with identities |
| T4 | Trace stamping gate | Create: internal/agent/trace_stamping_gate_test.go | `go test ./internal/agent/ -run TJ-` | no step-scoped stamp | GREEN after 02 | Given a step-job ctx; When the loop dispatches LLM calls; Then opts carry trace "stp-<stepID>"; judge-loop ctx carries its own (non-implementer) identity; ResolveReviewedModel picks the final successful row |

## Self-Verification Checklist

- [ ] RED for missing symbols only; `go vet` clean
- [ ] Assertions use frozen key names verbatim
- [ ] No implementation; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. RED evidence (test output); 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Coverage matches the Test Gate matrix rows F3/F4 exactly
- [ ] No gate scope creep; frozen names asserted exactly

Output: APPROVED or specific gaps.

## Notes

EM-PERSIST-1 constructs the collector against a temp-dir store (NewCollector,
collector.go:174) and feeds a hand-built BusMessage; no daemon.
