# Multi-Agent Orchestration

## Overview
Meept uses a multi-agent architecture where specialist agents handle different task types. The dispatcher agent routes incoming requests to appropriate specialists based on task requirements and agent capabilities.

## Problem
Single-agent systems struggle with complex tasks requiring different expertise. Multi-agent orchestration enables:
- Task decomposition into specialized subtasks
- Dynamic agent discovery and delegation
- Collaborative planning with review workflows
- Efficient resource allocation

## Behavior

### Agent Architecture
| Agent ID | Role | Purpose |
|----------|------|---------|
| `dispatcher` | Dispatcher | Intake, classify, route to specialists |
| `chat` | Executor | General conversation |
| `coder` | Executor | File ops, shell, coding tasks |
| `debugger` | Executor | Troubleshooting, bug fixing |
| `planner` | Executor | Task decomposition, planning |
| `analyst` | Executor | Research, data analysis |
| `committer` | Executor | Git operations |
| `scheduler` | Executor | Job scheduling |
| `writer` | Executor | Long-form writing (essays, docs, briefs) |
| `architect` | Executor | System design, tech evaluation, trade-off analysis |
| `skeptic` | Executor | Stress-tests claims, surfaces contradictions |
| `librarian` | Executor | Memory steward — reflection, tag hygiene, epistemic integrity |

### Task Flow
1. **Intake**: Dispatcher receives user request
2. **Classification**: Dispatcher analyzes task requirements
3. **Memory Search**: Relevant context retrieved from memory
4. **Agent Discovery**: Available specialists identified via `platform_agents`
5. **Delegation**: Task routed via `delegate_task`
6. **Execution**: Specialist agent performs work with evidence collection
7. **Dynamic Handoff**: Agent may call `request_handoff` to inject a new step for another specialist mid-task
8. **Validation**: Evidence verified against claims (Deterministic Execution)
9. **Review**: Optional collaborative review workflow
10. **Report Routing**: `ReportRouter` determines next action (close, handoff, notify user, or error)
11. **Completion**: Results returned to user

### Phase frontier (parallel phases)

By default, plan phases run **strictly serially**: when a phase completes,
the orchestrator starts the next phase in list order. Setting
`plans.parallel_phases = true` (config key `plans.parallel_phases`, default
`false`) switches phase dispatch to a **ready frontier**: after each phase
terminal event, every phase whose dependencies are satisfied starts at once.

**Frontier rule.** A phase is ready to start when:

1. every *required* artifact it declares in `Consumes` is present in the
   artifact store (produced by a completed step of another phase). Optional
   consumes are best-effort and never block;
2. no phase it transitively depends on still has unfinished steps.
   Dependency edges are: every consume (required or optional) whose
   producing phase exists, plus cross-phase step `DependsOn` links (a step
   in phase X depending on a step in phase Y creates an X→Y edge).

The phase list order is only a tiebreak (stable start ordering) and an
anti-cycle escape: if the remaining graph cannot advance, the orchestrator
logs a warning and falls back to the serial list-order pick rather than
deadlocking.

**Enabling it.** Set `plans.parallel_phases = true` under `plans` in
`~/.meept/meept.json5` (or the project config). The default `false`
preserves byte-identical serial behavior. Parallel-friendly plans declare
`Produces`/`Consume` artifacts on their phases and group independent work
into sibling phases — a plan with no artifact declarations falls back to
effectively serial dispatch, safely.

**Per-phase worktrees.** When the orchestrator's worktree provisioner is
wired, each starting phase under parallel mode gets its own worktree, and
step jobs of that phase run in it: the step working-dir resolution prefers
the phase worktree over the session chain
(`phase worktree > WorktreePath > ProjectPath > session CWD`). The
worktree is skipped — and the session chain used unchanged — when the flag
is off (serial mode), when no provisioner is wired, when the plan performs
no file writes (the provisioner returns "no worktree needed"), or when
provisioning fails (a Warn is logged; the phase proceeds without
isolation).

