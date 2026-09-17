# Routing guards and fallback - implementation leaf

DISPATCH INSTRUCTION: Execute after leaves 01 and 02 pass review. Use tests before code. Do NOT commit or stage files.

## Meta

Parent: [master.md](master.md).
Scope: repair operation-blind guards and verify fallback taxonomy.
Dependencies: 01-privacy.md and 02-scoring.md reviewed. Concurrency group: B.
Estimated Context: 65K. Effort: 3-6 hours in serial checkpoints.
Audit references: AR-1 through AR-4 and I11.

## Goal

Let requests reach the correct decision path without phrase-specific patches. Preserve recall priority, real scheduling, media ingestion, arithmetic handling, and explicit agent overrides.

## Context

Primary file: `internal/agent/dispatcher.go`. `classifyIntent` contains short-message and media guards before classifier arbitration. Recall precedes imperative handling; the git agreement veto follows both.

Parent ClassifyAndRoute probes reproduce two errors: a diagnostic path question selects chat; a coding request containing a YouTube URL selects analyst. Parent helper probes show locative 'at' creates time evidence and 'hey,' defeats imperative recognition. The latter two still require injected-verdict integration tests.

`docs/workflows/intent-routing.md` defines review-and-correct as quickplan. Current fallback keywords can select debug from 'bugs' or platform from 'help me understand'. Verify the full fallback path before repair.

## Interface Contracts (From Parent)

Own dispatcher.go and proposed `internal/agent/dispatcher_routing_repair_test.go`. No other leaf writes dispatcher.go. Leaf 05 updates workflow documentation after code stabilizes.

Keep method recording meaningful. Preserve recall > imperative > git-veto order. Do not bypass `AgentOverrideApplied`, task creation, or planning flags. New tests must use actual `classifyIntent` or `ClassifyAndRoute`, not copies of predicates.

Use existing classifier construction seams after locating their definitions. Do not invent an IntentOther value or an unavailable mock interface. Synthetic verdict injection may use a local HTTP test server; no live model is needed for these tests.

## Tasks

### Task 1: restrict the arithmetic fast path

1. Add `TestRoutingRepairArithmetic` with the path question 'what is the bug in src/parser.go?' and an observing classifier.
2. Run `go test -p 2 ./internal/agent -run '^TestRoutingRepairArithmetic$' -count=1 -v`. Require failure because classification was skipped.
3. Recognize an arithmetic expression instead of punctuation anywhere in prose. Keep parsing bounded and avoid evaluation of arbitrary code.
4. Add arithmetic positives, source paths, hyphenated names, operator descriptions, and long prose controls.
5. Rerun the test. Require the diagnostic request to reach classification, not a hardcoded debug label.

### Task 2: distinguish media consumption from URL data

1. Add `TestRoutingRepairMediaOperation` through ClassifyAndRoute.
2. Reproduce analyst routing for a request to write a unit test using a YouTube URL.
3. Require media-consumption evidence for deterministic ingestion. Do not replace the old rule with an unconditional coding-verb override.
4. Retain summary/transcript requests and context-qualified bare IDs. Test coding fixtures, quoted URLs, and mixed requests.
5. Run `go test -p 2 ./internal/agent -run '^TestRoutingRepairMediaOperation$' -count=1 -v`.

### Task 3: repair evidence predicates without moving arbitration

1. Add `TestRoutingRepairArbitrationEvidence` with schedule/0.90 verdicts for locative file questions and file creation.
2. Add paired git/0.90 and platform/0.90 verdicts for 'hey create...' and 'hey, create...'.
3. Run the test and confirm the complete misroutes before changing helpers.
4. Require time evidence to express timing, not location. Normalize punctuation before polite-prefix comparison. Preserve real reminder, true git verb, disabled-veto, and recall controls.
5. Run `go test -p 2 ./internal/agent -run '^TestRoutingRepairArbitrationEvidence$' -count=1 -v`.

Also test 'Create a reminder component in React'. The word reminder may name an artifact rather than request scheduling. Fix the evidence class only if the injected production route reproduces the defect.

### Task 4: align degraded routing with the output-based taxonomy

1. Add `TestRoutingRepairFallbackTaxonomy` with earlier classifiers unavailable or rejected.
2. Reproduce review-and-correct selecting debug and informational 'help me understand' selecting platform.
3. Use a shared operation/evidence rule where the current paths require parity. Avoid adding individual sentence exceptions.
4. Test verdict-only review, one named defect, repo-document changes, actual platform questions, and autonomous correction requests.
5. Run the test, then `go test -p 2 ./internal/agent -run '^TestRoutingRepair' -count=1` and `go vet ./internal/agent`.

Do not change the empty-response-to-chat exception in this leaf without a product decision. Leaf 05 documents the observed exception.

## Self-Verification Checklist

- [ ] Each accepted defect fails a production-path test before repair.
- [ ] Positive controls retain arithmetic, media, recall, schedule, and git behavior.
- [ ] Agent override and planning semantics survive guard changes.
- [ ] Tests and gofmt pass; report unrelated shared-tree failures separately.
- [ ] No live model-quality claim follows from injected verdicts. Do NOT commit.

## Review Checklist (For Review Agent)

- [ ] Parent independently runs the four test groups.
- [ ] Guard changes address operation/evidence classes rather than named phrases.
- [ ] Thresholds and classifier models remain unchanged.
- [ ] Leaf 05 receives exact method names and test evidence for scratch verification.
- [ ] Report APPROVED or gaps with file and line references.
