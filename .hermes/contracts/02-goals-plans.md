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

