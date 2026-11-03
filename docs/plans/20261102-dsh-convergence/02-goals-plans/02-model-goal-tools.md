# 02-model-goal-tools - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate —
> the failing tests ALREADY EXIST (00T-tests.md, goals-plans e2e
> `goal_tools_test.go` G2) and are committed — your job is to make them pass;
> do NOT delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Model-facing `goal_get` / `goal_create` / `goal_update` tools over
  the employee.Manager goal store, with an authority gate (direct-human for
  create/edit/pause/resume; complete/blocked allowed during an autonomous
  round; blocked gated at the consecutive-round threshold). Human `/goal`
  command and `meept agents goal-*` CLI remain unchanged.
- **Dependencies:** 01-goal-model.md (C4a fields/methods must compile)
- **Estimated Context:** 35K
- **Concurrency Group:** B (after 01 REVIEWED)
- **Audit references:** /tmp/dsh-audit/goal-workflow-plan.md Feature 1 (dsh
  tool-goal: get_goal/create_goal/update_goal)

## Goal

Today the model cannot self-create, self-read, or self-complete a goal inside a
session — the audit shows `get_goal|create_goal|update_goal` resolve only to
CLI/constitution/GUI read paths, never a model tool. This leaf surfaces the
existing employee goal store to the model as three snake_case tools, carrying
dsh's authority model so the model is trusted to create/edit/pause/resume only
under a direct human request, but can legally mark a goal complete or blocked
during an automatic continuation round, and cannot self-block too early
(before the consecutive-round threshold, default 3).

## Context

- `internal/tools/builtin/` — tool registrars live here; the init-populated
  `ToolFactories` / `sys.Register` pattern (see `acp_register_tools.go`,
  browser/ask registrars) is the registration seam. Tools implement
  `tools.Tool` (`internal/tools/interface.go:24-54`).
- The tool needs the owning agent/employee identity. The executor injects
  `details["agent_id"]` into the permission-check context
  (`internal/agent/executor.go:1811-1816`) — but a tool must resolve the
  CURRENT agent of the calling turn. Follow the pattern of `agent_message.go`
  (an `owner func(ctx context.Context) string` sm) or read the session/agent
  from the tool context. Decide with the orchestrator's approval at review —
  do not invent a new context channel.
- Backing calls are the existing Manager methods (verified): `CreateGoal`,
  `UpdateGoal`, `GetGoal`, `SetGoalGate` (`internal/employee/manager.go:1965-2042`).
  `goal_update` self-complete/self-block mutates `State` toward
  `GoalActive->complete` semantics; note meept has no distinct `complete` state
  today — the audit maps `complete`/`blocked` onto goal phases. Decide (record
  in report) whether `complete` maps to retiring a SourceImplicit goal /
  setting a completed flag, or a new `GoalCompleted` state — prefer the least
  invasive: reuse `GoalRetired` for "done/complete" ONLY if the audit's
  `complete` maps to it, otherwise ADD `GoalCompleted` consistently with leaf
  01's additive pattern. Get orchestrator sign-off if ambiguous.
- Repo rule: NEW feature tests live in the e2e tier (00T G2 covers the tools);
  do NOT add standalone unit test files for these tools.

## Verified Anchors

