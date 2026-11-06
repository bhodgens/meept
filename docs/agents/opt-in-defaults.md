# Agent Invariants: Opt-in Defaults

Cross-cutting features that are deliberately OFF by default; flipping any of
these defaults is a product decision, not a code cleanup. Referenced from the
root AGENTS.md. Update both in the same commit per the root maintenance rule.

## Memory subsystem defaults are ON (authorized, 2026-10-30)

The five memory subsystem defaults are TRUE per the memory second-brain
product decisions (plan `docs/plans/20261030-memory-second-brain/master.md`,
User decisions table, frozen 2026-10-30 — that plan is the authorization for
this flip):

- **D1: `memory.bridges.enabled` = true.** "Q1 a" — bridging facts
  (IndexRAG stage 2) generate by default now; the leaf-07 substitution
  experiment may flip it back if the eval loses.
- **D3: full default-on** ("sure", chose "enable it all by default"):
  - `memory.l0_enabled` = true — every turn records L0 lines and distills
    into L1 claims + L2 scenes on the persistent queue.
  - `memory.usefulness.enabled` = true — consolidation evicts by
    usefulness score (net votes <= -2 first, then the bottom floor).
  - `memory.epistemic.ambient_extraction.enabled` = true — turn-side
    ambient claims land in the human review queue as `status=auto`.
  - `memory.knowledge.enabled` = true — the knowledge_* tools register out
    of the box (on-demand tool access; nothing ambiently injected).

These are OPT-OUT knobs now: every one still honors an explicit `false` in
the user's config. Flipping any of them back OFF by default is again a
product decision. Not flipped (still opt-in, deliberate):
`memory.conflict.enabled` (an extra LLM call per extraction batch),
`memory.tiers.enabled` (PROPOSED below). Models-less hosts degrade by
SKIP-Warn (one Warn per distill-worker cycle, jobs stay queued, L0 lines
preserved) — documented in docs/workflows/memory.md.

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

## Fresh-context critic flags are on by default (disable-form keys)

The three plan-critic features (`fresh_critic_disabled`,
`hostile_critic_disabled`, `interview_options_disabled` in `[plans]`)
default ON per the 2026-11-02 operator decision and are expressed as
INVERTED disable-form keys: absent = enabled, explicit true = disabled
(a true-by-default bool cannot be expressed with an enable-named field;
the original enable-named keys shipped dark for one day and were
renamed). With defaults on and the `hostile_critic_model` slot empty
(the shipped state), a TierComplex plan still cannot self-seal: the
hostile pass skips at runtime (reason `no_model`), the outcome stamps
`hostile_skipped`, and the draft waits at the human seal - intentional,
per the no-clean-seal-without-hostile-reader property. Flipping a
default back is a product decision, not a code cleanup. See
docs/workflows/agent-orchestration.md ("Fresh-Context Plan Critic").

## OTLP metrics export is opt-in

`metrics.otlp.enabled` (default false) gates the daemon's OTLP metrics
exporter (`internal/daemon/otlp_wiring.go`, `internal/metrics/
otlp_exporter.go`). When false, no exporter is constructed, no goroutines
run, no metric leaves the host, and boot logs exactly one Warn line
stating export is disabled. When true, batches POST as OTLP/HTTP JSON to
`<endpoint>/v1/metrics` (stdlib net/http; the `OTLPMetricSink` bridge
keeps the otel SDK out of go.mod). Local SQLite collection (`llm.metrics`)
is unaffected and always on. Flipping this default is a product decision,
not a code cleanup. The sibling trace exporter
(`metrics.otlp_traces.enabled`, default false) follows the same posture;
see `docs/workflows/llm-tracing.md`.

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

## Agent groups default management is opt-in (fail-closed)

Two frozen defaults from the agent-groups tree
(docs/plans/20261030-agent-groups/master.md, decisions D7 and D8 -
that plan is the product-decision authorization):

- **D7: `security.groups.default_managed` defaults FALSE.** Agents with no
  group membership and no binding keep TODAY's behavior (spec allowlist
  only, legacy empty-means-all registry semantics, call-time gate skipped).
  Setting it true routes those agents through the fail-closed `default`
  policy and arms the membership file backstop. Flipping it on an install
  with ungrouped agents is an isolation-posture upgrade that WILL shrink
  their access; treat it as the product decision it is.
