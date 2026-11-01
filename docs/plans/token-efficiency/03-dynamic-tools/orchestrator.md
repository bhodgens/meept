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
default `predicted`. Small/local models get D automatically; B is opt-in per
model tier. Evidence for the tier split: the always-full promotions
(transcript_fetch 2026-09-07, json_extract 2026-09-12, schema.go:1959-1969)
are live-smoke proof that small models do not reliably pay a two-step
tool_view round-trip - so B must never be the default.

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
| 01.. | (authored after 00D) | - | 00D | - | GATED |

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
| (design-gated features) | authored with leaves 01+ | TBD post-00D | GATED |

## Coding Conventions

Inherited from ../master.md. Branch-specific: measurement scripts live in
scratch (/tmp) or scripts/ (if committed, they follow scripts/ audit-script
conventions); no daemon behavior changes from leaf 00A.

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00A-token-audit | PENDING | - | dispatchable now |
| 00D-design-decision | GATED | - | needs 00A + Q1/Q2 |

## Integration Test Plan

Post-00D decomposition. Minimum for any candidate: cold-start token count
(before/after, measured with the 00A script), cache-hit continuity across 3
consecutive turns (log assertion: no "prefix cache invalidated" on turns 2-3
of the same episode), and a task-completion regression (an existing e2e chat
suite that exercises a non-core tool must still pass - starvation canary).

## Review Checklist

- [ ] 00A numbers reproducible (script committed or saved in report)
- [ ] Design doc names the cache-stability mechanism explicitly
- [ ] Starvation recovery path exists in the ratified design
- [ ] No implementation dispatched before Q1/Q2 recorded
