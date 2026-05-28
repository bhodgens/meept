# Plan-Session Association Design

## Overview

Associate formal plans (plan.md files) with sessions in meept, enabling visual assessment of plan creation, approval, and completion progress across the TUI, CLI, and HTTP API.

Plans are project-scoped entities with their own lifecycle, stored as markdown files on disk with SQLite metadata tracking. They synthesize into the existing task system (Tasks + TaskSteps) on approval.

## Data Models

### Plan

```go
type Plan struct {
    ID            string     // "plan-<hex>"
    Title         string
    Description   string
    FilePath      string     // Absolute path to plan.md
    ProjectID     string     // Project this plan belongs to
    State         PlanState
    CreatedAt     time.Time
    UpdatedAt     time.Time
    ApprovedAt    *time.Time
    ConfirmedAt   *time.Time
    ApprovedBy    string     // Client/session that approved
    ConfirmedBy   string     // Client/session that confirmed
    TaskID        string     // Linked task (set on approval)
    SourceSession string     // Session that originated the plan
}
```

### Plan States

```
planning → draft → pending_approval → approved → executing → completed → confirmed
                                                              ↑
                                                        (all steps done,
                                                         awaiting sign-off)
  Any state → cancelled
```

- `planning` — planner agent is actively generating the plan.md
- `draft` — plan.md written to disk, not yet submitted for review
- `pending_approval` — plan submitted, awaiting user review
- `approved` — user approved, synthesizing into tasks
- `executing` — tasks created, TacticalScheduler running steps
- `completed` — all steps finished, awaiting human sign-off
- `confirmed` — human signed off, plan is done
- `cancelled` — plan cancelled at any point

### PlanPhase

```go
type PlanPhase struct {
    ID              string
    PlanID          string
    Name            string    // e.g., "design", "implementation", "testing"
    Sequence        int
    TotalSteps      int       // Parsed from plan.md
    CompletedSteps  int
    FailedSteps     int
    State           PhaseState // pending → in_progress → completed → confirmed
}
```

### PlanSignoff

```go
type PlanSignoff struct {
    ID        string
    PlanID    string
    PhaseID   string     // Empty = full plan signoff
    SessionID string
    By        string     // Client identifier
    Action    string     // "approved", "rejected", "confirmed", "revision_requested"
    Comment   string
    CreatedAt time.Time
}
```

### SQLite Tables

```sql
CREATE TABLE plans (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT,
    file_path TEXT NOT NULL,
    project_id TEXT,
    state TEXT NOT NULL DEFAULT 'planning',
    task_id TEXT,
    source_session TEXT,
    approved_at DATETIME,
    confirmed_at DATETIME,
    approved_by TEXT,
    confirmed_by TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE plan_phases (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    total_steps INTEGER NOT NULL DEFAULT 0,
    completed_steps INTEGER NOT NULL DEFAULT 0,
    failed_steps INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE plan_sessions (
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    linked_at DATETIME NOT NULL,
    PRIMARY KEY (plan_id, session_id)
);

CREATE TABLE plan_signoffs (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    phase_id TEXT REFERENCES plan_phases(id),
    session_id TEXT NOT NULL,
    by TEXT NOT NULL,
    action TEXT NOT NULL,
    comment TEXT,
    created_at DATETIME NOT NULL
);
```

## Plan.md Format

Plans are markdown files with structured sections parseable by `PlanManager`:

```markdown
# Plan: Add OAuth2 Token Refresh

## Meta
- plan_id: plan-a1b2c3d4
- project: my-app
- created: 2026-05-28T10:30:00Z
- status: planning
- threshold: complex

## Summary
Implement automatic OAuth2 token refresh for the API gateway
to prevent 401 errors on expired sessions.

## Phase 1: Design [pending]
1. ~~Analyze current auth flow~~ [completed]
2. Design token refresh scheme [pending]
3. Document API contract changes [pending]

## Phase 2: Implementation [pending]
4. Update auth middleware [pending] (depends: 2)
5. Add refresh endpoint [pending] (depends: 2)
6. Implement client-side retry [pending] (depends: 4, 5)

## Phase 3: Testing [pending]
7. Write unit tests for refresh logic [pending] (depends: 5)
8. Integration test full flow [pending] (depends: 6, 7)

## Notes
- Phase 2 and 3 can partially overlap
- Refresh tokens must be rotated per RFC 6749
```

Parsing rules:
- `## Meta` section: YAML-like key-value pairs for plan metadata
- `## Phase N: <name>` headings: define phases, bracket-enclosed state annotation
- Numbered list items: steps with `[status]` annotations and optional `(depends: N)` cross-references
- `~~strikethrough~~`: completed steps (visual indicator in any renderer)
- Status in Meta section is the canonical state, kept in sync by `PlanManager`

## Plan-to-Task Mapping

Plans synthesize into the existing task system on approval:

