# Branch 03 - As-Needed Tool Definitions - Orchestrator (DESIGN-GATED)

> **For the executing agent:** You are the orchestrator. This branch is
> DESIGN-GATED: only the audit leaf (00A) and the design leaf (00D) may run
> until the user answers Open Questions Q1/Q2 in ../master.md. Do NOT author
> or dispatch implementation leaves before the answers are recorded here.

## Meta

- **Parent:** ../master.md
- **Gap:** GAP-3: every turn ships ~75 stub definitions (loop.go:4437) even in
  indexed mode; no task-driven subset exists.
- **Status gate:** Q1/Q2 ANSWERED 2026-10-31 (see ../master.md Open
  Questions). Ratified shape: hybrid D with deterministic intent map +
  model-tier `tool_exposure` config. Leaf 00A (audit) still runs FIRST - it
  sizes the savings and validates the budget numbers the design pins. 00D is
  unblocked once 00A returns.

## Ratified Shape (user decision 2026-10-31)

**Single-knob design.** All modes are one mechanism - `tool_exposure` with an
up-front full-schema budget - differing only in how many tools get full
schemas before the model asks:

| Budget | Mode | Behavior |
|--------|------|----------|
| all | `full` / `indexed` | today's behavior (all tools enumerated; full or stub schemas) |
| N (predicted) | `predicted` (DEFAULT, shape D) | intent-map-selected N tools + always-full core get FULL schemas; rest unlisted; tool_search/tool_view recover misses |
| 0 | `search` (shape B) | category digest (~10 lines) + tool_search/tool_view only; opt-in for frontier-class models |

**Prediction origin (three layers, no new LLM call):**
1. CEILING (security): the agent's role list (AGENT.md `additional_tools` ->
   FilteredToolRegistry). The predicted subset NEVER exceeds it.
2. SELECTION (episode start): deterministic intent->toolset map (config file,
   auditable) keyed by the intent label the EXISTING intent analyzer already
   produces (internal/agent/intent_analyzer.go:257). Not the planner/
   orchestrator LLM: the planner only runs for dispatched multi-step tasks
   (chat turns would get no prediction), and an LLM tool-picker adds latency
   + tokens to the path being optimized. Classifier tool-head is the future
   upgrade path behind the same seam if the map proves coarse.
3. PROMOTION (mid-episode, model-driven): tool_search finds a missing tool in
   the FULL registry (search is never limited to the active set); tool_view
   fetches the schema AND activates the tool - append to the active list.

**Cache-stability mechanics (binds all leaves):**
- Prediction computed ONCE at episode start; the active list is frozen for
  the episode except append-only promotions (StabilizeToolPrefix keeps the
  existing prefix byte-identical when tools are appended, loop.go:4442-4448).
- Shrinking happens only at the next episode boundary.
- tool_search + tool_view are always-full (a stubbed discoverer is
  undiscoverable) - already true for tool_view (schema.go:1958).
- A mid-session model switch may change `tool_exposure` (per-model override);
  the resulting one-time list churn is acceptable - a model switch already
  invalidates the cache prefix.

**Config resolution order** (mirrors the existing schema_mode seam,
EffectiveSchemaMode at loop.go:1434-1437): per-model > per-agent (AGENT.md
frontmatter `tool_exposure`) > global `[agent.tools] tool_exposure` >
default `predicted`. Per-agent `tool_exposure: "search"` (B) is a supported
setting - but see the capability gate below; config alone does not enable B.

**Capability gate on `search` (B) - HARD RULE (user decision 2026-10-31):**
`tool_exposure: search` takes effect ONLY when the ACTIVE model is
frontier-class. Resolution is: compute the configured mode, then if mode ==
`search` and the active model is NOT frontier-class, silently downgrade to
`predicted` (D) and log one warn line naming the model and the downgrade.
Consequences that MUST hold:
- A mid-session switch to a lesser model reverts the effective mode to D on
  the switch - even when the agent config says `search` and even mid-episode
  (this is the one allowed non-append-only list change; a model switch
  already invalidates the cache prefix, so the churn is free).
- Switching BACK to a frontier model restores `search` at the next episode
  boundary (not mid-episode - avoids flapping).
- The tier signal comes from models.json5 capability metadata (00D pins the
  exact key; likely a `frontier: true` tag or a capability-class field on the
  model entry). Unknown/unclassified models are treated as NON-frontier
  (fail-safe: defaults to D).
- Docs requirement (binds leaf 02-equivalent docs tasks): config template
  comment, docs/workflows page, and AGENT.md frontmatter docs ALL state
  plainly: "search mode requires a frontier-class model. Do NOT enable it for
  small/local models - they fail to use the search/view round-trip reliably
  (live-smoke evidence: always-full promotions 2026-09-07/09-12). The daemon
  downgrades search to predicted automatically for non-frontier models."

