# 02-goals-plans - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 00T-tests.md (test gate), 01-goal-model.md, 02-model-goal-tools.md,
  03-round-driver.md, 04-plan-mode.md (implementation leaves)
- **Scope:** Close the goals/plans model-facing gaps from the 2026-11-02
  dsh-vs-meept audit (Feature 1: durable same-session goal + automatic
  continuation via Rounds; Feature 2: plan-mode present-for-review; the
  audit's ranked items 2 and 3). Delivers fully autonomous goals (rounds cap
  + activation, model get/create/update_goal tools, zero-human-turn round
  driver) and a per-session plan-mode with model-presented approve/send-back.

## Goal

The dsh harness runs ONE durable objective per session through automatic,
zero-human-turn continuation rounds: a round driver resumes the same session
at quiescence until the goal completes or its round budget is exhausted, and
the model self-directs via `get_goal`/`create_goal`/`update_goal` tools. It
also carries a plan mode: a per-session toggle that injects a plan-policy
prompt section while allowing the model (`exit_plan_mode`) to present its
markdown plan for an inline Approve / Keep-planning decision that lands in the
durable plan store.

Meept has the engine half (a durable per-employee `Goal` with plan history,
gates, health; an approvable plan store) but NOT the autonomous continuation,
the model-facing goal tools, the round budget/activation model, or the
plan-mode session toggle. Its GoalLoop comment admits Tier 3 "would run the
full cycle without the signoff gate" (`goal_loop.go:14`). This branch delivers
the missing surface: it makes goals fully autonomous now (master D4 / R2) and
adds the plan-mode present-for-review collaboration mode.