```
Plan (1:1) → parent Task
├── Phase 1: Design (1:1) → child Task
│   ├── Step 1 → TaskStep
│   └── Step 2 → TaskStep (depends_on: [step1])
├── Phase 2: Implementation (1:1) → child Task
│   ├── Step 3 → TaskStep (depends_on: [step2])
│   └── Step 4 → TaskStep (depends_on: [step3])
└── Phase 3: Testing (1:1) → child Task
    ├── Step 5 → TaskStep (depends_on: [step4])
    └── Step 6 → TaskStep (depends_on: [step5])
```

### Synthesis Flow

```
plan.approved bus event
  → PlanManager.Synthesize()
    → Create parent Task linked to Plan.TaskID
    → For each PlanPhase:
        → Create child Task with parent_id
        → Parse steps from plan.md section
        → Create TaskSteps with dependency chains
    → Publish task.planned
    → TacticalScheduler picks up as usual
```

### Progress Flowback

```
task.step.completed → PlanManager → updates PlanPhase.CompletedSteps
task.completed (child) → PlanManager → updates PlanPhase.State
task.completed (parent) → PlanManager → updates Plan.State to "completed"
```

The plan system sits above the existing task system as a coordination layer. It does not re-implement execution.

## Plan Creation Triggers

Configurable via `plans.mode` in config:

| Mode | Behavior |
|------|----------|
| `threshold` (default) | Planner decides based on complexity (step count, keywords, intent type) |
| `always` | Every non-trivial request generates a plan |
| `off` | Plans only created via explicit `/plan` command |

On-demand: `/plan <description>` in chat forces plan creation regardless of mode.

Threshold logic:
- `min_steps >= 3`: LLM decomposition produces 3+ steps → trigger plan
- `complexity_keywords`: request matches keywords like "refactor", "migrate", etc.
- `always_plan_intents`: intent types like "plan", "implement", "build" always trigger

## Approval Workflow

1. Plan generated → state: `draft`
2. Plan submitted for review → state: `pending_approval`
3. Inline chat notification appears with plan summary + link to Plans tab
4. User reviews in Plans tab:
   - `[a] approve` — plan approved, synthesis begins
   - `[r] reject` — plan rejected with optional feedback
   - `[v] revise` — plan revised and re-submitted (up to `max_revisions` rounds)
5. On approval: `PlanManager.Synthesize()` creates task hierarchy
6. On all steps complete: state → `completed`
7. User confirms sign-off: state → `confirmed`

## Configuration

New `plans` section in `meept.json5`:

```json5
{
  plans: {
    mode: "threshold",

    threshold: {
      min_steps: 3,
      complexity_keywords: [
        "refactor", "migrate", "implement", "redesign",
        "rewrite", "integrate", "architect",
      ],
      always_plan_intents: ["plan", "implement", "build"],
    },

    storage: {
      default_path: "docs/plans",
      external_path: "",
      filename_template: "{{slug}}.md",
    },

    approval: {
      require_approval: true,
      auto_approve_simple: false,
      allow_revision: true,
      max_revisions: 3,
    },

    confirmation: {
      require_signoff: true,
      auto_confirm_phases: false,
    },
  },
}
```

Config template at `config/meept.json5` updated with this section.
`meept config plans` opens the TUI config editor at this section.

## Bus Events

New events published by `PlanManager`:

| Event | Payload | Trigger |
|-------|---------|---------|
| `plan.created` | PlanID, Title, FilePath | plan.md generated |
| `plan.submitting` | PlanID | moved to pending_approval |
| `plan.approved` | PlanID, TaskID | approved, synthesizing |
| `plan.rejected` | PlanID, Comment | rejected with feedback |
| `plan.revised` | PlanID, RevisionCount | revised and re-submitted |
| `plan.executing` | PlanID, TaskID | synthesis complete |
| `plan.phase_started` | PlanID, PhaseID | phase's first step begins |
| `plan.phase_completed` | PlanID, PhaseID | all phase steps complete |
| `plan.completed` | PlanID | all phases complete |
| `plan.confirmed` | PlanID, ConfirmedBy | human signed off |
| `plan.cancelled` | PlanID, Reason | plan cancelled |

## Integration Points

| Component | Integration |
|-----------|-------------|
| `internal/plan/` | New package: Plan, PlanPhase, PlanSignoff models; PlanStore (SQLite); PlanManager lifecycle |
| `internal/agent/dispatcher.go` | Check `plans.mode` config; route to PlanManager for plan-eligible requests |
| `internal/agent/strategic.go` | Called by `PlanManager.Synthesize()` to create task hierarchy |
| `internal/agent/orchestrator.go` | Subscribe to `task.step.completed`/`task.completed`, forward to PlanManager |
| `internal/session/` | New bus topic `session.get_plans` returns plans linked to a session |
| `internal/config/schema.go` | New `PlansConfig` struct with nested config types |
| `config/meept.json5` | Template updated with `plans` section |
| `internal/rpc/` | New methods: PlansCreate, PlansList, PlansGet, PlansApprove, PlansReject, PlansConfirm, PlansGetBySession |
| `internal/comm/http/` | REST: GET/POST /api/v1/plans, POST /api/v1/plans/:id/approve|reject|confirm |
| `cmd/meept/` | CLI: `meept plans list`, `meept plans show <id>`, `meept plans approve <id>`, `meept plans reject <id>`, `meept plans confirm <id>` |
| `internal/tui/` | ViewPlans tab, header badges, session picker indicators, chat notifications |

