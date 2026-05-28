# Plans

Plans are project-scoped entities with their own lifecycle, stored as markdown files on disk with SQLite metadata tracking. They sit above the existing task system as a coordination layer and synthesize into Tasks + TaskSteps on approval.

## Plan Lifecycle

Plans progress through a defined set of states:

```
planning -> draft -> pending_approval -> approved -> executing -> completed -> confirmed
                                                                  ^
                                                            (all steps done,
                                                             awaiting sign-off)
  Any state -> cancelled
```

| State | Description |
|-------|-------------|
| `planning` | Planner agent is actively generating the plan.md |
| `draft` | plan.md written to disk and parsed, ready for user review |
| `pending_approval` | Plan submitted for user approval, awaiting explicit action |
| `approved` | User approved, synthesizing into tasks |
| `executing` | Tasks created, TacticalScheduler running steps |
| `completed` | All steps finished, awaiting human sign-off |
| `confirmed` | Human signed off, plan is done |
| `cancelled` | Plan cancelled at any point |
| `failed` | Plan execution failed |

## plan.md Format

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

### Parsing Rules

- `## Meta` section: YAML-like key-value pairs for plan metadata
- `## Phase N: <name>` headings: define phases, bracket-enclosed state annotation
- Numbered list items: steps with `[status]` annotations and optional `(depends: N)` cross-references
- `~~strikethrough~~`: completed steps (visual indicator in any renderer)
- Status in Meta section is the canonical state, kept in sync by `PlanManager`

## Plan-to-Task Mapping

Plans synthesize into the existing task system on approval:

```
Plan (1:1) -> parent Task
+-- Phase 1: Design (1:1) -> child Task
|   +-- Step 1 -> TaskStep
|   +-- Step 2 -> TaskStep (depends_on: [step1])
+-- Phase 2: Implementation (1:1) -> child Task
|   +-- Step 3 -> TaskStep (depends_on: [step2])
|   +-- Step 4 -> TaskStep (depends_on: [step3])
+-- Phase 3: Testing (1:1) -> child Task
    +-- Step 5 -> TaskStep (depends_on: [step4])
    +-- Step 6 -> TaskStep (depends_on: [step5])
```

### Synthesis Flow

When a plan is approved, `PlanManager.Synthesize()` creates the task hierarchy:

1. Create parent Task linked to `Plan.TaskID`
2. For each PlanPhase:
   - Create child Task with `parent_id`
   - Parse steps from plan.md section
   - Create TaskSteps with dependency chains
3. Publish `task.planned` bus event
4. TacticalScheduler picks up as usual

### Progress Flowback

Task completion events flow back to update plan progress:

| Bus Event | Plan Update |
|-----------|-------------|
| `task.step.completed` | Increment `PlanPhase.CompletedSteps` |
| `task.completed` (child) | Update `PlanPhase.State` |
| `task.completed` (parent) | Update `Plan.State` to `completed` |

## Configuration

Plans are configured via the `plans` section in `meept.json5`:

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

Access via config editor: `meept config plans`

## Bus Events

`PlanManager` publishes the following events on the message bus:

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

## Package Structure

```
internal/plan/
  plan.go          # Plan, PlanPhase, PlanSignoff models + state enums
  store.go         # PlanStore interface
  store_sqlite.go  # SQLite implementation
  manager.go       # PlanManager (lifecycle, synthesis, progress tracking)
  parser.go        # plan.md parser (extract phases, steps, dependencies)
  writer.go        # plan.md writer (update status annotations)
  handler.go       # Bus event handler (subscribes to plan.* and task.* events)
```