- internal/tools/interface.go:24-54 — `Tool` interface (Name/Description/Parameters/Execute/IsReadOnly/IsConcurrencySafe)
- internal/tools/builtin/acp_register_tools.go — registrar pattern (init-populated)
- internal/tools/builtin/agent_message.go:71-117 — `owner func(ctx) string` + Execute pattern
- internal/employee/manager.go:1965-2006 — `CreateGoal(ctx, spec GoalSpec)`
- internal/employee/manager.go:2007-2018 — `UpdateGoal(ctx, goal *Goal) error`
- internal/employee/manager.go:2033-2042 — `GetGoal(ctx, id) (*Goal, error)`
- internal/employee/goal.go — `Goal`, `GoalState`, `GoalBlocked`, `RoundsStarted`, `IsBlockedAfterNConsecutive()` (leaf 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin — registered via the existing registrar seam:
// Tool names (Contract C4b, verbatim):
//   goal_get    — read current goal: {id, revision, objective, phase,
//                 roundsStarted, maxRounds, blockedReason?, activation}.
//   goal_create — params objective(string,req), max_rounds(number|opt,pos int)
//                 -> same goal value. Direct-human required.
//   goal_update — params goal_id(string,req), revision(number,req),
//                 action(edit|pause|resume|complete|blocked),
//                 objective|max_rounds|blocked_reason (action-scoped).
//                 edit/pause/resume: direct-human required.
//                 complete/blocked: allowed during an autonomous round;
//                 blocked BEFORE IsBlockedAfterNConsecutive() is REJECTED
//                 (error GOAL_TOOL_BLOCK_THRESHOLD); blocked requires
//                 non-empty blocked_reason.
// Authority detection: a bool `autonomousRound` flag threaded from the round
// driver (leaf 03) into the tool's exec context on the continuation turn;
// when unknown, default to requiring direct-human (fail-safe).
```

### What This Leaf Consumes

- Contract C4a (leaf 01) — `GoalState.GoalBlocked`, `RoundsStarted`,
  `MaxRounds`, `BlockedReason`, `IsBlockedAfterNConsecutive()`.
- `employee.Manager` `CreateGoal`/`UpdateGoal`/`GetGoal` (existing).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement `goal_get` tool: read the current goal and return the compact value (incl. activation) | Create: `internal/tools/builtin/goal_tools.go`; Modify: registrar (register `goal_get`) | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run GoalTools -v` (gate G2) | FAIL: tool not registered / goal-store read path absent | PASS: model turn invokes `goal_get` over RPC and sees objective/phase/rounds/activation; nil-safe `{goal:null}` when none | Given a session with no goal; When the model calls goal_get; Then result is `{goal:null}` |
| T2 | Implement `goal_create`: validate + persist via Manager.CreateGoal; require direct-human | Modify: `internal/tools/builtin/goal_tools.go` + registrar | gate G2 same | FAIL: goal_create not registered / create not wired | PASS: direct-human turn creates a goal (State active, Revision 1, armed); result echoes the goal value | Given a direct human turn; When the model calls goal_create{objective, max_rounds}; Then a durable goal is created and returned |
| T3 | Implement `goal_update` actions edit/pause/resume with the direct-human gate | Modify: `internal/tools/builtin/goal_tools.go` | gate G2 same | FAIL: action handling absent | PASS: edit changes objective and bumps revision; pause/resume transition State and arm/disarm per leaf 01 | Given a direct human turn + a goal ref; When action=edit/pause/resume; Then the goal mutates and the CAS revision gates stale updates |
| T4 | Implement `goal_update` actions complete/blocked with the autonomous-round authority + blocked threshold | Modify: `internal/tools/builtin/goal_tools.go` | gate G2 same | FAIL: complete/blocked path absent; blocked not gated | PASS: in an autonomous round complete and blocked succeed; blocked before 3 rounds errors GOAL_TOOL_BLOCK_THRESHOLD; blocked with empty reason errors; terminal write persisted | Given an autonomous continuation round + `goal_update{action:blocked, blocked_reason}`; When roundsStarted<3; Then it is rejected, and at >=3 it is accepted with the reason stored |
| T5 | Register all three tools in the registrar + ensure `IsReadOnly`/`IsConcurrencySafe` defaults correct | Modify: registrar file | `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -v` (G2 full) | FAIL: some tool still unregistered | PASS: all three callable over RPC; gate G2 GREEN | Given the daemon; When the model enumerates tools; Then goal_get/goal_create/goal_update are present |

## Self-Verification Checklist

- [ ] All tasks implemented and gate test G2 passing (RED -> GREEN)
- [ ] Interface contract C4b satisfied exactly (tool names, params, actions,
      authority, error codes)
- [ ] All files at exact specified paths; registrar updated
- [ ] Human `/goal` CLI + `meept agents goal-*` surfaces unaffected
- [ ] No scope creep — no round driver, no plan-mode here
- [ ] Deferred decision (complete-state mapping) recorded + orchestrator-approved

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path
2. **Verify command + output:** `go test -p 2 -tags e2e ./e2e/suites/goals-plans/ -run GoalTools -v` last 20 lines; `gofmt -l` clean
3. **Deviations:** none / list with rationale (esp. the complete-state mapping decision)

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate test G2 untouched (git diff empty)
- [ ] Contract C4b matches exactly; tool params/actions/authority/error codes verbatim
- [ ] Conventions: no ignored errors, snake_case tool names, typed-nil guards on the owner/manager injection, predictable-ID (none introduced)
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] Tool registration + agent wiring present (root AGENTS.md wiring requirement)

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh authority source to re-read:
  `/tmp/deepseek-harness/packages/goal/tool-goal/src/authority.ts` (requireDirectHuman,
  completionAuthority) and `tool-goal/src/index.ts:281-311` (blocked threshold,
  wrap-up injection). The meept version need not port the wrap-up user-message
  injection if the loop's evidence/review path already covers it — note the
  delta in the report.
- The `autonomousRound` flag is thread-hop wired by leaf 03; this leaf only
  READS it. Provide the exported accessor the driver will set so the two meet
  without coordination (name it in the report; the orchestrator pins it if
  03 has already authored).
