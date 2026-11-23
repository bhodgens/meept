# Verdict Emission + Collector Persistence - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests
> (00T-capture) EXIST - make them pass; never weaken them. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** ReviewResult verdicts, enriched review events, collector -> store rows
- **Dependencies:** 00T-capture TESTS_VALIDATED; branch 01 COMPLETE
- **Estimated Context:** 60K
- **Concurrency Group:** B (serialized with 02-trace-stamping: shared file
  review_manager.go)

## Goal

Review producers grade with the catalog and publish verdicts; the collector persists
them with judge/reviewed identities. No trace stamping here (leaf 02 owns it).

## Context

`ReviewResult` (internal/agent/review.go:30-37) gains `Verdicts []modelrating.Verdict`
omitempty - additive, so pair_manager and review_tools compile untouched. The judge
graded the step already: ReviewManager's verdict pass derives `Verdicts` from the
review outcome (issues found => defect verdicts; approved terminal => one pass
verdict, category `pass`, severity ""). Judge identity: agent id from `ReviewerID`;
model id via the agent->model precedence chain (`modelRefForAgent`, registry.go) -
"" when unresolvable, never guessed. `cycle` = the review manager's revision count
for the step; `is_final` = true when this pass's status is terminal (approved without
another revision, or final rejection). The evidence gate sites
(review_evidence_gate.go:162-221) attach the same shape. Collector
(recordReviewMetrics, collector.go:408-420) unmarshals the new keys and calls
`RecordReviewVerdict` per verdict; rows carry `trace_id` from the payload ("" until
leaf 02 stamps it - the column exists from day one).

## Verified Anchors

- `internal/agent/review.go:30-37` - ReviewResult struct
- `internal/agent/review_manager.go:211` - ReviewStep
- `internal/agent/review_manager.go:909-946` - publishReviewEvent (both step.* and task.* messages)
- `internal/agent/review_manager.go:919` - confidence already in payload
- `internal/agent/review_evidence_gate.go:162-221` - gate ReviewResults
- `internal/metrics/collector.go:260,408-420` - subscription + unmarshal
- `internal/agent/registry.go` - modelRefForAgent precedence chain

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `ReviewResult.Verdicts []modelrating.Verdict` (omitempty)
- publishReviewEvent payload keys (frozen): `verdicts`, `reviewed_model_id`,
  `judge_agent_id`, `judge_model_id`, `orchestrator_agent_id`,
  `orchestrator_model_id`, `trace_id`, `cycle`, `is_final` - on BOTH the
  `step.review_completed` and `task.review_completed` messages (they duplicate the
  payload today; keep them identical)
- collector: new keys unmarshaled; `Store.RecordReviewVerdict` called per verdict;
  store failures log Warn and never fail the review path

### What This Leaf Consumes

- C1 Verdict/catalog, C2 RecordReviewVerdict (branch 01)
- C4 producer rule: ReviewManager + evidence gate only

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Verdicts field + emission in ReviewManager path | Modify: internal/agent/review.go; internal/agent/review_manager.go (ReviewStep outcome -> Verdicts; publishReviewEvent payload) | `go test ./internal/agent/ -run 'EM-PAYLOAD\|EM-PRODUCER'` | keys missing | GREEN | Given a reviewed step; When the event publishes; Then frozen keys present; judge model resolved via modelRefForAgent or "" |
| T2 | Evidence-gate emission | Modify: internal/agent/review_evidence_gate.go | `go test ./internal/agent/ -run EM-PRODUCER` | gate emits no verdicts | GREEN | Given a claim-without-evidence rejection; When the gate builds its ReviewResult; Then a defect verdict (category per finding) attaches |
| T3 | Collector persistence | Modify: internal/metrics/collector.go (recordReviewMetrics) | `go test ./internal/metrics/ -run EM-PERSIST` | new keys ignored | GREEN | Given enriched message; When handled; Then N rows via RecordReviewVerdict; store error => Warn, review path unaffected |
| T4 | Full package green | Verify only | `go test ./internal/agent/ ./internal/metrics/` (macOS add `-p 2`) | - | all green | Given both packages; When full run; Then no regressions (TUI/handler subscribers untouched) |

Judge prompt material: the verdict derivation prompt lives in
`internal/agent/review_manager.go` beside buildReviewPrompt - the judge returns
rubric IDs + category + severity in its structured verdict; parse leniently, unknown
rubric ID => Detail carries the raw text, RubricID "" (never drop the row).

## Self-Verification Checklist

- [ ] Gate tests pass; gate files unmodified
- [ ] Payload keys byte-identical to master.md C4
- [ ] No producer outside ReviewManager + evidence gate touched
- [ ] review_tools.go / pair_manager.go UNTOUCHED (v1 exclusion)
- [ ] gofmt clean; no artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. gate + package test tails; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] C4 verbatim; both review topics carry identical payloads
- [ ] Existing `reviewer` identifiers untouched (naming contract)
- [ ] Lenient parse keeps rows (never drops unknown-rubric verdicts)
- [ ] Store failures degrade (Warn) - no review-path failure

Output: APPROVED or specific gaps.

## Notes

`task.review_completed` mirrors the payload for TUI/ChatHandler compat
(handler.go:434, tui/sidebar.go:602) - keep the two message constructions in sync or
extract a helper; one construction site preferred.