Requirements this branch satisfies (from master's Requirement Traceability):
- **R2** "Goals: we were supposed to have implemented this fully" → 01, 02, 03
  (C4 autonomy).
- **R4** "END TO END and unit tests for all features as a prerequisite" → 00T
  test gate for every leaf below.
- **R6** "items 1-10,13,14 = moderate features" → plan-mode (04) is the item-9
  moderate feature, present-for-review surface.

## Architecture

```
model-facing surface (02-goals-plans)
   |
   +-- 01-goal-model     (goal.go + store): MaxRounds/RoundsStarted, live
   |       armed/disarmed activation, GoalBlocked state + blockedReason,
   |       IsBlockedAfterNConsecutive() default 3; CAS-ish revision column.
   +-- 02-model-goal-tools (tools/builtin): goal_get / goal_create /
   |       goal_update (actions edit|pause|resume|complete|blocked); authority
   |       gate (direct-human vs autonomous-round) mirroring dsh.
   +-- 03-round-driver   (employee + scheduler): at active+armed+idle, queue
   |       round N+1 with zero human turns via scheduler deferred-turn
   |       re-entering the GoalLoop; waits on C1 children; auto-blocks on cap.
   `-- 04-plan-mode      (session per-session bool + /plan + exit_plan_mode):
           plan-policy prompt section; inline Approve/Keep-planning review
           landing on the existing PlanManager.ApprovePlan gate.
```

01 is the data substrate; 02 and 03 both read it (02 through the goal tools,
03 through the round driver's armed/goal-phase check). 04 is orthogonal
(plan-mode is distinct from goal-mode, per master C4 "goal mode vs plan mode
are distinct"). Leaves 02, 03, 04 all gate on 01's model additions compiling;
03 additionally gates on 01-delegation reaching REVIEWED (it waits on C1
children).

## Interface Contracts

Cross-leaf contracts live here once. Leaves consume exact signatures only;
they never restate full definitions. Signature grounding is real meept source
(verified 2026-11-02); symbols marked `[NEW]` are added by this branch and
their names/shapes are the contract.

### C4a: Goal model autonomy surface (Owner: 01-goal-model.md)

```go
// internal/employee/goal.go — extends the existing Goal struct + GoalState.
// Existing (verified): type GoalState int; GoalActive|GoalPaused|GoalRetired
// (goal.go:37-47). Goal struct fields ID, EmployeeID, Title, Mandate, State,
// Source, TriggerRef, Health, LastAssessed, ActivePlanID, ActivePlanIDs,
// PlanHistory, MaxPlanHistory, CreatedAt, RetiredAt, Gate (goal.go:158-222).
type GoalState int
const (
    GoalActive GoalState = iota
    GoalPaused
    GoalRetired
    GoalBlocked   // [NEW] C4 "State adds 'blocked'" — durable blocked phase
)
// [NEW] durable + live continuation fields on Goal:
//   MaxRounds      int    `json:"max_rounds,omitempty"`   // 0 = unlimited? NO: default 256
//   RoundsStarted  int    `json:"rounds_started"`
//   BlockedReason  *BlockedReason `json:"blocked_reason,omitempty"` // {Code,Message}
//   blockedAt      time.Time // [optional] when blocked occurred (audit)
// Activation is LIVE-ONLY (never persisted): Arm()/Disarm() toggle an
// in-process atomic 'armed'/'disarmed' bool on Goal; zero value = disarmed.
// Round-cap default resolved the same way MaxPlanHistory resolves:
//   const DefaultMaxGoalRounds = 256   // dsh defaultMaxGoalRounds z.number().default(256)
func (g *Goal) Activate()                          // [NEW] arms continuation
func (g *Goal) Disarm()                            // [NEW] disarms (no continuation)
func (g *Goal) IsArmed() bool                      // [NEW] live-only read
func (g *Goal) IsBlockedAfterNConsecutive() bool   // [NEW] threshold default 3
// State-string/parse must include "blocked" (<-> GoalBlocked).
// goal.go:50-77 String()/ParseGoalState; goal.go:525-578 migrate(); scanGoal
// goal.go:865-956; Create goal.go:619; Update goal.go:714.
// Consumers: 02-model-goal-tools.md, 03-round-driver.md.
```

### C4b: Model goal tools (Owner: 02-model-goal-tools.md)

```go
// internal/tools/builtin — registers via the existing init-populated
// ToolFactories / sys.Register pattern (mirrors acp/browser tool registrars;
// tools.Tool interface at internal/tools/interface.go:24-54). Tools receive
// agent identity via the executor's details["agent_id"] injection
// (internal/agent/executor.go:1811-1816).
// Tool names (single underscore, snake_case, per tools.Tool.Name docs):
//   goal_get     — read current goal (objective, phase, roundsStarted,
//                  maxRounds, blockedReason, activation); no args.
//   goal_create  — params: objective(string, req), max_rounds(number|opt,
//                  positive int). Requires DIRECT human authority.
//   goal_update  — params: goal_id(string,req), revision(number,req),
//                  action(enum edit|pause|resume|complete|blocked),
//                  objective(string,opt), max_rounds(number,opt),
//                  blocked_reason(string,opt). edit/pause/resume require
//                  DIRECT human; complete/blocked allowed during an
//                  autonomous round; blocked rejected before
//                  IsBlockedAfterNConsecutive() threshold (default 3).
// Backing calls delegate to employee.Manager (verified):
//   CreateGoal(ctx, spec GoalSpec) (*Goal, error)   manager.go:1965
//   UpdateGoal(ctx, goal *Goal) error               manager.go:2007
//   GetGoal(ctx, id) (*Goal, error)                 manager.go:2033
//   SetGoalGate(...)                                manager.go:2042
// Human /goal command and meept agents goal-* CLI remain UNCHANGED.
// Consumers: 03-round-driver.md (uses same Manager goal paths + Activate()).
```

### C4c: Autonomous round driver (Owner: 03-round-driver.md)

```go
// internal/employee (goal_round_driver.go [NEW]) + scheduler deferred-turn.
// Reuses the EXISTING trigger path and scheduler, not a new loop:
//   employee.Manager.Trigger(ctx, id, payload)         manager.go:1482
//     -> triggerViaGoalLoop(ctx, id, payload, tier, loop)  manager.go:1540
//        -> loop.Decide(ctx, TriggerEvent{Source,Payload,FiredAt}) goal_loop.go:1376
//   scheduler.Scheduler.ScheduleConfig(JobConfig)       scheduler/scheduler.go
//   JobTypeInterval (no-op placeholder re-registered by adapter) jobs.go:140-161
//   AgentJob.Execute publishes source="scheduler" on chat.request jobs.go:212-247
// [NEW] goalRoundDriver: a per-employee goroutine/interval hook that, when
//   the goal is active+armed and the agent is idle/settled, reserves round
//   N+1 = RoundsStarted+1, persists RoundsStarted (CAS on revision), waits on
//   C1 children (01-delegation child registry; no-op when 01 not yet wired),
//   then queues round N+1 by scheduling a ONE-SHOT deferred turn that
//   re-enters triggerViaGoalLoop with payload source="goal" containing
//   round=N+1, goal_id, revision. Zero human turns. When
//   RoundsStarted >= MaxRounds, block(goal, {code:'round-limit',
//   message:'Goal reached its configured limit of N rounds.'}) and disarm.
//   Disarm on error / provider wait / max-tokens (mirror dsh driver :262-265,
//   :348-357). Round admission is validated before the loop re-enters: reject
//   a stale round whose revision/round counter no longer matches.
// Depends on: C4a (armed/maxRounds/RoundsStarted), C1 children (01 branch),
//   C2 steer/interrupt only for pause-abort. No NEW queue primitives.
// Consumers: none downstream.
```

### C4d: Plan mode (Owner: 04-plan-mode.md)

```go
// Per-session plan_mode boolean. In meept, session state persistence +
// restore-on-resume is branch 03's substrate (C5); 04 must NOT depend on it.
// [NEW] simplest durable seam that COMPILES against existing source: persist
// plan_mode as a session metadata field on the session row (session store
// already persists per-session rows; branch 03 adds an additive Version —
// 04 uses today's store, not 03's). /plan command toggles it. While active,
// inject the plan:policy prompt section (existing system-prompt section
// mechanism, internal/agent system prompt builder). exit_plan_mode tool:
//   params: plan(string, req, markdown starting with a # heading)
// presents an inline Approve / Keep-planning decision to the user (reuse the
// existing user-questions / ask channel — meept has ask tool /
// internal/tools/builtin/ask.go). On Approve, exit plan mode and land the
// plan on the EXISTING plan gate:
//   PlanManager.ApprovePlan(ctx, planID, sessionID, by)   internal/plan/manager.go:381
//   RejectPlan(ctx, planID, sessionID, by, reason)        manager.go:431
//   ConfirmPlan(ctx, planID, sessionID, by)               manager.go:509
// keep-planning returns feedback to the model (revise + present again).
// plan-mode is DISTINCT from goal-mode (master C4 consumer note).
// Consumers: none downstream.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tests.md | test gate | none (but must compile against 01 fields: authored to pin C4a/C4b/C4c/C4d) | 30K | gate |
| 01 | 01-goal-model.md | leaf | none | 35K | A |
| 02 | 02-model-goal-tools.md | leaf | 01 (compiles against C4a) | 35K | B (after 01 REVIEWED) |
| 03 | 03-round-driver.md | leaf | 01 + 01-delegation (C1 children) | 40K | C (after 01 + parent-delegation REVIEWED) |
| 04 | 04-plan-mode.md | leaf | none (uses existing plan gate + session store) | 35K | A (parallel with 01) |

Dispatch: 00T first (Phase 0). Then 01 + 04 in parallel (group A). Then 02
(group B, after 01). Then 03 (group C — its C1-child wait needs 01-delegation
REVIEWED from the parent tree; the orchestrator confirms this before dispatch;
the child-wait wiring is optional-at-compile so 03 can land without breaking
the build if 01-delegation is delayed — the leaf makes the waiter a nil-safe
no-op seam).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-tests.md first: tests only, fail RED for missing-implementation
reasons. Validate in-session (never delegate review) against the Test Gate
coverage matrix below; apply the delta protocol; commit gate tests; set children
TESTS_VALIDATED. Then implementation may dispatch. Repo e2e rule: new-feature
suite under `e2e/suites/goals-plans/` with the `e2e` build tag; manifest
path->suite entry in `e2e/manifest.json` same commit; scratch MEEPT_HOME harness
(harness.WithPreBootHook); assert through live RPC/WS surface.

### Phase 1: Dispatch Concurrency Group A (01-goal-model + 04-plan-mode)

Dispatch both in parallel via `delegate_task`: goal = "Implement all tasks
from [leaf]"; context = full leaf text + the C4a/C4d contracts + repo
conventions + relevant source INLINED + the no-commit, no-git-add,
no-read_file-on-source, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate to parent.

### Phase 3: Dispatch Concurrency Group B (02-model-goal-tools) and C (03-round-driver)

Dispatch 02 after 01 REVIEWED; 03 after BOTH 01 REVIEWED and the parent tree's
01-delegation REVIEWED (the orchestrator checks the parent's tracking table;
the C1 child-wait is a nil-safe seam so a delay does not block the build).

### Phase 4: Integration Review

After ALL children REVIEWED: verify C4a-d contracts between leaves, `go build
./...`, `make test` (short, -p 2), `make e2e-fast-area AREA=goals-plans`, check
no import cycles. Gaps: re-dispatch the failing child with contract fixes
(uncommitted). Pass: normalize formatting, run the line-number corruption
check, commit integration changes, set children COMPLETE, report COMPLETE to
parent (../../master.md).

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths) — C4a-d
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed; repo e2e policy honored)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on wiring seams, predictable-ID prevention)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing tool (goal_get/create/update, exit_plan_mode) has
      tool registration + agent wiring or an explicit documented deviation

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN)
- **Naming:** exported/unexported per repo; tool names snake_case with single
      underscore (`goal_get`, `goal_create`, `goal_update`, `exit_plan_mode`)
