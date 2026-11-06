# Config default flips + builtin tools + skills + agent-groups audit

- Scope: commit range 0a446a21..HEAD in /Users/caimlas/git/meept
- Commits in scope: 5da890a1 (memory default-ON flips + BaselineTools + template mirrors), 2876a4a5 (codemode builtin tool + codemode_enabled gate), a8b4b374 (tool_search builtin), 47718539 + c0db3546 + f2effd15 + 7217b0b8 (skills origin/refresh seam), agent-groups commits (groups.Resolved, resolveForAgent)
- HEAD: 6b42213b
- Captured: 2026-11-06T18:42:01Z
- Method: read-only; git show/diff of committed state; ignore dirty tree
- Already closed (do not re-report): .hermes/audits/2026-10-30-bughunt-wave.md, PB-1/PB-2 (fixed in 6b42213b)

## Findings

## F-1 [MEDIUM] tool_search searches the FULL shared registry — the documented ceiling re-bind (SetScope) was never wired
- File: internal/daemon/components.go:8055 (HEAD 6b42213b); internal/tools/builtin/tool_search.go:64-90
- Evidence: `registry.Register(builtin.NewToolSearchTool(registry))` binds the meta-tool to the daemon-wide registry. Commit a8b4b374 states "leaf 05 can re-bind to a ceiling-filtered view" via `SetScope`, but `git grep` at HEAD finds **zero non-test callers** of `ToolSearchTool.SetScope` (only `SetScopeUserID`/`ReflectionEngine.SetScopeProvider` hits — unrelated seams). tool_search is in `config.DefaultAlwaysFullTools()` (schema.go:2379+), so under predicted/search exposure modes every agent that has the tool can search names+descriptions of tools outside its own role ceiling; `tool_view` (registered the same way against the full registry, components.go:8018 `NewToolViewTool(registry, 0)`) can then expand their full schemas.
- Why it's a bug: the tool's own doc comment promises the scope is "the agent's role ceiling ... never the filtered active subset" — but the daemon never narrows it to the ceiling either. The discovery contract (design §3.2 / §6) is unenforced: cross-role tool names, descriptions, and parameter schemas leak into any agent's context. Execution is still allow-list-blocked, so this is an information-disclosure / prompt-injection-surface bug, not a privilege escalation.
- Fix shape: wire SetScope at loop creation (agent.Registry.createLoop already has the per-agent filtered registry — hand it to the tool instance, or construct a per-agent ToolSearchTool view) when dynamic exposure is on; or bind scope per-call via the tool-registry context seam that codemode already uses.

## F-2 [MEDIUM] tierpolicy eviction planner hardcodes usefulness config — template knobs not honored and `usefulness.enabled=false` is ignored on that path
- File: internal/memory/tierpolicy/engine.go:86-87, 486-490
- Evidence: `planEvict` builds `memory.UsefulEvictionConfig{Enabled: true, FloorPct: floorPct, Weights: memory.DefaultWeights()}` from package constants (`floorPct = 0.05`) — no `config.MemoryUsefulnessConfig` reaches the engine. Meanwhile config/meept.json5 (5da890a1) advertises `memory.usefulness.{enabled,floor_pct,base,wv,wa,ws}` as user-tunable ("set false to restore age-only ordering"), and the consolidation path honors them via `ResolveUsefulEviction` (internal/memory/usefulness.go:305).
- Why it's a bug: template-vs-code default divergence in behavior, not just value: a user who opts OUT of usefulness eviction (`enabled=false`) still gets usefulness-driven eviction on the tier-policy path once `[memory.tiers]` is enabled, and any custom floor_pct/weights are silently dropped there. The engine's header comment even cites the opt-in rule ("every knob ships default-off") while hardcoding `Enabled: true` for one knob.
- Fix shape: thread the resolved `ResolveUsefulEviction(cfg.Memory.Usefulness)` through `tierpolicy.Deps`/`TiersConfig` and gate planEvict on `Enabled`, instead of literals. (Mitigating: `[memory.tiers]` itself is default-off, so blast radius is opted-in installs only.)

## F-3 [LOW] BaselineTools grants knowledge_* unconditionally while tool registration is conditional on `memory.knowledge.enabled` — scope-capped agents lose real tool slots when knowledge is off
- File: internal/agent/spec.go:200-224 (5da890a1); template config/meept.json5:279-282 ("set false to opt out (no knowledge_* tools registered...)")
- Evidence: `knowledge_search/read/impact` are now in `BaselineTools`, so every agent's `GrantedToolNames()`/`ScopedToolNames(limit)` include them. With `ToolScopeLimit > 0`, `ScopedToolNames` (spec.go:311) counts baseline names against the cap; on a knowledge-disabled install those three names never resolve to registered tools, and cap ordering (AdditionalTools first, then BaselineTools) means the 3 dead names displace up to 3 real tail tools (e.g. project_info, delegate_task) from a capped agent's grant. `HasTool("knowledge_search")` also reports true on knowledge-disabled installs.
- Why it's a bug: a config opt-out silently changes the effective tool set of agents whose spec was capped against the pre-flip baseline. The D3/C4 rationale ("the store registers its tools on every daemon") only holds when knowledge is enabled; the grant assumes it always is.
- Fix shape: filter baseline grants through registry presence at filter time (the filtered registry already drops unknown names for Get — apply the same drop before the cap is consumed), or document that BaselineTools entries must always be registered.