- **D8: an empty effective allowlist on a MANAGED subject means DENY-ALL.**
  When a group policy is active for a subject, zero resolved allowed tools
  denies everything at dispatch AND call time; `FilteredToolRegistry`'s
  legacy empty=all behavior must never fire for a managed subject. This is
  the fail-closed reading, not an opt-in: the "default" that stays fixed is
  the legacy empty=all behavior for UNMANAGED subjects only (D7's false
  posture).

## Automated memory-tier eviction is PROPOSED for default-on (NOT yet flipped)

`memory.tiers.enabled` (tree 02 policy-automation leaf) ships default FALSE
per this rule. This section is the product-decision PROPOSAL to flip
automated eviction to default-on; it records intent only - the shipped
default remains false until a product owner accepts this proposal.

- **What flips:** `memory.tiers.enabled` becomes true on fresh configs
  (and `config.MemDefaults()` seeds it), turning on the background
  evict/compress policy tick
  (`internal/memory/tierpolicy`, off the chat path, bounded interval,
  dry-run posture available via `dry_run`). Compression reuses the
  memory.l1_extract distill pipeline; eviction reuses
  PlanUsefulEviction ranking.
- **Rationale:** AMV-L measured 3.1x throughput and 4.2x lower median
  latency for value-based tier lifecycle management over TTL-only
  retention (findings-memory-tiers.md, tier architecture 3), and MaRS
  shows scheduled tier migration beating reactive policies at
  conversation scale. Without automation the tier ceilings exist but
  nothing enforces them between manual consolidation passes.
- **Risk:** an over-aggressive eviction pass could delete low-vote but
  useful memories before distillation captures them. Mitigated by the
  cost bounds (R-B5: min batch 5 / max 100 per pass, per-tier
  watermark, no-op sentinel - an empty pass costs zero LLM calls),
  compress-before-evict ordering (R-B4), and the ~50%-of-overage batch
  sizing (R-B2, MemGPT flush precedent). The 90d access-expiry prior
  art (config schema memory.expiration) already accepts automated
  deletion as a concept.
- **Rollback:** set `memory.tiers.enabled = false` (one config key, no
  data migration - evicted rows live in episodic recall storage, never
  silently dropped; working-tier flushes are lossless). No restart
  ordering hazards: the engine is constructed after its deps and
  stops cleanly on cancel.
- **Decision owner:** meept maintainers (product). Status: PROPOSED,
  2026-10-31, tree 02 leaf 02.

## As-needed tool exposure is opt-in (default OFF)

`[agent.tools] dynamic_tool_exposure` (token-efficiency branch 03) is FALSE
by default. When off, tool definitions are byte-identical legacy
(`full`/`indexed` only) - the as-needed predicted/search exposure engine
(`tool_exposure` knob, `internal/agent/tool_exposure.go`) is entirely inert.

- **What flips:** `dynamic_tool_exposure = true` enables the predicted
  (default) / search exposure modes. The mode knob `tool_exposure`
  (full | indexed | predicted | search) and `predicted_budget` (N, default
  25) only take effect while the flag is on; with it off they silently
  downgrade to indexed legacy (one warn).
- **Capability gate (hard rule):** `search` mode is additionally gated on
  the active model being frontier-class (the `"frontier"` capability tag,
  `llm.CapFrontier`). A non-frontier active model is downgraded to
  `predicted` with ONE warn line naming the model; unknown models are
  treated as non-frontier (fail-safe).
- **The 5.3 warning (verbatim, required in the config template, this doc,
  and the AGENT.md frontmatter doc):** search mode requires a
  frontier-class model. Do NOT enable it for small/local models - they fail
  to use the search/view round-trip reliably (live-smoke evidence:
  always-full promotions 2026-09-07/09-12,
  `internal/config/schema.go:1959-1971`). The daemon downgrades search to
  predicted automatically for non-frontier models.
- **Rollback:** set `dynamic_tool_exposure = false` - one config key,
  no data migration, behavior returns to byte-identical legacy.

User-facing how-to: `docs/workflows/dynamic-tools.md`. Design contract:
`docs/research/dynamic-tools-design.md`.