## TUI Design

### Header Bar — Plan Badges

Below the session name, plan badges appear color-coded by state:

```
session-name | Description text                                              [project branch*]
  plans: 1 confirmed  1 executing (4/8 steps)  1 pending approval
[0] chat  [1] tasks  [2] plans  [3] queue  [4] memory
```

### Session Picker — Plan Indicators

Each session shows plan count with state-colored indicators:

```
> auth-overhaul     ■ 2 plans: 1 exec 1 pending        5m ago
  bugfix-session    ■ 1 plan: confirmed                 2h ago
  quick-chat        no plans                             1d ago
  refactor-api      ■ 3 plans: 1 done 1 failed 1 exec   3d ago
```

### Plans Tab — Full Plan List

```
 plans for: auth-overhaul                              filter: [all] active completed pending

 ● Add OAuth2 Token Refresh                         executing
   plan-a1b2 · docs/plans/oauth2-refresh.md · my-app
   Phase 1: Design     ██████████ 3/3 confirmed
   Phase 2: Impl       ██████░░░░ 2/4 executing
   Phase 3: Testing    ░░░░░░░░░░ 0/3 pending
    4/8 steps · 1.2k tokens · agent: coder

 ● Rate Limit Middleware                            pending approval
   plan-b2c3 · docs/plans/rate-limit.md · my-app
   3 phases · 11 steps · awaiting review
    [a] approve  [r] reject  [v] revise  [enter] view plan.md

 ● Fix Auth Header Parsing                          confirmed
   plan-c3d4 · docs/plans/auth-header-fix.md · my-app
   2 phases · 5/5 steps · confirmed by caimlas 2h ago

──────────────────────────────────────────────────────────
[/] filter · [enter] detail · [e] edit plan.md · [a] approve · [c] confirm · [n] new plan
```

### State Color Coding

- `planning` — blue
- `draft` — gray
- `pending_approval` — blue
- `approved` — green
- `executing` — amber/yellow
- `completed` — green
- `confirmed` — green, bold
- `failed` — red
- `cancelled` — gray, hollow

### Chat Inline Notification

When a plan enters `pending_approval`, an inline message appears in chat:

```
+ plan ready for review ----------------------------------------+
| Plan: "Add OAuth2 Token Refresh"
| 3 phases · 8 steps · threshold: complex
| [2] plans tab to review  ·  /approve plan-a1b2
+--------------------------------------------------------------+
```

## Package Structure

```
internal/plan/
├── plan.go          # Plan, PlanPhase, PlanSignoff models + PlanState/PhaseState enums
├── store.go         # PlanStore interface
├── store_sqlite.go  # SQLite implementation
├── manager.go       # PlanManager (lifecycle, synthesis, progress tracking)
├── parser.go        # plan.md parser (extract phases, steps, dependencies)
├── writer.go        # plan.md writer (update status annotations)
├── handler.go       # Bus event handler (subscribes to plan.* and task.* events)
└── manager_test.go  # Tests
```

## Cleanup: Deferred CollaborativePlanner

The final phase removes the deferred `CollaborativePlanner` code:
- Delete `internal/agent/collaborative.go` (TaskPlan, TaskStep, PlanReview, CollaborativePlanner)
- Remove `NewCollaborativePlanner` references
- The `WorkspaceManager` in `internal/agent/workspace.go` is retained — it's useful for general workspace operations. Only the collaborative-specific usage is removed.
- Any tests referencing collaborative types are updated.

This cleanup happens last, after the new plan system is fully wired and tested.

## Documentation Updates

The following documentation must be updated to stay in sync with the implementation:

### New Documentation

| Document | Purpose |
|----------|---------|
| `docs/concepts/plans.md` | New page: plan concepts, lifecycle, plan.md format, plan-to-task mapping |
| `docs/workflows/plans.md` | New feature spec: plan workflow end-to-end |
| `docs/reference/http-api.md` | Add plan endpoints (GET/POST /api/v1/plans, approve/reject/confirm) |
| `docs/reference/http-api/openapi.yaml` | Add plan endpoint schemas |

### Updated Documentation

| Document | Changes |
|----------|---------|
| `CLAUDE.md` | Add `internal/plan/` to architecture table, add `meept plans` CLI commands, update agent table if planner agent changes |
| `docs/concepts/architecture.md` | Add Plan layer to request flow diagram, add PlanManager to component map |
| `docs/concepts/multi-agent.md` | Update planner agent description, add plan creation routing |
| `docs/reference/cli.md` | Add `meept plans` subcommand reference |
| `docs/configuration/index.md` | Add plans config section documentation |
| `mkdocs.yml` | Add `plans.md` entries to nav under concepts and workflows |

### Documentation Rules

- New CLI commands require updating `docs/reference/cli.md` immediately
- New agents/tools/architectural components must be documented in `docs/concepts/architecture.md`
- All doc pages must be linked from `mkdocs.yml` nav
- README.md feature list must stay in sync with `docs/workflows/`
- Struct changes in `internal/config/schema.go` require running `make docs-generate`