## F-4 [LOW / observation] skills origin metadata is write-only — no no-clobber consumer of `OriginUser` exists yet
- File: internal/skills/lifecycle/writer.go:248 (WriteSkillWithOrigin), evolver.go:815 (applyProposal), skill_patch_tool.go:147, skill_create_tool.go:144
- Evidence: c0db3546 stamps `origin:` on every write path (user/agent/evolver), but `git grep` for read consumers of `.Origin`/`OriginUser` at HEAD finds only the model field itself, the memory_facts wiring (unrelated), and `PreImage`. Neither `Writer.WriteSkillWithOrigin`, the evolver funnel, nor skill_patch_tool consults the existing file's origin before overwriting; the evolver will overwrite a user-edited (`origin: user`) SKILL.md with an evolver-stamped candidate (mitigated only by the verifier gate and the best-effort version snapshot at writer.go:265, whose failure is logged and skipped — "Continue with write").
- Why it matters: the C2/#104 premise is provenance-based protection; today provenance is recorded but never enforced. Not a regression (pre-origin behavior was identical) — it is the missing half of the feature, so refresh/evolver can still wipe user edits.
- Fix shape: in the evolver funnel and agent write paths, refuse or downgrade-to-`.new`-sidecar when the on-disk frontmatter declares `origin: user`, until an explicit re-authorization exists.

## Audited-clean
- **Opt-in trio (AGENTS.md / docs/agents/opt-in-defaults.md):** `skills.state` explicitly `Enabled: false` in DefaultConfig (schema.go ~4533); `plans.parallel_phases` and `plans.plan_compiler_enabled` unset in DefaultConfig (Go zero = false) and absent from the plans section of config/meept.json5 — none was flipped in 0a446a21..HEAD.
- **Memory default-ON flips (5da890a1):** template and code agree — `l0_enabled`, `usefulness.enabled`, `epistemic.ambient_extraction.enabled`, `knowledge.enabled`, `bridges.enabled` are true in both `DefaultConfig` (schema.go 4156-4259) and config/meept.json5 (251-338), each with drift annotations; `conflict.enabled` stayed false in both (correctly excluded from D3). Zero-value sub-knobs resolve via `ResolveUsefulEviction`/spec defaults as documented. (F-2 above is the one consumer that bypasses this.)
- **codemode gate (2876a4a5):** `codemode_enabled` default-false (schema.go:2290-2296), absent from the template = byte-identical legacy as claimed; registry.go:954 union applies only when the flag is on AND the agent is scoped, and it runs BEFORE the T2 group-intersection (registry.go:960-990), so a managed group's deny/allow set still strips codemode when the group does not allow it — groups never widen (D4) intact. Unscoped agents receive it via the unfiltered registry, which is the documented opt-in semantics.
- **tool_search registration (a8b4b374):** registered on every daemon; harmless for scoped agents (absent from every baseline/additional grant, filtered out of their registries). The ceiling-bypass defect is F-1, not the registration itself.
- **skills refresh seam (47718539):** C1 is an in-memory re-scan (`SkillIndex.Refresh()` at turn boundary, warn-not-fail); it writes no files, so it cannot clobber user edits. Install-side no-clobber (scripts/install-sync.py `.new` flow) is untouched in range.
- **agent-groups (groups.Resolved / resolveForAgent):** intersection math (internal/groups/groups.go:39-87) ranks unknown scopes narrowest and unknown network classes most-restrictive, fail-safe defaults (invalid ceiling never narrows, "" ceiling never widens); D8 deny-all uses the explicit marker instead of the legacy empty=all fallthrough (registry.go:932-934, 984-986). 769e8339 fixed live-date-rot in the override test only — no prod-code issue found.
- **config template mirrors:** meept.json5 `agent.tools` section matches code defaults (`dynamic_tool_exposure: false`, `tool_exposure: "predicted"` with flag-off downgrade-to-indexed verified in tool_exposure.go:46-90, `predicted_budget: 25`).

## Wave closeout (parent, 2026-11-06)
- F-2 CLOSED: tierpolicy.Config.Usefulness plumbed from
  ResolveUsefulEviction(mmgr.Config().Usefulness) in BuildTierPolicyEngine;
  planEvict honors it (Enabled=false -> age-only pool; zero-value ->
  documented defaults byte-identically). Pinned by
  TestPlanEvictHonorsUsefulnessConfig (ranked-lane E-first vs age-only
  A-first discrimination + zero-value legacy check). tierpolicy + daemon ok.
- F-1 OPEN (tool_search ceiling re-bind never wired).
- F-3 OPEN (BaselineTools knowledge_* dead names under scope caps).
- F-4 OPEN (skills origin metadata write-only).

- F-1 CLOSED: per-call ctx seam, not SetScope. tool_search Execute now
  prefers agent.ToolRegistryFromContext(ctx) (the executor injects the
  agent's FILTERED registry per call at executor.go ContextWithToolRegistry
  — same seam codemode uses); tool_view Execute refuses an out-of-ceiling
  name BEFORE the shared LRU cache via scope.Get(name)==nil. A shared-
  singleton SetScope would race across concurrent agents; the ctx value is
  per-call and byte-identical for standalone callers (tests, direct use).
  Pins: internal/tools/builtin/tool_ceiling_seam_test.go (mutation-proven
  RED for both halves). Registry-side companion (agent-core F-2 corollary):
  filterTools unions the meta pair into scoped agents' allow-lists when
  [agent.tools].dynamic_tool_exposure is on, so every agent keeps the
  recovery path under the row-4 hard gate.
