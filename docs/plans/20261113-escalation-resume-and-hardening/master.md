# Escalation Resume + Employee Hardening - Implementation Orchestrator (Root)

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 6 leaf documents under this node
- **Scope:** Wire approved-plan resumption into employee GoalLoops, close the migration-hint widening vector, fix the broken `meept agents amend` CLI, and pin the escalation-trigger wildcard semantics.

## Goal

Three defects and one documentation gap were found in the employee permission/escalation stack (audit 2026-11-13):

1. **Fix A (R1):** After a plan is approved (`meept plans approve` / `agents.goals.approve`), NOTHING resumes employee execution. `Manager.ApprovePlan` (internal/employee/manager.go:2312) claims "the GoalLoop driver picks it up on its next Assess" but `ApproveAndExecute` (internal/employee/goal_loop.go:2003) has exactly one production caller — the parked-episode resume (internal/employee/park.go:396-405). The `plan.approved` bus event has only two subscribers (evolver approval bridge, gate-resume pump); neither executes an employee goal loop.
2. **Fix B (R2):** The migration LLM's `tools_allowed_hint` REPLACES the base constitution allowlist (internal/employee/manager.go:2844-2858) instead of intersecting with it, violating the D4 "intersection never widens the spec" rule (internal/groups/groups.go:40). Latent widening vector.
3. **Fix C (R3):** `meept agents amend` CLI always fails: it sends `{id, field, value}` but the RPC handler requires `{id, fields, reason}` (internal/employee/handler.go:256-260). The HTTP surface is already correct.
4. **Fix D (R4):** Document + pin that `matchesTrigger` treats `match: "*"` as an EXACT action-string compare, not a wildcard (internal/employee/enforcement.go:990-1016). A secretary constitution carries `{on: action, match: "*"}`; it is a DEAD trigger today, and must stay dead-on-purpose with a test pinning it.

## Architecture

Fix A spans two packages with an import-cycle constraint: `internal/employee` may NOT import `internal/plan` (the PlanDisposer interface is satisfied by daemon adapters), and `internal/groups.resolveLayered` is unexported. All cross-package bus wiring therefore lives in `internal/daemon`. The resume flow after this tree:

```
PlanManager.ApprovePlan (internal/plan/manager.go:541)
  -> publishes "plan.approved" {plan_id, by}
  -> NEW daemon subscriber "employee-plan-resume" (leaf 02)
       resolves plan -> goal -> employee
       calls Manager.ResumeApprovedPlan (leaf 01, Contract C1)
            -> guards (busy / already-resumed / unknown owner)
            -> GoalLoop.ApproveAndExecute(ctx, planRef)
```

Leaf 01 (employee side) and leaf 02 (daemon side) are FILE-DISJOINT and parallel because Contract C1 freezes the seam. Leaf 03 serializes behind leaf 01 (both edit internal/employee/manager.go).

## Interface Contracts

### C1: Manager.ResumeApprovedPlan

```
// ResumeApprovedPlan resumes execution of an APPROVED plan on the goal loop
// of the employee that owns the plan's goal. Deliberate skips (unknown plan,
// unknown goal owner, no live GoalLoop, loop busy executing another round,
// plan already resumed by this process) return nil and log the reason at
// INFO — the bus subscriber must not error-retry skips. Real failures
// (store errors, ApproveAndExecute error) return the wrapped error.
func (m *Manager) ResumeApprovedPlan(ctx context.Context, planID, callerID string) error
// Owner: 01-employee-resume.md
// Consumers: 02-daemon-resume-wiring.md (via Contract C2)
```

Guards (all required, pinned by leaf 01 tests):
- Once-per-process per planID: a small mutex-guarded map records resumed planIDs; a second call for the same planID is an INFO skip. This coordinates with the parked-episode path (internal/employee/park.go:396-405): if the park resume already ran `ApproveAndExecute` for the plan, the map records it (add the recording call at the park resume site) and the bus subscriber skips.
- Busy guard: if the employee's GoalLoop is mid-round (invoke mutex held / DecideSkip-equivalent), skip with INFO — do NOT queue; the next scheduled Trigger picks up normal rounds.
- `ApproveAndExecute(ctx, *planRef)` is called with the EXISTING signature (goal_loop.go:2003); goal_loop.go is NOT edited by this tree.

