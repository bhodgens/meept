# QuickPlan Session-State Upgrade — Master Plan

**Branch:** `classifier-iteration`
**Date:** 2026-09-21
**Status:** READY

## Goal

Close the quickplan-vs-code/plan/review misroute class at the layer that
holds the missing signal. The teacher-mix gate (2026-09-21, FAIL 31/48)
proved the ceiling is NOT model capability: two frontier cloud models
agreed on the wrong lane for 9 of 17 misses, concentrated on quickplan.
The distinguishing signal — an approved plan or tracked tasks exist in the
session — lives in session state, invisible to any per-message classifier
(campaign hard rule 3; teacher-mix REPORT.md confirms it for cloud models).

Design (user-ratified direction, campaign rule 5): a ONE-WAY session-
evidence upgrade at dispatch time. When the classifier verdict lands in a
boundary lane AND the session holds quickplan-shaped state, upgrade the
verdict to quickplan. Session evidence NEVER downgrades a quickplan
verdict and is NEVER a classifier feature — it gates the ROUTE DECISION
only (the cue-guard lesson: features-in-probe failed at 67.5%).

## Architecture

New file `internal/agent/session_state_gate.go` (no dispatcher.go growth;
the file is already >5000 lines):

```
// sessionEvidence returns (hasQuickplanState, reason) for a session.
func (d *Dispatcher) sessionEvidence(ctx context.Context, sessionID string) (bool, string)
```

Two evidence sources, either suffices:

1. **Plan state:** `d.planManager.GetPlansForSession(ctx, sessionID)`
   contains a plan in `StateApproved`, `StateExecuting`, or
   `StateConfirmed` (non-terminal, action-capable states —
   `internal/plan/plan.go:16-24`; `IsTerminal()` states never count).
2. **Tracked tasks:** `d.taskStore.GetTasksForSession(sessionID)`
   (NO ctx — `internal/task/store.go:580`) contains a task in
   `StatePending`, `StatePlanning`, `StateAwaitingApproval`,
   `StateExecuting`, or `StateTesting`.

Upgrade site: `ClassifyAndRoute`, immediately AFTER the LLM-verdict
finalization block (`dispatcher.go:1695`, after `Method = "llm"` is set,
BEFORE the `applyContextWeighting` return) AND after the cue-upgrade
branch. Upgrade condition — ALL must hold:

- verdict lane ∈ {code(coding), plan(planning), review, debug(debugging),
  git, analyze(analysis)} — the measured scatter set (teacher-mix misses
  + the 17/20 adjudicated scatter)
- `d.sessionEvidence(...)` returns true
- input carries lexical quickplan evidence — `QuickPlanCuePattern`
  (dispatcher.go:5138 usage) OR the action-verb imperative check already
  used by `quickPlanCueUpgradeApplies`

The upgrade records `quickplan_session_upgrade` as the classification
method, sets Confidence 0.85, AgentType orchestrator,
RequiresPlanning true. Config-gated:

```
orchestrator.classifier.session_state_upgrade (bool, default false)
```

New config struct `ClassifierConfig` in `internal/config/schema.go` near
`ClassifierPrefilterConfig` (schema.go:2462), template entry in
`config/meept.json5` beside `classifier_prefilter` (:664). Default-off =
byte-identical behavior (the C1 lesson: a dead gate ships inert).

## Interface Contracts (frozen)

C1. `sessionEvidence` signature exactly:
`func (d *Dispatcher) sessionEvidence(ctx context.Context, sessionID string) (bool, string)`
— bool = quickplan-shaped state exists; string = machine reason
(`"plan:approved"`, `"plan:executing"`, `"plan:confirmed"`,
`"task:executing"`, `"<count> active tasks"`, or `""`).

C2. Method string: `quickplan_session_upgrade` (recorded via
`d.recordClassificationMethod`, observability parity with
`quickplan_cue_upgrade` at dispatcher.go:1666).

C3. Config key path: `orchestrator.classifier.session_state_upgrade`,
type `*bool` in the struct? NO — plain `bool` default false (nil-gate
not needed; absence = false = off). Schema struct name:
`ClassifierConfig`, field `SessionStateUpgrade bool
json:"session_state_upgrade"`, nested under the existing
`OrchestratorConfig.Classifier` (add the nested struct if absent).

C4. One-way rule, test-enforced: session evidence may CHANGE a verdict
INTO quickplan, never OUT of quickplan. A test asserts a quickplan
verdict with session evidence stays quickplan.

C5. Log line on upgrade: `"LLM verdict upgraded to quickplan by session
state evidence"` with keys `verdict`, `session_reason`, `session`,
`input_len` — mirrors the cue-upgrade log shape (dispatcher.go:1661).

C6. Non-quickplan sessions with evidence but NO lexical cue are never
upgraded (the cue requirement stands — session state alone is not
enough; "fix this typo" mid-plan-session stays code).

C7. Repo layout: gate logic in `internal/agent/session_state_gate.go` +
`internal/agent/session_state_gate_test.go`. The dispatcher.go edit is
the single call-site insertion (~15 lines). Config edits in
`internal/config/schema.go` + `config/meept.json5`.