**Budgets.** Under parallel phases, the per-loop budget hierarchy selects
the phase context per-phase (each phase start advances the loop's budget
phase), so concurrent phases each see their own budget tier rather than
sharing the serial "current phase" slot.

**Observability.** Each phase start fires the existing phase-transition
hook (the same one serial mode uses; frontier activations report an empty
`fromPhase`). Completion still publishes `plan.phase_completed` and step
progress still publishes `task.progress`. **No new bus topics** were added
for parallel dispatch.

### Report Router (Multi-Agent Handoff)

When an agent completes, the `ReportRouter` examines its structured report and decides what to do next. This replaces the previous behavior where routing decisions were computed but never acted on.

**Route actions:**

| Action | Behavior |
|--------|----------|
| `RouteActionClose` | Agent finished. Format response from accomplishments and observations. |
| `RouteActionRoute` | Hand off to the next suggested agent. Context accumulates across handoffs. |
| `RouteActionNotifyUser` | User input needed. Force notification to all session participants. |
| `RouteActionNotifyError` | Agent failed. Force notification with error details. |

**Properties:**
- **Max depth: 5** — prevents infinite agent-to-agent loops. After 5 handoffs, forces user notification.
- **Context accumulation** — each handoff passes the previous agent's `Accomplished`, `Issues`, `Observations`, and `DecisionContext` to the next agent.
- **Single response** — the caller receives one final synthesized response, not N intermediate ones.

### Collaborative Planning
- **Review/Approval Workflow**: Tasks can require reviewer approval
- **Revision Cycles**: Agents can iterate based on feedback
- **Auto-Approve Patterns**: Simple tasks approved automatically

### Coworker Awareness
Agents discover each other dynamically:
- `platform_agents`: List available agents and capabilities
- `platform_tools`: List registered tools
- `delegate_task`: Route tasks to specific agents (synchronous, blocking)
- `request_handoff`: Dynamically inject a new step and route to another agent (async, non-blocking)

### Dynamic Agent Handoff

When an agent discovers mid-execution that it needs expertise from another specialist, it can use `request_handoff` to dynamically inject a new step into the running task's DAG without going through the dispatcher.

**How it works:**
1. Agent calls `request_handoff` with target agent ID, description, reason, and partial results
2. Tool publishes `orchestrator.handoff` bus event via `models.NewBusMessage()`
3. `Orchestrator.handleHandoff()` receives the event and delegates to `TacticalScheduler.HandleHandoff()`
4. `HandleHandoff` creates a new `TaskStep` with dependency on the originating step
5. Downstream steps are rewired to depend on the injected step (when `inject_after` is true)
6. New step is promoted and scheduled via the existing step lifecycle

**Amendment integration:**
When `HandoffUseAmendment` is enabled and an `AmendmentSubmitter` is configured, handoff requests route through the amendment system for review/approval before step creation. If the amendment is rejected, the handoff fails. If `HandoffUseAmendment` is true but no `AmendmentSubmitter` is available, it falls through to direct creation.

**Rate limiting:**
`MaxHandoffSteps` (default 5) limits the number of handoff steps per task to prevent runaway handoff chains.

**Implementation:**
- Tool: `internal/tools/builtin/handoff.go` — `RequestHandoffTool` struct
- Handler: `internal/agent/tactical.go` — `HandleHandoff()`, `rewireDownstreamDeps()`, `agentIDToToolHint()`
- Wiring: `internal/agent/orchestrator.go` — subscribes to `orchestrator.handoff`
- Registration: `internal/daemon/components.go` — registers tool with bus and agent existence check

## Configuration

```toml
[multiagent]
enabled = true
dispatcher_model = "claude-opus-4-5-20251101"
default_model = "claude-sonnet-4-5-20241022"
max_memory_refs = 20
context_search_limit = 10

[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
prompts_dir = "config/prompts"
default_model = ""
dispatcher_id = "dispatcher"

[collaborative]
enabled = true
reviewer_mapping = {}
auto_approve_simple = true
max_revision_cycles = 3
```