### C2: Daemon resume subscriber

```
// New file internal/daemon/employee_resume_wiring.go:
// subscribes msgBus topic "plan.approved" under subscriber id
// "employee-plan-resume". Payload is map[string]any{"plan_id": string, "by": string}
// (published at internal/plan/manager.go:563-566 — DO NOT extend the payload;
// additive keys would be safe but this tree does not touch internal/plan).
// Resolution path: plan record lookup via the plan manager/store seam the
// daemon already holds (the plan record carries its goal linkage — created
// via CreatePlanWithGoal, internal/daemon/employee_service_adapter.go:334-353;
// verify the record struct exposes GoalID. If it does NOT, fall back to
// scanning the goal store for a goal whose ActivePlanIDs contains planID,
// internal/employee/goal.go:257/511-527). Then call deps-style
// Manager.ResumeApprovedPlan(ctx, planID, by).
// Wiring site: internal/daemon/components.go alongside the existing
// gate_park_wiring.go subscription (internal/daemon/gate_park_wiring.go:266).
// Owner: 02-daemon-resume-wiring.md
// Consumers: integration review only
```

### C3: Hint intersection semantics (D4)

```
// At internal/employee/manager.go:2844-2858, REPLACE the verbatim-replace
// block with:
//   cleaned := trim + drop empties from llmResp.ToolsAllowedHint (unchanged)
//   if len(cleaned) == 0 { keep base (empty = inherit default) }  // unchanged today
//   else if len(base.Constraints.ToolsAllowed) > 0 {
//       cleaned = intersection(cleaned, base.Constraints.ToolsAllowed), sorted+deduped
//   }
//   base.Constraints.ToolsAllowed = cleaned
// Rationale: when the base is empty (= inherit-everything universe), the
// hint IS the intersection, so replacement is correct there; when the base
// is non-empty, D4 forbids widening, so intersect. Mirror the set
// algorithm of internal/groups/resolve.go:163-171 (iterate one set keeping
// members of the other; sort.Strings result) INLINE in internal/employee —
// internal/groups.resolveLayered is unexported and internal/employee must
// not import internal/groups.
// Owner: 03-hint-intersection.md
```

### C4: CLI amend request shape

```
// cmd/meept/agents_cmd.go amend subcommand sends:
client.Call("agents.amend", map[string]any{
    "id": agentID,
    "fields": map[string]any{field: parsedValue},
    "reason": reasonFlag, // new --reason flag; omit key when empty
})
// parsedValue: json.Unmarshal the value argument first; on success send the
// decoded value (arrays/objects/numbers/bools work), else send the raw string.
// Handler contract (unchanged): internal/employee/handler.go:256-260
// amendRequest{ID string `json:"id"`; Fields map[string]any `json:"fields"`;
// Reason string `json:"reason,omitempty"`}.
// Owner: 04-amend-cli.md
```

### C5: Trigger wildcard semantics pin (documentation contract)

