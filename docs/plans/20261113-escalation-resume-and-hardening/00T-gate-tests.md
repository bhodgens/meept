# 00T Gate Tests - Implementation Leaf (Tests Only)

> **For the implementing agent:** You are the TEST-GATE implementer.
> Write the tests below ONLY — no implementation. Every test must FAIL RED
> for a missing-implementation reason (undefined symbol, unimplemented
> branch). If a test passes immediately, re-check: it is either already
> covered (drop it and report the duplicate) or it does not test the new
> behavior. Do NOT delete, skip, or weaken any existing test. Do NOT
> commit - the orchestrator handles all git operations after review. Do
> NOT use read_file on existing source files - explore with search_files
> or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** Write ALL gate tests for features F1-F5 of this tree; RED today, GREEN after leaves 01-05 land.
- **Dependencies:** none (tests compile against current tree where possible; C1/F1 tests reference `Manager.ResumeApprovedPlan`, which does not exist yet — see T0 stub rule)
- **Estimated Context:** 70K
- **Concurrency Group:** gate (Phase 0)
- **Audit references:** 2026-11-13 audit (escalation resume gap; hint widening; amend CLI; '*' trigger semantics)

## Goal

Produce the full failing-test suite that pins this tree's contracts before any implementation dispatches: resume guards (C1), daemon subscriber wiring (C2), hint intersection (C3), CLI amend shape (C4), and trigger wildcard semantics (C5).

## Context

Repo test-placement policy (AGENTS.md e2e policy): bug-fix unit tests extend existing unit test files; NEW feature end-to-end coverage goes in `e2e/suites/<name>/` with the `e2e` build tag plus an `e2e/manifest.json` entry. Existing pins that must keep passing: internal/employee/goal_loop_test.go:771-807, internal/daemon/gate_park_wiring_test.go, internal/employee/handler_test.go:31, internal/employee/migrate_llm_test.go.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time by the tree survey -->