Evidence for the tier split: the always-full promotions
(transcript_fetch 2026-09-07, json_extract 2026-09-12, schema.go:1959-1969)
are live-smoke proof that small models do not reliably pay a two-step
tool_view round-trip - so B must never be the default AND never survive a
downgrade to a small model.

## Current State (verified)

- Indexed mode (default): all tools -> one-line stubs except
  DefaultAlwaysFullTools (10 names, internal/config/schema.go:1954-1971).
- Per-agent static lists already exist: AGENT.md `additional_tools` ->
  FilteredToolRegistry (executor.go:1927). This is STATIC per role, not per task.
- tool_view is the existing meta-tool for schema fetch (Hermes-style two-step).
- Intent analyzer (classifier alias) runs per turn already
  (internal/agent/intent_analyzer.go:257) - a possible prediction hook.
- Cache stability: StabilizeToolPrefix (loop.go:4442) assumes a mostly-static
  tool list; ANY per-turn tool-list churn invalidates the prompt cache prefix.
  This is the central design tension of this branch.

## Candidate Shapes (for the design leaf to cost out)

A. **Tune indexed stubs (baseline):** keep enumeration; trim descriptions to
   one line; measure. Zero cache risk. Savings: stub bytes only (~75 x ~30 tokens).
B. **Search-not-enumerate:** replace the stub list with 2 meta-tools
   (tool_search + existing tool_view). Model discovers tools on demand.
   Biggest savings; cost = extra round-trips on tasks needing uncommon tools;
   risk = model doesn't know what exists (mitigated by keeping category
   digest ~10 lines instead of 75 stubs).
C. **Orchestrator-predicted subset:** classifier/intent or deterministic
   intent->toolset map picks N tools per EPISODE (not per turn); full schemas
   for predicted + always-full core; episode-boundary swap only (cache-safe:
   prefix changes once per episode, and session-affinity erase already
   tolerates episode boundaries). Risk = silent tool starvation; needs
   recovery (meta-tool fallback = B as safety net).
D. **Hybrid C+B (expected winner):** predicted subset with full schemas +
   tool_search/tool_view fallback for misses.

## Child Index

| # | Child | Type | Depends on | Est. context | Concurrency |
|---|-------|------|-----------|--------------|-------------|
| 00A | 00A-token-audit.md | measurement leaf | none | 55K | solo, dispatchable NOW |
| 00D | 00D-design-decision.md | design leaf | 00A + Q1/Q2 answers | 50K | GATED |
| 00T | 00T-gate.md (AUTHORED 2026-11-01) | test gate leaf | 00D | 60K | solo, FIRST after 00D |
| 01.. | (00D §8 decomposition; leaf docs to be authored before dispatch) | - | 00T | - | GATED |

## Dispatch Protocol

1. Dispatch 00A now (read-only + scratch script; produces measured numbers).
2. Q1/Q2 are ANSWERED (Ratified Shape above). 00A numbers feed 00D's budget
   sizing only - no further user gate before 00D.
3. Dispatch 00D to write docs/research/dynamic-tools-design.md implementing the
   ratified shape (contracts, cache-stability analysis, starvation recovery,
   intent-map file format, tool_exposure config resolution).
4. Orchestrator decomposes 00D's implementation section into leaves 01+ and
   updates this Child Index + ../master.md tracking BEFORE dispatching any.

## Interface Contracts

Pinned after 00D. Placeholder constraint that binds ALL candidates:
- **Episode-stability rule:** the tool-definition list may change only at
  episode boundaries (new conversation/turn-set), never mid-episode, unless
  the change is additive AND StabilizeToolPrefix keeps the existing prefix
  byte-identical (append-only growth preserves the cache prefix).
- Any new meta-tool must be in DefaultAlwaysFullTools (a stubbed discoverer
  is undiscoverable).

## Test Gate

| Feature | Gate leaf | Test target | Status |
|---------|-----------|-------------|--------|
| token audit numbers | 00A | n/a (measurement, no code) | PENDING |
| tool_exposure config + capability gate + starvation + cache-continuity | 00T-gate.md | e2e/suites/dynamic-tools/ + manifest path_map | PENDING |

## Coding Conventions