```
// matchesTrigger (internal/employee/enforcement.go:990-1016) compares
// EscalateOnAction via trig.Match == action — EXACT string equality, no
// wildcard expansion. The pin test asserts a trigger {on: action,
// match: "*"} does NOT fire for action "shell_execute" (or any normal
// action string). A comment at the step-3 site (enforcement.go:766-782)
// states the exact-match semantics on purpose.
// Owner: 05-trigger-wildcard-pin.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (tests) | none | 70K | gate (Phase 0) |
| 01 | 01-employee-resume.md | leaf | 00T | 60K | A |
| 02 | 02-daemon-resume-wiring.md | leaf | 00T, C1 frozen | 70K | A |
| 03 | 03-hint-intersection.md | leaf | 00T, AFTER 01 (shared manager.go) | 40K | B |
| 04 | 04-amend-cli.md | leaf | 00T | 35K | A |
| 05 | 05-trigger-wildcard-pin.md | leaf | 00T | 25K | A |

Same-letter children are file-disjoint and dispatch together. Leaf 03 is deliberately in group B: it and leaf 01 both write `internal/employee/manager.go`; serialize on purpose.

## Dispatch Protocol

For each phase, in order. Full recipe: skill `hierarchical-plan-execution`.

### Phase 0: Test Gate

Dispatch `00T-gate-tests.md` as a subagent: tests ONLY, fail RED for missing-implementation reasons, do NOT commit. Validate the returned tests in-session (never delegate review) against the Test Gate matrix below; apply the delta protocol (update the LESSER artifact). Commit the gate tests; set children to TESTS_VALIDATED. Then Phase 1 may dispatch. Recipe: `references/tests-first-gate.md`.

Repo test-placement policy (binds the gate leaf): bug-fix unit tests may extend existing unit test files (internal/employee/*_test.go, handler_test.go pins). Fix A is a new feature: its end-to-end coverage goes in `e2e/suites/escalation-resume/` + an `e2e/manifest.json` entry per docs/workflows/e2e-testing.md.

### Phase 1: Dispatch Concurrency Group A (01, 02, 04, 05)

Dispatch simultaneously via `delegate_task`; goal = "Implement all tasks from <leaf path>"; context = full leaf text + contracts C1-C5 + conventions below + relevant source INLINED; include the no-commit, no-git-add, no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (main model, never a delegated reviewer): leaf spec, contracts, Review Checklist; run the leaf's verify commands; gate-test diff must be empty. Pass: orchestrator commits with `git commit -- <paths from that leaf>` (repo convention: staged paths must equal declared scope) + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles.

### Phase 3: Dispatch Group B (03) after 01 is REVIEWED + committed

Then review/commit 03 the same way.

### Phase 4: Integration Review

After ALL children REVIEWED: run `go build ./...`, `go test -p 2 ./internal/employee/... ./internal/daemon/...`, the e2e suite `make e2e-fast-area AREA=escalation-resume` (or the manifest-declared runner), and `make graphs-check` if any bus surface changed (leaf 02 adds a subscriber — regenerate with `make graphs` and include in the 02 commit). Verify contract C1 usage end-to-end: approve a plan in a scratch daemon and observe ApproveAndExecute execution. Commit integration fixes separately.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (below)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders, commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Staged paths == leaf-declared write-targets (pre-commit scope check)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (repo module github.com/... per go.mod); gofmt before reporting.
- **Naming:** exported for new cross-package APIs (C1), unexported internals; receiver `m` for Manager methods, `l` for GoalLoop.
- **Errors:** wrap with %w; never ignore errors (pre-commit enforced); no bare panic.
- **Setters:** every Set* gets a nil guard (repo invariant).
- **Testing:** table-driven Go tests next to the package (existing *_test.go files); e2e in `e2e/suites/<name>/` with the `e2e` build tag + manifest entry.
- **Mutexes:** never hold a mutex across I/O; snapshot-under-lock, evaluate-outside pattern (PreExecChecker precedent).
- **Logging:** slog structured; INFO for deliberate skips (C1), WARN for degraded wiring.
- **Docs:** feature changes update the matching `docs/workflows/` page in the same commit; root AGENTS.md reviewed per commit (maintenance rule).
- **Do NOT commit:** implementation agents never run git commit/add. Orchestrator commits after review with explicit pathspecs.

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "continue after approval" — approved plans must resume employee execution | 01 + 02, Contract C1/C2 | e2e escalation-resume suite + unit guards |
| R2 | "#4 fix" — migration hint must not widen tool grants | 03, Contract C3 | migrate_llm unit test (RED in gate) |
| R3 | "#5 - fix properly" — agents amend CLI sends the right shape | 04, Contract C4 | handler_test.go (existing pin) + new CLI flag test |
| R4 | "#3" — document/pin '*' trigger semantics before anyone 'fixes' it into an interlock | 05, Contract C5 | wildcard-pin unit test (RED in gate) |
| R5 | Keeper behavior: parked-episode resume must stay the only OTHER path, no double-execute | 01 T3 (once-guard) | unit test double-resume skip |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests.md | TESTS_VALIDATED | 1 | Validated in-session 2026-11-13. All RED for missing-impl reasons (T0-T4 name m.ResumeApprovedPlan; T5 names the Components seam; T6 names intersectToolsAllowed; T7 names buildAmendPayload). T8 pins compiled-green in the subagent run (package-wide run blocked only by leaf-01 RED — expected). T9 compiles under -tags e2e, manifest test ok. 4 documented deviations: T6 helper-targeted pin (conservative base has no seam to seed non-empty base), T7 builder seam (no RPC-client injection in cmd/meept), T5 func-field seam (parallel-leaf rule), T4 uses real ResumeGoalEpisode path. |
| 01-employee-resume.md | PENDING | 0 | |
| 02-daemon-resume-wiring.md | PENDING | 0 | seam shape pinned by T5: Components.resumeApprovedPlan func field + start/stopEmployeeResumeSubscriber |
| 03-hint-intersection.md | PENDING | 0 | helper must be named intersectToolsAllowed per T6 pin (or adapt the 2 call sites) |
| 04-amend-cli.md | PENDING | 0 | must factor out buildAmendPayload(agentID, field, value, reason) per T7 pin |
| 05-trigger-wildcard-pin.md | PENDING | 0 | T8 pins already green |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches TESTS_VALIDATED.

## Integration Test Plan

1. `go build ./...` — clean.
2. `go test -p 2 ./internal/employee/... ./internal/daemon/...` — all GREEN including gate tests.
3. `make e2e-fast-area AREA=escalation-resume` — suite GREEN (approve → ApproveAndExecute runs once; double-approve skips).
4. Scratch-daemon manual: hire a tier-2 test employee, trigger an escalated candidate, `meept plans approve <id>`, observe the goal round execute without a parked episode (log line from leaf 02 subscriber + ApproveAndExecute telemetry).
5. `meept agents amend <id> --field=role "new role" --reason=test` — returns amendment_proposed (or direct-apply on installs without a PlanCreator); previously always errored "at least one field is required".
6. `make graphs` if bus surfaces changed (leaf 02) — subscriber appears in docs/generated/bus-topology.md; `make graphs-check` green.

## Test Gate

Test leaf: `00T-gate-tests.md` — dispatched in Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | Approved-plan resume guards (C1) | 01-employee-resume.md T1-T4 | TestManager_ResumeApprovedPlan_* (internal/employee) | e2e escalation-resume | PENDING |
| F2 | Daemon subscriber wiring (C2) | 02-daemon-resume-wiring.md T1-T2 | TestEmployeeResumeWiring_* (internal/daemon) | e2e escalation-resume | PENDING |
| F3 | Hint intersection D4 (C3) | 03-hint-intersection.md T1 | TestSynthesizeConstitution_HintIntersectsBase (migrate_llm_test.go) | n/a — bug-fix unit coverage sufficient | PENDING |
| F4 | CLI amend shape (C4) | 04-amend-cli.md T1-T2 | TestAmendCmd_BuildsFields (cmd/meept test or handler round-trip) | n/a — existing handler_test.go pin + CLI unit | PENDING |
| F5 | Trigger '*' exact-match pin (C5) | 05-trigger-wildcard-pin.md T1 | TestMatchesTrigger_ActionStarIsExact (internal/employee) | n/a — semantics pin | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: (empty at authoring)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Fix D context (why the pin, not a fix): the live secretary constitution carries `escalation_triggers: [{on: action, match: "*"}]`. Because `matchesTrigger` is exact-match, this trigger is inert. An earlier audit draft assumed it fired on every action (a livelock risk combined with an allowlist); the survey disproved that (enforcement.go:995 — `trig.Match == action`, no wildcard expansion; the only '*' special-case in the file is the tools_forbidden deny wildcard at enforcement.go:667). If a future change ever gives '*' wildcard meaning, EVERY allowlisted employee call would route to RequiresPlan — approve→execute→re-escalate livelock. The pin test makes that regression loud. Optionally narrow the secretary's '*' row in the live constitution afterwards — operator decision, NOT in this tree.
- Import-cycle constraints that shaped the tree: internal/employee cannot import internal/plan or internal/groups (resolveLayered unexported); all bus wiring lives in internal/daemon.
- plan.approved payload is map[string]any — do not extend it in this tree; leaf 02 resolves goal linkage from the plan record or goal store.
- Existing tests that pin current behavior and MUST keep passing: internal/employee/goal_loop_test.go:771-807 (ApproveAndExecute), internal/daemon/gate_park_wiring_test.go (at-most-once park resume), internal/employee/handler_test.go:31 (amend handler shape), internal/employee/migrate_llm_test.go.