- **Imports:** stdlib group first, then third-party; no unused (U1000-strict)
- **Error handling:** wrap with context; no ignored errors; no bare panic(err)
- **Security:** predictable-ID prevention (pkg/id.Generate, never UnixNano);
      typed-nil guards on every Set*/wiring seam; blocked_reason sanitized
      before it enters the model-visible/stored goal value
- **Testing:** repo e2e policy governs FEATURE tests (hermetic e2e tier under
      e2e/suites/goals-plans/). Unit tests are permitted ONLY where this branch
      explicitly extends EXISTING in-package test files (goal_test.go,
      goal_loop_test.go, scheduler tests) for the additive data-structure /
      state-machine fields — no NEW standalone feature unit files. The 00T
      test leaf pins this split.
- **Formatting:** gofmt before reporting; ASCII only in new .md
- **Model-facing rule:** every capability ships a user-facing surface (CLI,
      TUI, GUI, HTTP) + agent wiring, or documents the deferral — goal tools
      ride the existing `meept agents goal-*` CLI; plan-mode gate reuses the
      `meept plans approve/reject` surface

## Requirement Traceability

| Req | Source (user words, verbatim) | Plan element (leaf/task/contract) | Verified by |
|-----|-------------------------------|-----------------------------------|-------------|
| R2 | "Goals: we were supposed to have implemented this fully" | 01-goal-model (C4a: MaxRounds/RoundsStarted/armed/blocked); 02-model-goal-tools (C4b); 03-round-driver (C4c) | 00T gate + e2e goals-plans suite |
| R4 | "END TO END and unit tests for all features as a prerequisite" | 00T-tests.md (all leaves) | branch Test Gate matrix |
| R6 | "items 1-10,13,14 = moderate features" (item 9 = plan-mode) | 04-plan-mode.md (C4d) | 00T gate + e2e goals-plans suite |
| D4 | "Goals fully autonomous now" | 03-round-driver.md (zero-human-turn continuation) | e2e autonomous-round scenario |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests | PENDING | 0 | gate — must validate before impl dispatch |
| 01-goal-model | PENDING | 0 | substrate |
| 02-model-goal-tools | PENDING | 0 | after 01 |
| 03-round-driver | PENDING | 0 | after 01 + parent 01-delegation |
| 04-plan-mode | PENDING | 0 | parallel with 01 |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T-tests reaches
TESTS_VALIDATED.