## Observability

### Logging
- Agent delegation events
- Task routing decisions
- Memory context injection
- Review workflow state changes
- Report router decisions (action, agent, depth, has_report)
- Multi-agent handoff events (from/to/depth)

### Metrics
- Agent utilization rates
- Task completion times
- Memory hit rates
- Review approval rates
- Multi-agent handoff depth per conversation
- Route action distribution (close vs route vs notify vs error)

### Debug Info
- Current agent assignments
- Task queue status
- Memory context relevance scores
- Review workflow state
- Current routing depth per active handoff chain

## Edge Cases

### No Suitable Agent
- Dispatcher returns "no specialist available"
- Suggests manual agent selection
- Logs capability gap for monitoring

### Agent Unavailable
- Task queued for retry
- Alternative agents considered
- User notified of delay

### Memory Context Missing
- Dispatcher proceeds with limited context
- Logs missing context warning
- Subsequent tasks may benefit from current execution

### Review Cycle Limit
- Maximum revision cycles enforced
- Final decision forced after limit
- User notified of resolution

### Max Route Depth Exceeded
- `ReportRouter` forces `RouteActionNotifyUser` after 5 handoffs
- Accumulated response includes what each agent accomplished
- Warning logged with depth and max depth values

### Agent Reports No Suggested Next Agent
- `RouteActionRoute` requires `SuggestedNextAgent` in the report
- Falls back to `RouteActionClose` if missing

### New knowledge-work intents

(Plan 2 — Agent Roster Extension.) The dispatcher recognizes four additional intent types that route to the new executor agents:

| Intent | Constant | Default Agent | Example Trigger |
|--------|----------|---------------|-----------------|
| Write | `IntentWrite` | `writer` | "Write an essay about X" |
| Architect | `IntentArchitect` | `architect` | "Design a system for X" |
| Skeptic | `IntentSkeptic` | `skeptic` | "What's wrong with my reasoning?" |
| Librarian | `IntentLibrarian` | `librarian` | "Review my memory" |
| Image gen | `IntentImageGen` | `image-gen` | "Generate an image of X" |
| Video gen | `IntentVideoGen` | `video-gen` | "Generate a video of X" |
| Image id | `IntentImageID` | `image-id` | "Identify this image" |

