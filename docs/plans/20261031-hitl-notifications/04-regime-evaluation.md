# HITL Regime Evaluation Harness - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** An evaluation harness (internal/eval) that runs identical
  hermetic scenarios under three autonomy regimes and reports completion
  rate, wall-clock, interrupt count, intervention precision, and
  per-risk-class escape rate from ground-truth sources.
- **Dependencies:** none (consumes R0 methodology via dispatch context)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Audit references:** contract C7 in ../master.md

## Goal

Nobody knows whether meept's interruption regime earns its cost. This leaf
builds a harness that runs the SAME scripted scenario three ways -
(1) per-action relay ([acp] permission_mode "human"), (2) plan-approve-lead
(plan gate + gate_command), (3) autonomous with sampled post-hoc spot-check
- and produces a HitlRegimeReport comparing them on objective metrics
computed ONLY from ground truth: the auditlog hash chain, the permission
table RuleSource, and acp.permission.decision events. The report answers
one question: which regime gets work done with the fewest, most precise
interruptions and the fewest escapes?

## Context

Meept is a Go agent daemon with an existing eval framework:
- internal/eval/ - evaluation packages (grep for existing harness/benchmark
  patterns; classifier_benchmark.go is a construction precedent - note its
  deliberate lack of boot wiring).
- Ground truth sources:
  - internal/auditlog/ - hash-chained records (chain.go Record{Seq, Type,
    EmployeeID, Payload, At, PrevHash, RecordHash}); the chain is
    tamper-evident, so metrics derived from it are auditable.
  - permission table RuleSource - permission rules record their origin
    (grep RuleSource in internal/acp + internal/security for the exact
    column/type).
  - bus topic acp.permission.requested/.resolved - typed
    bus.ACPPermissionEvent (WS-visible); decision outcomes carry decision
    allow/always/deny + reason + approver.
- Regimes map to existing config:
  - per-action: [acp] permission_mode "human" (relay per action,
    internal/acp/relay.go).
  - plan-approve-lead: plan gate with gate_command
    (internal/services/plan_service.go:182; `meept plans approve`;
    cmd/meept/plans.go:341).
  - autonomous + sampled: no relay; the harness post-hoc samples N%
    of actions and simulates a spot-check verdict against the recorded
    audit trail.
- Scripted-human e2e precedent: e2e/suites/step-pipeline
  (TestNeedsInfoReviewVerdictPausesForHuman answers via RPC) - the
  pattern for a hermetic "human" that answers deterministically.
- R0 methodology (inlined in your dispatch context from research-notes.md
  section B) pins the operational definitions: intervention precision =
  interventions that changed the outcome / total interventions; escape
  rate = autonomous actions a spot-check would block / autonomous actions,
  per risk class. Ground truth for "changed the outcome": the turn/step
  state recorded in the audit chain differs between the answered and the
  counterfactual-deny run of the same scenario.

## Verified Anchors

Anchors grep-confirmed 2026-10-31. Re-grep before editing - siblings move lines.

- internal/eval/ - package root (existing harness patterns; survey first).
- internal/auditlog/chain.go:13-21 - Record struct (Seq, Type, EmployeeID,
  Payload, At, PrevHash, RecordHash).
