# Leaf 02 — Session-state gate + ClassifyAndRoute insertion

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write code, run tests, report results. The
orchestrator handles all git operations.

**Parent:** `docs/plans/quickplan-session-upgrade/master.md`
**Scope:** `internal/agent/session_state_gate.go` (+ tests) and the
single call-site insertion in `ClassifyAndRoute`. This leaf is the ONLY
editor of dispatcher.go in this tree.
**Dependencies:** leaf 01 COMPLETE (config path from its report).
**Estimated context:** ~70K.

## Verified source facts (2026-09-21 — trust these; re-grep line numbers, they move)

- LLM verdict finalization: `internal/agent/dispatcher.go` — after the
  cue-upgrade `else if ShouldUseLLMResult(intent)` branch (~:1677-1695)
  sets `intent.Method = "llm"` then returns via
  `d.applyContextWeighting(intent, memCtx, input)`. THE insertion point
  is immediately before that return, inside the same branch. The
  cue-upgrade branch itself (~:1646-1675) returns EARLIER with its own
  upgraded intent — do not duplicate the gate there (the cue upgrade
  already required lexical evidence; re-checking session state there is
  optional and only adds latency — skip it, note the exclusion).
- `d.planManager.GetPlansForSession(ctx, sessionID) ([]*Plan, error)` —
  internal/plan/manager.go:76.
- Plan states: `plan.StateApproved`, `plan.StateExecuting`,
  `plan.StateConfirmed`; `Plan.IsTerminal()` covers
  confirmed/cancelled/failed (internal/plan/plan.go:16-27).
- `d.taskStore.GetTasksForSession(sessionID) ([]*Task, error)` — NO ctx
  (internal/task/store.go:580). Active task states:
  `task.StatePending, StatePlanning, StateAwaitingApproval, StateExecuting,
  StateTesting` (internal/task/task.go:18-22).
- `d.recordClassificationMethod(...)` pattern: dispatcher.go:1666.
- `QuickPlanCuePattern.MatchString(lower)` usage: dispatcher.go:5138
  (inside `quickPlanCueUpgradeApplies`); note it lowercases input first.
- The dispatcher holds `planManager *plan.PlanManager` (:328) and
  `taskStore *task.Store` (:311); both may be nil — nil checks REQUIRED
  (nil = no evidence, never a crash).

## Tasks

### Task 1 (RED) — gate tests

Create `internal/agent/session_state_gate_test.go`. Tests must run
against the REAL dispatcher construction path used by existing tests
(read `internal/agent/dispatcher_test.go` helpers first — reuse its
fixture builders; do not invent a second construction path).

1. `TestSessionEvidence_PlanApproved` — session with an approved plan ->
   `(true, "plan:approved")`.
2. `TestSessionEvidence_ActiveTask` — session with an executing task ->
   true, reason non-empty.
3. `TestSessionEvidence_Empty` — no plans, no tasks -> `(false, "")`.
4. `TestSessionEvidence_TerminalPlanIgnored` — a COMPLETED plan does not
   count (one-way rule context: done plans are not execution evidence).
5. `TestSessionStateUpgrade_Gates` — table-driven over the upgrade
   predicate:
   - code verdict + evidence + cue input + knob ON -> upgraded
     (quickplan, 0.85, orchestrator, RequiresPlanning,
     method `quickplan_session_upgrade`)
   - knob OFF -> byte-identical verdict (same Type/Confidence/AgentType/
     Method — the default-off inertness proof)
   - evidence + cue but verdict=chat -> NOT upgraded (chat is the
     conversational catch-all, not in the boundary set)
   - evidence but NO cue ("fix this typo" + approved plan) -> NOT
     upgraded (C6)
   - cue but NO evidence -> NOT upgraded
   - quickplan verdict + evidence -> STAYS quickplan (C4 one-way; this
     is the no-downgrade pin)
6. Every upgrade asserts a `quickplan_session_upgrade` method
   record (C2) and the log line fires (if the fixture captures logs).

### Task 2 (GREEN) — session_state_gate.go

```go
// sessionEvidence reports whether the session holds quickplan-shaped
// state: an approved/executing/confirmed plan or an active tracked task.
// Evidence sources: planManager.GetPlansForSession (ctx),
// taskStore.GetTasksForSession (no ctx). Either source suffices.
// C1 signature — do not change.
func (d *Dispatcher) sessionEvidence(ctx context.Context, sessionID string) (bool, string)
```