Inherited from ../master.md. Branch-specific: measurement scripts live in
scratch (/tmp) or scripts/ (if committed, they follow scripts/ audit-script
conventions); no daemon behavior changes from leaf 00A.

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00A-token-audit | COMPLETE | - | report archived to docs/research/token-efficiency-2026-10/meept-token-audit.md; key numbers: bare-turn indexed 9,314 est-tok (0.51x pi 18.2K anchor); tool defs indexed 7,800 vs full 13,421; shape B saves 5,685, C(N=10) saves 4,222, D(N=25) saves 1,110; D break-even N=30->31. Co-occurrence evidence skipped (no in-repo telemetry) - 00D flags it |
| 00D-design-decision | COMPLETE | 52d6f9dd | docs/research/dynamic-tools-design.md - all 7 sections + 7-leaf decomposition; frontier key pinned (models.json5 "frontier" capability tag, llm.CapFrontier const); default N=25 from 00A break-even; flag-off = byte-identical legacy, flag-on default = predicted |
| 00T-gate | TESTS_VALIDATED | 3a88f52a | hermetic e2e skeleton e2e/suites/dynamic-tools/ (capability-gate cases 1-4 RED on missing seam; flag-off GREEN control; starvation + cache-continuity t.Skip TODOs). In-session fixes: helpers_test.go:460 append-no-values vet + gofmt :494. Manifest rows swept into HEAD by sibling e7a966d4 |
| 01-config-surface | COMPLETE | 5dc8ba0f (+c24db7d9) | AgentToolsConfig DynamicToolExposure/ToolExposure/PredictedBudget + NormalizeAgentToolsDefaults clamp (default 25, cap 30) + Validate() downgrade-on-flag-off; CapFrontier const; config/meept.json5 [agent.tools] block w/ 5.3 frontier warning. Sibling c24db7d9 swept schema.go+meept.json5 hunks (same defect as 9f8a974c); provider_registry.go committed 5dc8ba0f. flag-off control PASSES; 4 cap-gate cases RED-right-reason |
| 03-intent-map | COMPLETE | fe04b160 | internal/agent/intent_tool_map.go (IntentMap, LoadIntentMap, For, SetUniverse seam) + config/tools_intent_map.json5 v1 template + Makefile CONFIG_FILES + config-drift-check optional-templates. Unwired by design until leaf 05. |
| 04-tool-search | COMPLETE | a8b4b374 (+c24db7d9) | internal/tools/builtin/tool_search.go (NewToolSearchTool(scope agent.ToolRegistry) + SetScope seam): case-insensitive name+desc search over full registry, deterministic order, ~168-tok result. components.go registration hunk swept into sibling c24db7d9; tool_search.go committed a8b4b374. |
| 02-model-tier | COMPLETE | 1c8ff3e2 | ModelDef/ModelConfig ToolExposure pass-through + AgentSpec frontmatter field; "frontier" capability tag on agnes-3.0-flash + kimi-k2.6 in config/models.json5 (+tool_exposure="search" example); lfm-* untagged (fail-safe holds). Plumbing for leaf 05: AGENT.md frontmatter metadata lives in agents.AgentMetadata (internal/agents/models.go) + mergeSpec/definitionToSpec (internal/agent/registry.go) - AgentSpec.ToolExposure only reachable once 05 wires it |
| 05-exposure-engine | COMPLETE | f8c862e9 | internal/agent/tool_exposure.go (NEW: ToolExposureMode enum, IsFrontierClass, EffectiveExposureMode, ExposurePlan + Promote) + loop.go applyExposureGate warn-once downgrade in ApplyRequestModel (design 5.2.1) + exposureDowngradeWarned map + ConfigSnapshot propagates [agent.tools] config to per-session clones + registry.go createLoop stamps schemaModeCfg onto registry-built loops (the dispatcher-path gap both subagents missed). tool_view.go SetActivateHook seam + fireActivate on both success paths (mutexio-safe). Gate evidence: 4 capability-gate cases GREEN + flag-off byte-identical PASS; starvation + cache-continuity stay SKIP (leaves 06/07) |

## Integration Test Plan

Post-00D decomposition. Minimum for any candidate: cold-start token count
(before/after, measured with the 00A script), cache-hit continuity across 3
consecutive turns (log assertion: no "prefix cache invalidated" on turns 2-3
of the same episode), a task-completion regression (an existing e2e chat
suite that exercises a non-core tool must still pass - starvation canary),
and the capability-gate cases: (1) `search` configured + non-frontier active
model -> effective mode D + warn log line; (2) mid-session switch
frontier->lesser with `search` configured -> effective mode reverts to D on
the switch; (3) switch back lesser->frontier -> `search` restored at the next
episode boundary, not mid-episode; (4) unknown model treated as non-frontier.

## Review Checklist

- [ ] 00A numbers reproducible (script committed or saved in report)
- [ ] Design doc names the cache-stability mechanism explicitly
- [ ] Design doc pins the frontier tier signal (exact models.json5 key) + fail-safe for unknown models
- [ ] Capability gate honored: mid-session switch to a lesser model reverts to D; restore only at episode boundary
- [ ] Docs (config template + workflows + AGENT.md frontmatter) carry the "NOT for small/low-end models" warning verbatim
- [ ] Starvation recovery path exists in the ratified design
- [ ] No implementation dispatched before Q1/Q2 recorded