- internal/acp/relay.go:94-107 - PermissionNotifier; relay timeout
  fail-closed (the per-action regime's interrupt source).
- internal/services/plan_service.go:180-200 - Approve flow (the
  plan-approve-lead regime's interrupt source).
- internal/employee/goal.go:208 - GateConfig (gate_command field).
- e2e/suites/step-pipeline/ - scripted-human RPC answering precedent.
- internal/config/schema.go:2662-2714 - [acp] permission_mode values.

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C7: RegimeMetrics + HitlRegimeReport structs exactly as pinned in
  ../master.md (JSON tags byte-identical), a Runner that executes one
  scenario under one regime against a hermetic daemon, a Metrics computation
  layer over the three ground-truth sources, and a report renderer writing
  JSON + a markdown summary to a caller-provided directory.
  New files: internal/eval/hitl/ (harness.go, metrics.go, report.go).

### What This Leaf Consumes

- [auditlog chain reader] - existing internal/auditlog read API (grep the
  store's list/query methods; read-only).
- [permission table RuleSource] - existing type/column (grep first; do not
  alter the permission table).
- [acp.permission events] - typed bus subscription (bus.SubscribeT on the
  acp.permission.* topics as they exist).
- [scripted-human pattern] - step-pipeline's RPC answering approach, lifted
  into the harness (not imported from e2e; reimplement the small client in
  internal/eval/hitl and say so in the report).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Scenario definition + Runner: one hermetic scenario executed under a named regime (regime = daemon config preset + scripted human behavior); per-action and plan-approve-lead regimes runnable | Create: internal/eval/hitl/harness.go; Test: internal/eval/hitl/harness_test.go | `go test -p 2 ./internal/eval/hitl/ -run TestHarness` | package absent | the runner boots a hermetic daemon (pattern: e2e harness scratch-bin rules apply to eval too - reuse the existing e2e/harness builder if importable, else daemon in-process per existing eval precedent; state your choice), runs the scenario, records the event timeline | Given scenario S and regime per-action; When Run; Then a timeline records every permission request, decision, and terminal event with timestamps |
| T2 | Autonomous regime + sampled post-hoc spot-check: run without relay; sample N% of recorded actions; simulate spot-check verdicts from the audit trail | Modify: internal/eval/hitl/harness.go; Test: internal/eval/hitl/harness_test.go (autonomous case) | `go test -p 2 ./internal/eval/hitl/ -run TestHarnessAutonomous` | autonomous regime absent | sampling is deterministic (seeded); a sampled action "blocked" in simulation leaves the metric stream marked, never rewrites history | Given 20 recorded autonomous actions and sample rate 0.2; When spot-check runs; Then exactly the seeded sample is evaluated and escape candidates are classified by risk class |
| T3 | Metrics layer: compute C7 fields from ground truth only (auditlog chain + RuleSource + acp.permission.decision events); intervention precision via counterfactual re-run (answered vs forced-deny) | Create: internal/eval/hitl/metrics.go; Test: internal/eval/hitl/metrics_test.go | `go test -p 2 ./internal/eval/hitl/ -run TestMetrics` | metrics absent | completion rate, wall-clock, interrupt count computed from the timeline; precision = changed-outcome interventions / total; escape rate per risk class; float comparisons use epsilon; report JSON matches C7 tags exactly | Given two runs of the same scenario (answered / forced-deny); When metrics computed; Then precision reflects only interventions whose recorded outcome diverged, and the JSON decodes into RegimeMetrics with identical field names |
| T4 | Report renderer: HitlRegimeReport -> JSON + markdown summary (per-regime table, one row per scenario) | Create: internal/eval/hitl/report.go; Test: internal/eval/hitl/report_test.go | `go test -p 2 ./internal/eval/hitl/ -run TestReport` | renderer absent | markdown table renders all three regimes side by side; JSON + md written to the output dir; no fabricated numbers (every cell traces to a Metrics computation) | Given three RegimeMetrics for scenario S; When Render; Then the md has one row per regime and the JSON round-trips HitlRegimeReport |

Worked example (canonical row expansion, T3):

1. Write failing test:

```go
func TestMetricsInterventionPrecision(t *testing.T) {
    m := NewMetricsTest(t) // fixture timeline: 3 interventions, 1 changed outcome
    got := m.InterventionPrecision()
    if math.Abs(got-1.0/3.0) > 1e-9 { t.Fatalf("precision = %v", got) }
}
```

2. Verify failure: `go test -p 2 ./internal/eval/hitl/ -run TestMetrics`
   Expected: FAIL - NewMetricsTest / InterventionPrecision not defined.
3. Minimal implementation: precision over the fixture timeline.
4. Verify pass: same command, PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C7 JSON tags byte-identical)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Ground-truth purity: metrics read ONLY auditlog, RuleSource,
      acp.permission.* events (grep the metrics layer for any other
      source; none may appear)
- [ ] Deterministic: seeded sampling; epsilon float compares; two runs of
      the same scenario+regime produce identical metrics
- [ ] gofmt clean; go vet clean on touched packages

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C7 structs byte-identical to master contract
- [ ] No second daemon-boot mechanism (reused existing harness/eval
      patterns; choice documented)
- [ ] Counterfactual precision method implemented as specified (no
      heuristic stand-ins)
- [ ] Risk classes derive from recorded data (no invented taxonomy beyond
      the R0-pinned classes)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep the harness out of the chat path entirely: it observes and drives
  existing surfaces, it never adds production code paths to
  internal/agent or internal/acp.
- Wall-clock metrics are inherently noisy on CI: the harness records them
  but the markdown summary should mark them as environment-relative
  (comparative across regimes in the SAME run, not absolute).
- If scripted turns need live models, STOP: this harness is hermetic only
  (scripted/recorded model behavior, like step-pipeline). Report what you
  substituted.