## Measurement plan (acceptance)

The replay ruler (`replay-gold.local.json5`, 48 cases, UNTRACKED) is the
acceptance instrument. Offline simulation first (leaf 03): replay the
cascade on all 48 cases WITH and WITHOUT the gate enabled in a scratch
config, using the existing scratch-daemon rig (meept-e2e-harness skill;
known traps: hujson quoted keys, `transport.http.rest` bool, dev_key from
real HOME, remap runtime ports off :8080-8084). Success bar: aggregate
replay accuracy improves by >= 2 cases (>= 86.0%) with NO regression on
non-quickplan lanes, and every upgraded case logs
`quickplan_session_upgrade`.

The 10 worker-agreed-wrong quickplan cases (teacher-mix raw) are the
expected rescue set — but the PRODUCTION cascade is the system under
test, not the cloud mixture.

## Child Index

| leaf | scope | deps | est context |
|---|---|---|---|
| 01-config-knob.md | ClassifierConfig struct + template + defaults test | none | ~30K |
| 02-session-gate.md | session_state_gate.go + ClassifyAndRoute insertion + tests | 01 | ~70K |
| 03-replay-acceptance.md | scratch-daemon A/B replay + verdict report | 02 | ~60K |

Sequential: 01 -> 02 -> 03. Leaf 02 is the only dispatcher.go editor.

## Dispatch Protocol

1. Read the child document. Dispatch via `delegate_task` single-task:
   `tasks: [{goal: <one-line imperative>, context: <full leaf brief>}]`.
2. Every dispatch context carries: leaf path, contracts C1-C7 verbatim,
   "Do NOT commit. Do NOT run git add. Write code, run tests, report
   results only. The orchestrator handles all git operations."
3. Orchestrator reviews in-session after each leaf (read the diff, run
   `go build ./internal/agent/... ./internal/config/...`, run the leaf's
   tests). Never a delegated reviewer.
4. Re-dispatch with specific feedback on gaps (max 3 rounds), then
   complete in-session if the residue is small (<= 20 lines).
5. Commit per leaf with explicit paths after review passes; update the
   tracking table after every transition.
6. Pre-dispatch drift rule: verify every file:line claim in the leaf docs
   against current source before dispatching (dispatcher.go moves).

## Review Checklist

- [ ] C1 signature exact; C2 method string recorded on every upgrade path
- [ ] C4 one-way rule has a test that would fail on a downgrade
- [ ] C6: cue requirement test — session evidence without cue does NOT
      upgrade
- [ ] Default-off proven: a test asserts the gate is inert when
      `session_state_upgrade` is false (byte-identical verdict)
- [ ] No changes to QuickPlanCuePattern, git-verb veto, or recall branch
      ordering (branch order is load-bearing, campaign rule 11)
- [ ] `go build ./...` green; `go test ./internal/agent/... ./internal/config/... -count=1` green
- [ ] No debug prints; gofmt clean; no TODOs left

## Coding Conventions

- Go stdlib style; errors wrapped with `%w` and context; two-value type
  assertions on map payloads; no mutex across I/O (GetPlansForSession is
  I/O — collect under no lock; the dispatcher owns no lock here).
- Tests: table-driven, `t.Parallel()` where no shared state; test names
  `TestSessionEvidence_*`, `TestSessionStateUpgrade_*`.
- Comments state WHY (cite campaign finding/issue) not WHAT.
- Setter/config additions follow repo AGENTS.md conventions.

## Completion Tracking Table

| leaf | status | notes |
|---|---|---|
| 01-config-knob.md | COMPLETE | 2026-09-21; commit 8edd92bf; 3 tests, template round-trip |
| 02-session-gate.md | COMPLETE | 2026-09-21; commit 68187794; 8 gate tests + insertion at ClassifyAndRoute 4.5 |
| 03-replay-acceptance.md | COMPLETE | 2026-09-21; commit 47a53756; OFF 38.30/48 vs ON 41.30/48, zero regressions; offline-simulation caveat recorded |

## Integration Test Plan

After leaf 03: full `go test -p 2 ./internal/agent/... ./internal/config/... -count=1`;
`go vet` on both packages; grep corruption check
(`grep -rcE '^\s+[0-9]+\|' internal/agent/session_state_gate*.go` = 0);
scratch-daemon replay A/B artifacts under
`tools/classifier-eval/results/session-upgrade/` (REPORT.md + summary
JSONs tracked; raw text joins gitignored). AGENTS.md updated in the final
commit if the config key or dispatcher behavior changes documented
surfaces (per repo rule).

## Open Questions

None blocking. Q1 (should the gate also fire on the heuristic/keyword
verdict paths, not just LLM?) — default NO for this tree: the LLM path
carries 37/48 of traffic share and the scatter evidence is LLM-path
specific; extending is a follow-up measurement. Q2 (upgrade to quickplan
vs directly to plan?) — quickplan per the ratified taxonomy
(execute-existing-plan = quickplan).