Plus an unexported predicate the tests can drive without a full dispatch:

```go
// sessionStateUpgradeApplies reports whether an LLM verdict should be
// upgraded to quickplan given session evidence. All three conditions:
// boundary lane, lexical cue, knob enabled. One-way (C4): quickplan
// verdicts return false (nothing to upgrade; never downgrade).
func sessionStateUpgradeApplies(verdictType, input string, hasEvidence bool, knob bool) bool
```

Boundary lanes (C1 scatter set): `code`, `plan`, `review`, `debug`,
`git`, `analyze` AND their lane spellings if the LLM classifier emits
`coding`/`debugging`/`analysis` — grep `IntentCode`/`IntentPlan`/
`IntentReview`/`IntentDebug`/`IntentGit`/`IntentAnalyze` constants and
use the typed constants, not string literals, wherever they exist.
Cue check: reuse `QuickPlanCuePattern` (lowercase the input first, same
as dispatcher.go:5138) OR `quickPlanCueUpgradeApplies`'s strong-form
check — call the existing function rather than duplicating its regex
logic.

### Task 3 — ClassifyAndRoute insertion

Inside the `else if ShouldUseLLMResult(intent)` branch
(~dispatcher.go:1691), before the `applyContextWeighting` return:

```go
if d.config Classifier knob (per leaf 01's path) &&
    sessionStateUpgradeApplies(intent.Type, input, hasEv, true) {
    // log per C5, record method per C2, rebuild intent as quickplan
    // (mirror the cue-upgrade block at ~:1666-1675 field-for-field,
    // Method: "quickplan_session_upgrade")
}
```

Compute `hasEv` via `d.sessionEvidence(ctx, conversationID)` ONLY when
the knob is on AND the lane is in the boundary set (latency: two DB
reads; do not pay them on every dispatch). Note the session variable at
this scope is `conversationID` (thread-routed, dispatcher.go:895).

Keep the insertion <= ~25 lines; the logic lives in
session_state_gate.go. If the branch's return shape makes this
awkward, extract a small helper `maybeUpgradeSessionQuickplan(d, intent,
input, conversationID) *Intent` in session_state_gate.go and call it.

### Task 4 — wiring check + full suite

- `go build ./...` green.
- `go test ./internal/agent/... ./internal/config/... -count=1` green
  (use `-p 2` if running the wider suite).
- `gofmt -l internal/agent/ internal/config/` empty.
- Confirm NO changes to: QuickPlanCuePattern definition, git-verb veto,
  recall branch order, cue-upgrade block. `git diff --stat` must show
  exactly: session_state_gate.go (new), session_state_gate_test.go
  (new), dispatcher.go (~25 lines), plus leaf 01's config files if
  uncommitted.

## Interface Contract (what this leaf exposes)

- `sessionEvidence` (C1 signature) and `sessionStateUpgradeApplies` in
  package agent.
- Method string `quickplan_session_upgrade` in dispatch audit rows.
- Leaf 03 consumes: the knob name, the method string, and the log line
  text for its replay A/B greps.

## Self-Verification Checklist

- [ ] All 6 RED tests captured BEFORE the implementation (paste outputs)
- [ ] GREEN on the full agent+config suites
- [ ] Default-off inertness test present and passing
- [ ] One-way (no-downgrade) test present and passing
- [ ] Cue-required test present and passing
- [ ] git diff --stat matches the declared file list exactly
- [ ] No mutex held across GetPlansForSession/GetTasksForSession calls

## Review Checklist (orchestrator)

- [ ] C1 signature verbatim; C2/C5 strings verbatim
- [ ] Insertion inside the LLM branch only; cue-upgrade branch untouched
- [ ] Boundary set uses typed constants; both evidence sources nil-guarded
- [ ] No latency regression on non-LLM paths (evidence reads only behind
      knob+lane)
- [ ] Existing classifier tests unmodified (no test reordering)

Suggested commit (orchestrator):
`git commit -m "feat(agent): one-way quickplan session-state upgrade at dispatch (default off)" -- internal/agent/session_state_gate.go internal/agent/session_state_gate_test.go internal/agent/dispatcher.go`