## Integration Test Plan

- `go build ./...` — the full binary compiles with all four leaves' changes.
- `make test` (short, -p 2) — existing suite stays GREEN alongside the
  extended goal/loop tests.
- `make e2e-fast-area AREA=goals-plans` — new hermetic suite GREEN.
- Cross-boundary (goal tools -> round driver): an e2e that has a goal armed,
  runs one model turn that self-completes it via `goal_update`, and asserts
  the round driver does NOT queue round N+1 after a complete (driver disarm on
  complete). A second scenario asserts a goal left active continues to round
  2 with zero human turns, then blocks at MaxRounds with blockedReason
  code=`round-limit`.
- Plan-mode -> plan gate: an e2e that enters plan mode, calls `exit_plan_mode`
  with a markdown plan, approves, and asserts the plan landed via
  PlanManager.ApprovePlan into StateApproved (or StatePlanning-then-approved)
  and plan_mode flicked off.
- `make graphs` + `make graphs-check` (new tools change surface);
  `scripts/config-drift-check.py` clean if config touched (none expected).
- Parent master.md Integration Test Plan cross-checks C4 with 01-delegation.

## Test Gate

Test leaves: [00T-tests.md] - dispatched in Phase 0, validated by the control
agent (orchestrator, in-session). The single test leaf produces BOTH the
allowed unit coverage (extensions of existing goal/loop test files) and the
hermetic e2e suite (e2e/suites/goals-plans/ + manifest entry). Repo e2e policy
note: new-feature coverage lives in the e2e suite; unit tests here only
extend existing in-package test files for additive model/state-machine fields.

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F4 | goal model: MaxRounds/RoundsStarted/armed-disarmed/blocked + IsBlockedAfterNConsecutive | 01-goal-model.md Task 1-4 | extend goal_test.go / goal_loop_test.go | segmented goals-plans e2e | PENDING |
| F4a | goal_get / goal_create / goal_update model tools + authority gate | 02-model-goal-tools.md Task 1-3 | n/a (feature) | goals-plans e2e goal-tools scenario | PENDING |
| F4b | autonomous round driver: zero-human-turn continuation, C1-child wait, auto-block on round-limit | 03-round-driver.md Task 1-4 | extend goal_loop_test.go / scheduler tests | goals-plans e2e autonomous-round scenario | PENDING |
| F5 | plan-mode: per-session boolean, /plan, exit_plan_mode present-for-review driving ApprovePlan gate | 04-plan-mode.md Task 1-5 | n/a (feature) | goals-plans e2e plan-mode scenario | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

