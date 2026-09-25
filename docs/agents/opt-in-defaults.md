# Agent Invariants: Opt-in Defaults

Cross-cutting features that are deliberately OFF by default; flipping any of
these defaults is a product decision, not a code cleanup. Referenced from the
root AGENTS.md. Update both in the same commit per the root maintenance rule.

## State mode is per-skill opt-in

`SKILL.state` execution (`internal/agent/skill_state.go`) activates only when
BOTH the skill frontmatter declares `state: true` AND `skills.state.enabled`
is true (default false). A skill declaring `state: true` with no runtime wired
falls back to the conversation path. Never force state mode on audit, debug,
or provenance tasks — for those, the history IS the deliverable (SKILL.state
§7). The state Σ uses null-deletion semantics: explicit `null` deletes a key,
a missing key leaves it unchanged.

## Phase dispatch mode is per-config opt-in

`plans.parallel_phases` (default false) preserves strict serial plan phases;
when true, phase starts are frontier-driven (artifact + dependency gating;
list order is a tiebreak only), conversationIDs stay phase-scoped
(`phase-<phaseID>-<stepID>`), per-phase worktrees are provisioned via the
orchestrator hook (leaf 03) and win in step working-dir resolution
(`internal/daemon` `resolveStepWorkingDirFor`: phase worktree > session
WorktreePath > ProjectPath > session CWD), and BudgetHierarchy phase
selection is per-phase (multi-select). Subscribers to the phase-transition
hook must tolerate `fromPhase == ""` (frontier activations have no completed
predecessor). Flipping the default is a product decision, not a code cleanup.
See docs/workflows/agent-orchestration.md (phase frontier section).

## Plan compiler pipeline is opt-in

`plans.plan_compiler_enabled` (default false) gates the brainstorm
draft→seal→compile planning pipeline. When false, the legacy JSON
`spec_plan` path is byte-identical — no code may assume the draft store
(`task.Metadata["plan_draft"]`) exists. When true, the draft IS the
interview: the one-shot `task.interview` path stays only for the legacy
pipeline. `plan.seal` and `plan.draft` are RPC-only, never a bus topic.
See docs/workflows/agent-orchestration.md ("Plan compiler pipeline").

## Wiki/state defaults

`skills.wiki` is enabled by default but inert until wired into the daemon
(writes happen only via the learning pipeline + evolver paths);
`skills.state.enabled` and `skills.evolver.enabled` default false. Flipping
these defaults is a product decision, not a code cleanup.

## Wiki and traces are evolver-only

The skill knowledge stores (`internal/selfimprove` WikiStore + TraceStore,
rooted at `skills.wiki.dir`) are inputs to the skill EVOLVER only. They must
never be reachable from `ContextInjector`, `BuildSystemPrompt`, or any
inference-path prompt builder (WikiSkill §5.1: giving the worker wiki access
during evolution degrades final skill quality). Sampling constants
(5 fail / 3 pass traces, 15k chars) live in code, not config.

Every loop that serves user turns must be wired for trace persistence: the
primary loop (components.go, `agent.WithTraceWriter`) AND every
registry-created specialist loop (`AgentRegistry.SetTraceWriter`) — chat,
coder, etc. turns all reach the store via `agent.NewTraceStoreWriter` +
`traceStorePersist`. When adding a new loop construction path, wire these
three (trace writer, usage tracker, learning pipeline) or the evolver
blind spot grows.

## Skill evolver ordering: constructed after its dependencies

The evolver requires `SkillUsageTracker`, `SkillWriter`, and `PlanManager`.
It is constructed by `initializeSkillEvolver` (components_wiki.go), invoked
from daemon.go AFTER the plan system initializes — NOT inside
`initializeSkills`, which runs before those dependencies exist (the old
inline gate was always false; found by the wiki smoke test, 2026-08-29).
Keep this ordering if you refactor daemon startup.