- internal/employee/manager.go:2312 - Manager.ApprovePlan - stamps goal.SetActivePlan(planID) at :2338; comment at :2310-2333 falsely claims GoalLoop picks up execution
- internal/employee/goal_loop.go:2003 - GoalLoop.ApproveAndExecute(ctx, planRef) (*bot.BotExecutionResult, GoalHealth, error) - exists; do NOT edit
- internal/employee/park.go:396-405 - park resume case "approve" - the only ApproveAndExecute caller today
- internal/plan/manager.go:563-566 - publishes "plan.approved" {plan_id, by}
- internal/daemon/gate_park_wiring.go:266 - existing subscriber pattern: c.msgBus.Subscribe("gate-resume-pump", "plan.approved")
- internal/employee/manager.go:2844-2858 - ToolsAllowedHint verbatim replace block
- internal/employee/manager.go:2736 - llmConstitutionResponse.ToolsAllowedHint []string `json:"tools_allowed_hint"`
- internal/employee/enforcement.go:990-1016 - matchesTrigger; EscalateOnAction is `trig.Match == action` exact compare (:995)
- internal/employee/enforcement.go:667 - the ONLY '*' special-case in the file (tools_forbidden deny wildcard)
- cmd/meept/agents_cmd.go:853-857 - amend sends {"id","field","value"} (broken shape)
- internal/employee/handler.go:256-260 - amendRequest{ID, Fields map[string]any, Reason string omitempty}
- internal/employee/handler_test.go:31 - existing pin: {"id":"x","fields":{"role":"new"}}

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Test functions (names frozen; leaves 01-05 make them GREEN):
- internal/employee/manager_resume_test.go: TestManager_ResumeApprovedPlan_Executes, _UnknownPlanSkips, _BusyLoopSkips, _DoubleResumeSkips
- internal/daemon/employee_resume_wiring_test.go: TestEmployeeResumeSubscriber_OnApproved_InvokesResume, _MalformedPayloadSkips
- internal/employee/migrate_llm_test.go (extend): TestSynthesizeConstitution_HintIntersectsBase, TestSynthesizeConstitution_EmptyHintKeepsBase, TestSynthesizeConstitution_AllUnknownHintDropsUnmatched
- cmd/meept/agents_cmd_test.go (new): TestAmendCmd_SendsFieldsShape, TestAmendCmd_ValueJSONDecoded
- internal/employee/enforcement_trigger_test.go (new): TestMatchesTrigger_ActionStarIsExactMatch, TestMatchesTrigger_ActionExactStillFires
```

### What This Leaf Consumes

- C1 ResumeApprovedPlan — `func (m *Manager) ResumeApprovedPlan(ctx context.Context, planID, callerID string) error` — owning leaf 01 (write tests against this signature; they fail RED on the undefined method)
- C2 subscriber id "employee-plan-resume" on topic "plan.approved" — owning leaf 02
- C3 intersection semantics — owning leaf 03
- C4 CLI fields shape — owning leaf 04
- C5 exact-match pin — owning leaf 05

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T0 | Compile-bridge for C1: in manager_resume_test.go, if ResumeApprovedPlan is undefined the package will not compile — instead declare the test file to reference the method via a small `var _ = (*Manager)(nil)` + method call inside test bodies; accept RED as "does not compile / undefined: m.ResumeApprovedPlan" | Test: internal/employee/manager_resume_test.go | `go vet ./internal/employee/ 2>&1 | head -5` | undefined symbol | compiles after leaf 01 | Given the method is absent; When the package builds; Then the failure names m.ResumeApprovedPlan |
| T1 | Resume executes: manager with a fake goal loop; Given an approved plan owned by a goal with a live loop; When ResumeApprovedPlan(ctx, planID, "user"); Then ApproveAndExecute ran once and nil error | Test: internal/employee/manager_resume_test.go | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_Executes` | undefined method (T0) | GREEN after 01 | Given approved plan + live loop; When resume called; Then loop executed exactly once |
| T2 | Unknown-plan skip: Given a planID with no record; When resume; Then nil returned (INFO skip), no execution | same | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_UnknownPlanSkips` | undefined | GREEN after 01 | Given unknown id; When resume; Then nil, no panic |
| T3 | Busy-loop skip: Given the loop mid-round (invoke mutex held); When resume; Then nil + no execution queued | same | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_BusyLoopSkips` | undefined | GREEN after 01 | Given busy loop; When resume; Then nil, ApproveAndExecute NOT called |
| T4 | Double-resume skip: Given resume already ran for planID; When resume again; Then nil + no second execution (also covers the park.go interplay — leaf 01 records park-resumed plans into the once-map) | same | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_DoubleResumeSkips` | undefined | GREEN after 01 | Given already-resumed plan; When resume; Then nil, execution count stays 1 |
| T5 | Subscriber wiring: daemon test bus publishing plan.approved {plan_id, by} reaches a stub resume seam exactly once; malformed payload (missing plan_id) is a logged skip, no panic | Test: internal/daemon/employee_resume_wiring_test.go | `go test -p 2 ./internal/daemon/ -run TestEmployeeResumeSubscriber` | undefined subscriber/file | GREEN after 02 | Given published event; When subscriber processes; Then ResumeApprovedPlan called once with (planID, by) |
| T6 | Hint intersection: base=[file_read], hint=[file_read, shell] -> result=[file_read] (sorted, deduped); empty hint keeps base (nil stays nil); all-unknown hint tokens vs non-empty base -> only base members survive | Test: extend internal/employee/migrate_llm_test.go | `go test -p 2 ./internal/employee/ -run TestSynthesizeConstitution_Hint` | replace-block yields [file_read, shell] today | GREEN after 03 | Given base+hint; When synthesize; Then result = base ∩ hint, sorted |
| T7 | CLI shape: amend command builds {"id", "fields": map, "reason" when set}; value that is valid JSON decodes (e.g. `["a","b"]` -> []any) | Test: cmd/meept/agents_cmd_test.go | `go test -p 2 ./cmd/meept/ -run TestAmendCmd` | command sends singular field/value today | GREEN after 04 | Given --field=role "planner"; When built; Then payload has fields.role=="planner" and no top-level "field" key |
| T8 | Wildcard pin: matchesTrigger {on:action, match:"*"} vs action "shell_execute" -> NO fire; {on:action, match:"shell_execute"} vs "shell_execute" -> fires | Test: internal/employee/enforcement_trigger_test.go | `go test -p 2 ./internal/employee/ -run TestMatchesTrigger_Action` | passes TODAY (documents current semantics — see Notes) | stays GREEN always; regression fails loudly if '*' gains wildcard meaning | Given '*' match; When action "shell_execute"; Then no trigger fire |
| T9 | E2E suite: e2e/suites/escalation-resume/ — scratch daemon, tier-2 employee, escalated candidate -> pending plan -> approve -> goal round executes exactly once; second approve event is a skip. Manifest path->suite mapping added to e2e/manifest.json | Create: e2e/suites/escalation-resume/*.go; Modify: e2e/manifest.json | `make e2e-fast-area AREA=escalation-resume` | suite missing | GREEN after 01+02 | Given approved plan on live daemon; When resume fires; Then one execution, one audit trail |

Notes for T8: this is a semantics PIN, not a bug test — it may pass immediately. That is intended; the gate value is that any future wildcard interpretation breaks it. Report it as "pin (passes today)" in the completion report; do not force it RED.

## Self-Verification Checklist

- [ ] All test files written at exact paths; T0-T8 written, T9 suite + manifest entry created
- [ ] Every RED test fails for a missing-implementation reason (or is the declared T8 pin)
- [ ] No existing test weakened, skipped, or deleted
- [ ] No implementation code added anywhere
- [ ] Conventions: table-driven where natural, no t.Parallel against shared fixtures

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [RED evidence per test; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Tests match the Task table exactly; no implementation smuggled in
- [ ] All RED for missing-implementation reasons (T8 pin exempt, declared)
- [ ] e2e suite + manifest entry follow docs/workflows/e2e-testing.md
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- T8 is deliberately a passing pin (documents C5 semantics); everything else must be RED on return.
- The gate leaf does NOT edit internal/employee/manager.go, internal/daemon wiring, or cmd/meept — those are leaves 01-04.
- If `go vet` shows OTHER pre-existing failures in the target packages, report them, do not fix.