1. 03-round-driver's C1-child wait depends on the parent tree's 01-delegation
   branch. Contract C4c makes the waiter a nil-safe seam so 03 compiles and
   lands without 01-delegation; the orchestrator must NOT dispatch 03 until
   it confirms 01-delegation REVIEWED (checked in the parent's completion
   table), or 03 ships with the waiter as a documented no-op awaiting wiring.
2. Plan-mode persistence: 04 uses today's session store (no dependency on
   branch 03 C5). If review of branch 03's versioned session log makes its
   additive seam available first, 04 MAY re-point to it as a dated addendum —
   it must never block or be blocked by branch 03.
3. `revision` CAS for goal tools: dsh uses an event-fold revision. Meept's
   GoalStore has no revision column today. 01 adds an integer revision column
   (incremented on every Create/Update) so `goal_update` carries a CAS ref.
   This is additive to the existing store, not a fold rewrite (master OQ3).

## Notes

- Source grounding (read 2026-11-02): dsh HEAD ce7073d,
  packages/goal/goal/src/types.ts (GoalPhase/GoalView/activation),
  packages/goal/goal-round-driver/src/index.ts (drive/admission fences),
  packages/goal/tool-goal/src/index.ts (tools + authority + blocked threshold),
  packages/plan/plan-mode/src/index.ts (session toggle + exit tool). Grep the
  actual dsh source before porting any threshold (defaults: maxGoalRounds=256,
  blockedAfterConsecutiveRounds=3) — the audit's file:line is the re-read
  point, not the final say.
- meept anchors (verified): internal/employee/goal.go (Goal, GoalState,
  GoalStore), internal/employee/goal_loop.go (GoalLoop.Decide, Tier-3 note at
  :14, ApproveAndExecute), internal/employee/manager.go (Trigger,
  triggerViaGoalLoop, CreateGoal/UpdateGoal/GetGoal), internal/plan/manager.go
  (ApprovePlan :381, RejectPlan :431, ConfirmPlan :509), internal/scheduler
  (ScheduleConfig, JobTypeInterval, AgentJob chat.request), internal/tools
  (Tool interface, registry, builtin init), internal/agent/executor.go
  (details["agent_id"] injection), internal/tools/builtin/ask.go (user
  question channel).
- Tier-3 autonomy already EXISTS for plan execution (decideTier3, goal_loop.go
  :1570); the round-cap + activation + zero-human-turn continuation is what
  "fully autonomous goals" adds. Do not re-build Decide; extend its trigger
  path.