These intents follow the same routing pipeline as the originals: dispatcher classification → memory search → agent discovery → delegation → execution → report routing. The `librarian` and `skeptic` agents additionally consume edges from the epistemic memory graph (see [Multi-Agent System — Epistemic Memory Integration](../concepts/multi-agent.md#epistemic-memory-integration)).

## Plan Compiler Pipeline (plans.plan_compiler_enabled)

An alternative to the LLM-generated strict-JSON `spec_plan` path: a
human-in-the-loop **brainstorm draft** in plan-dialect v1 markdown that a
deterministic Go compiler turns into `[]plan.PlanPhaseSpec`. Opt-in via
config (default **false**); when false the legacy JSON path is
byte-identical.

- Normative dialect spec: `docs/workflows/plan-dialect.md`
- Agent-facing template: `config/prompts/planner/plan_draft.md`
- Compiler: `plan.CompileSealed` (`internal/plan/compiler.go`)
- Tree emission: `plan.EmitTree` / `plan.ShouldEmitTree` (`internal/plan/treeemit.go`)

### Draft lifecycle

1. A task routed to `mode: plan` with the flag on seeds a **draft
   scaffold** on `task.Metadata["plan_draft"]` (Goal + Open Questions
   derived from the request; no LLM). The one-shot interview
   (`ConductInterview`/`task.interview`) is skipped — the draft IS the
   interview. The task stays `planning` awaiting a seal.
2. The user and planner agent iterate through normal conversation turns;
   each turn replaces the draft through `plan.draft` (get/save). Saves
   increment `version` and are refused once the task leaves `planning`.
3. `meept plans seal <task-id>` seals: the draft's exact bytes are hashed
   (sha256, recorded in `SealedHash`), compiled (zero LLM), persisted, and
   the task transitions to `executing`.

### Seal result: problems vs sealed

Compile problems (unknown artifact, unresolved Open Questions, consume
before produce, …) come back as a `problems` **result** — not an RPC
error. The draft stays a draft and the problem list feeds the next
brainstorm round. There is no LLM retry. `plan.CompileSealed` returns all
problems in one pass.

### Flat vs tree

`plan.ShouldEmitTree` gates emission: total steps ≤ 6 and every phase
within per-leaf sizing → **flat** steps (existing orchestrator path);
otherwise the compiler output is also rendered as a hierarchical tree
(`master.md` + numbered leaves, per the hierarchical-planning shape) under
`<data_dir>/plan-trees/<task-id>/`. In both modes the flat phases persist
through the existing `PlanManager.CreatePhase` path and the orchestrator
executes them. In tree mode each persisted phase records its tree-leaf
path — the integration point for a future leaf-agent dispatcher (NOT
wired in this tree; leaf dispatch through employee/delegation is
follow-up work).

### RPC + CLI surface

- `plan.seal {task_id}` — seal → compile → persist → execute. Reply:
  `{status:"sealed", hash, mode}` or `{status:"problems", problems}`.
- `plan.draft {task_id, markdown?}` — save (markdown present, returns
  `{status:"saved", version}`) or get (`{status:"draft", markdown,
  version}`).
- RPC only — never a bus topic.
- CLI (on the `meept plans` tree): `draft <task-id>` (status),
  `show-draft <task-id>` (print markdown), `edit-draft <task-id> [file|-]`
  (replace from file/stdin), `seal <task-id>` (prints sealed hash + mode,
  or the problem list, exit 2). `show`/`edit` names belong to the
  plan-lifecycle subcommands, so the draft commands carry the `-draft`
  suffix.

### Wiring

`internal/daemon/daemon.go` registers the pipeline beside the
`SetParallelPhases` site (before orchestrator Start) when
`plans.plan_compiler_enabled` is true: closures over `StrategicPlanner`
adapt it to the `rpc.PlanSealHandler` injected-func seams (`DraftSource`,
`Compile`, `Persist`, `Execute`) in `internal/daemon/plan_seal_wiring.go`.
`SealPlan` mirrors `ApprovePlan`'s tail: persist steps → generate spec →
`executing` → promote ready → `orchestrator.schedule`. The compile phase
cap reuses the planner's existing `MaxPhases` config — no second knob.

## Ralph Loop: Automatic Verification and Replanning

Ralph Loop provides self-correcting task execution by verifying completion evidence and triggering automatic replanning when verification fails.

### Workflow

```
Job Completed
    ↓
Extract task_id from job
    ↓
RalphLoop.CheckCompletion()
    ├── Parse result JSON
    ├── Check evidence array
    ├── Validate evidence against task keywords
    └── Return (isComplete, evidence, needsReplan)
    ↓
If needsReplan:
    ├── RalphLoop.TriggerReplan()
    ├── Publish "orchestrator.replan" bus event
    └── Skip normal completion
Else:
    └── Normal completion processing
```

### Evidence Requirements

Tasks must return structured results with evidence:

```json
{
  "success": true,
  "result": "Refactored database connection pooling",
  "evidence": [
    "Modified db/config.go to add max_connections",
    "Updated db/pool.go initialization logic"
  ]
}
```

Evidence is validated by checking if it mentions key terms from the task description. For example:
- Task: "Fix the login bug with session timeout"
- Key terms: `fix`, `login`, `bug`, `session`, `timeout`
- Valid evidence: "Fixed session timeout in login handler"

### Opt-in Configuration

Ralph Loop uses layered opt-in:

| Layer | Decision | Override |
|-------|----------|----------|
| **Dispatcher** | Intent-based policy | `IntentCode` → Enabled, `IntentChat` → Disabled |
| **Strategic Planner** | Complexity analysis | High complexity → Enable, Trivial → Disable |
| **Orchestrator** | Runtime heuristics | >3 steps or uncertainty markers → Enable |

### Files

- `internal/agent/ralph_loop.go` — Verification and replanning logic
- `internal/agent/orchestrator.go` — `handleJobCompleted()` integration
- `internal/agent/dispatcher.go` — Intent classification with `RalphLoopPolicy`
- `internal/agent/strategic.go` — Complexity-based overrides

See [Ralph Loop: Self-Referential Task Verification](../concepts/ralph-loop.md) for full documentation.

## Verification Fix-Loop Escalation (Adversarial Verification)

When the verification auto-trigger (`internal/agent/verification_hook.go`)
exhausts an agent's `max_fix_loops` budget, the next fix iteration is
switched to the agent's configured `escalation_model` instead of escalating
to the user.

### Workflow

```
Verifier FAIL (fixCount > max_fix_loops)
    ↓
DecideEscalation (spec.EscalationModel, resolver)
    ├── no escalation model / resolution failed → legacy escalate-to-user
    └── resolved → ApplyEscalation
            ├── arms PERSISTENT loop model override (full max_fix_loops budget)
            ├── publishes agent.model_escalated on the bus
            └── next fix iteration (agent turn + verifier spawn) runs escalated
    ↓
Fix loop continues on the escalation model with its own max_fix_loops budget
    ↓
Fresh turn without a pending escalation (verifier PASS/PARTIAL, user turn)
    ↓
Fresh-turn sweep clears the persistent override → base model restored
```

### Key Properties

- **R1 — no sticky escalation:** the override is persistent within the
  escalated window but cleared at the start of every turn without a
  re-armed escalation (`HookRegistry.ClearFreshTurnOverrides`, called from
  `RunOnceWithParts`). The escalated model never leaks into an unrelated
  turn.
- **Alias inheritance:** an alias escalation target inherits alias
  rotation, cooldowns, and quota blocks. A fully quota-blocked escalation
  alias fails with `ErrAllModelsQuotaBlocked` and the loop's existing
  quota handling takes over — no second handling path.
- **Observability:** bus topic `agent.model_escalated` (payload
  `{agent_id, from_model, to_model, reason, fix_loops}`; classified
  `agent_progress` on WS, never `chat_message`) and a routing-log row with
  reason `escalation`.

### Files

- `internal/llm/resolver.go` — `ResolveEscalationRef` (alias or
  `provider/model` → loop model ref)
- `internal/agent/verification_escalation.go` — `DecideEscalation`,
  `ApplyEscalation`, fresh-turn clear
- `internal/agent/verification_hook.go` — hook wiring seams
  (`SetResolver`, `SetOverrideApplier`, `SetEventPublisher`, ...)
- `internal/agent/loop.go` — construction-site wiring
  (`SetAgentSpec`/`SetResolver`) and the fresh-turn sweep call
- `internal/comm/http/server.go` — WS topic classification

## E2E naive-user regression (chat-dispatch-ux)

`scripts/e2e-naive-user-chat.sh` replays the 2026-09-04 naive-user
transcript against a scratch daemon (temp state dir, temp socket,
probed free port, sandboxed HOME) and asserts the harness-level
contract on every reply:

- no `Task <id> completed.` stubs (sync replies carry the real step
  result — leaf 01);
- honest failure states — errored steps reject review and fail the
  task (leaf 02);
- files land in the session's project dir, never the daemon cwd
  (leaf 03);
- no raw platform-tool catalogs or agent rosters become chat replies
  (leaf 05);
- quota failures surface as user-visible quota messages (leaf 06);
- daemon lifecycle hygiene (clean termination, temp-dir removal).

Usage: `bash scripts/e2e-naive-user-chat.sh [--keep]`. Requires a
provider reachable via env credentials (config/models.json5 is copied
into the sandbox); provider-unreachable turns are reported as SKIP
with a printed reason — never silent. `--keep` preserves the scratch
workdir for inspection.
