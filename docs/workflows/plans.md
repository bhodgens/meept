# Plans

## Overview

The plan system enables structured, multi-phase execution of complex tasks. Plans are project-scoped entities with their own lifecycle, stored as plan.md files on disk with SQLite metadata tracking. They synthesize into the existing task system on approval and track progress through bus events.

## Plan Creation Triggers

Plans can be triggered automatically based on configuration or created on demand:

| Mode | Behavior |
|------|----------|
| `threshold` (default) | Planner decides based on complexity (step count, keywords, intent type) |
| `always` | Every non-trivial request generates a plan |
| `off` | Plans only created via explicit `/plan` command |

### Threshold Logic

When mode is `threshold`, a plan is triggered when any of these conditions are met:

- **min_steps >= 3**: LLM decomposition produces 3+ steps
- **complexity_keywords**: request matches keywords like "refactor", "migrate", "implement", "redesign"
- **always_plan_intents**: intent types like "plan", "implement", "build" always trigger

### On-Demand Creation

Use `/plan <description>` in chat to force plan creation regardless of mode setting.

## Approval Workflow

Plans go through a review-and-approval workflow before execution begins:

1. **Plan generated** - state: `draft`
2. **Plan submitted for review** - state: `pending_approval`
3. **Inline chat notification** appears with plan summary and link to Plans tab
4. **User reviews** in Plans tab:
   - `[a] approve` - plan approved, synthesis begins
   - `[r] reject` - plan rejected with optional feedback
   - `[v] revise` - plan revised and re-submitted (up to `max_revisions` rounds)
5. **On approval**: `PlanManager.Synthesize()` creates task hierarchy
6. **On all steps complete**: state transitions to `completed`
7. **User confirms sign-off**: state transitions to `confirmed`

### Approval Configuration

```json5
{
  plans: {
    approval: {
      require_approval: true,
      auto_approve_simple: false,
      allow_revision: true,
      max_revisions: 3,
    },
  },
}
```

## Execution and Progress Tracking

### Synthesis

When a plan is approved, `PlanManager.Synthesize()` creates the task hierarchy:

```
plan.approved bus event
  -> PlanManager.Synthesize()
    -> Create parent Task linked to Plan.TaskID
    -> For each PlanPhase:
        -> Create child Task with parent_id
        -> Parse steps from plan.md section
        -> Create TaskSteps with dependency chains
    -> Publish task.planned
    -> TacticalScheduler picks up as usual
```

### Progress Flowback

Task completion events flow back to update plan progress:

```
task.step.completed -> PlanManager -> updates PlanPhase.CompletedSteps
task.completed (child) -> PlanManager -> updates PlanPhase.State
task.completed (parent) -> PlanManager -> updates Plan.State to "completed"
```

The plan system does not re-implement execution. It coordinates via the existing task system.

## Confirmation / Sign-Off

After all phases complete, the plan enters `completed` state and awaits human sign-off. A user with appropriate permissions confirms the plan, transitioning it to `confirmed`.

Confirmation configuration:

```json5
{
  plans: {
    confirmation: {
      require_signoff: true,
      auto_confirm_phases: false,
    },
  },
}
```

## TUI Views

### Header Badges

Below the session name, plan badges appear color-coded by state:

```
session-name | Description text                                              [project branch*]
  plans: 1 confirmed  1 executing (4/8 steps)  1 pending approval
[0] chat  [1] tasks  [2] plans  [3] queue  [4] memory
```

### Session Picker - Plan Indicators

Each session shows plan count with state-colored indicators:

```
> auth-overhaul     * 2 plans: 1 exec 1 pending        5m ago
  bugfix-session    * 1 plan: confirmed                 2h ago
  quick-chat        no plans                             1d ago
  refactor-api      * 3 plans: 1 done 1 failed 1 exec   3d ago
```

### Plans Tab

The plans tab provides a full plan list with filtering, approval actions, and detail navigation:

```
 plans for: auth-overhaul                              filter: [all] active completed pending

 * Add OAuth2 Token Refresh                         executing
   plan-a1b2 . docs/plans/oauth2-refresh.md . my-app
   Phase 1: Design     3/3 confirmed
   Phase 2: Impl       2/4 executing
   Phase 3: Testing    0/3 pending
    4/8 steps . 1.2k tokens . agent: coder

 * Rate Limit Middleware                            pending approval
   plan-b2c3 . docs/plans/rate-limit.md . my-app
   3 phases . 11 steps . awaiting review
    [a] approve  [r] reject  [v] revise  [enter] view plan.md

[/] filter . [enter] detail . [e] edit plan.md . [a] approve . [c] confirm . [n] new plan
```

### State Color Coding

| State | Color |
|-------|-------|
| `planning` | blue |
| `draft` | gray |
| `pending_approval` | blue |
| `approved` | green |
| `executing` | amber/yellow |
| `completed` | green |
| `confirmed` | green, bold |
| `failed` | red |
| `cancelled` | gray, hollow |

### Chat Inline Notification

When a plan enters `pending_approval`, an inline message appears in chat:

```
+ plan ready for review ----------------------------------------+
| Plan: "Add OAuth2 Token Refresh"
| 3 phases . 8 steps . threshold: complex
| [2] plans tab to review  .  /approve plan-a1b2
+--------------------------------------------------------------+
```

## CLI Commands

### `meept plans list` - List Plans

List all plans, optionally filtered by state or project.

```bash
# List all plans
meept plans list

# Filter by state
meept plans list --state pending_approval

# Filter by project
meept plans list --project my-app
```

### `meept plans show` - Show Plan Details

Display full plan details including phases, steps, and progress.

```bash
meept plans show plan-a1b2c3d4
meept plans show plan-a1b2c3d4 --verbose
```

### `meept plans approve` - Approve a Plan

Approve a plan that is in `pending_approval` state. This triggers synthesis into tasks.

```bash
meept plans approve plan-a1b2c3d4
meept plans approve plan-a1b2c3d4 --comment "Looks good, proceed"
```

### `meept plans reject` - Reject a Plan

Reject a plan with optional feedback.

```bash
meept plans reject plan-a1b2c3d4
meept plans reject plan-a1b2c3d4 --comment "Needs more detail on phase 2"
```

### `meept plans confirm` - Confirm Plan Completion

Confirm sign-off on a completed plan.

```bash
meept plans confirm plan-a1b2c3d4
meept plans confirm plan-a1b2c3d4 --comment "All deliverables verified"
```

## HTTP API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/plans` | List plans (query params: `state`, `project_id`, `session_id`) |
| `GET` | `/api/v1/plans/:id` | Get plan details with phases and progress |
| `POST` | `/api/v1/plans` | Create a new plan |
| `POST` | `/api/v1/plans/:id/approve` | Approve a pending plan |
| `POST` | `/api/v1/plans/:id/reject` | Reject a pending plan |
| `POST` | `/api/v1/plans/:id/confirm` | Confirm sign-off on a completed plan |

### Example: List Plans

```bash
curl http://localhost:8081/api/v1/plans?state=pending_approval
```

### Example: Approve a Plan

```bash
curl -X POST http://localhost:8081/api/v1/plans/plan-a1b2c3d4/approve \
  -H "Content-Type: application/json" \
  -d '{"comment": "Looks good"}'
```
