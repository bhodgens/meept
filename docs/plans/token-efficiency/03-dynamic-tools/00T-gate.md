# Branch 03 Test Gate - Dynamic Tool Exposure e2e Skeleton - Test Leaf

> **For the implementing agent:** Write TESTS ONLY, no implementation.
> RED for missing-implementation reasons against CURRENT exported
> surfaces. Do NOT commit. Write once, never read back. Explore with
> search_files/terminal, not read_file.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** hermetic e2e skeleton for the ratified single-knob
  `tool_exposure` design (00D, docs/research/dynamic-tools-design.md)
- **Dependencies:** 00A + 00D COMPLETE
- **Estimated Context:** 60K
- **Concurrency Group:** solo (gate)

## Goal

Pin the post-implementation behavior of the as-needed tool-exposure
mechanism, RED today, green when the 01..07 leaves land:

1. **Flag-off equivalence:** with `[agent.tools] dynamic_tool_exposure =
   false` (the default), tool definitions are byte-identical legacy
   (full/indexed enumeration only). Assert the exact legacy payload in a
   hermetic scripted case.
2. **Capability-gate cases (orchestrator Integration Test Plan 1-4):**
   - (1) `tool_exposure = "search"` configured + NON-frontier active
     model -> effective mode `predicted` + exactly one warn log line
     naming the model; repeats not re-logged.
   - (2) mid-session frontier->lesser model switch with `search`
     configured -> effective mode reverts to `predicted` ON THE SWITCH.
   - (3) lesser->frontier switch back -> `search` restored only at the
     NEXT episode boundary, not mid-episode (no flapping).
   - (4) unknown/unclassified model -> treated as NON-frontier.
   Frontier signal: `capabilities` array carries the tag `"frontier"`
   (00D §5.1: `llm.CapFrontier`, `ModelConfig.HasCapability`).
3. **Starvation canary:** an e2e chat case that needs a NON-core tool
   (one not in `DefaultAlwaysFullTools`) still completes by recovering
   via `tool_search` -> `tool_view` promotion. The meta-tool pair is
   always present and always-full in dynamic modes.
4. **Cache continuity:** three consecutive turns of one episode emit no
   "prefix cache invalidated" line in the daemon log (assert the ABSENCE
   of the debug line, not the presence of hits - provider-agnostic).
5. **Episode-stability:** mid-episode the active tool list is frozen
   except append-only promotions; a promotion splices into its
   alphabetical position and produces exactly ONE prefix-change event
   (00D §4.3).

## Context

- Design: docs/research/dynamic-tools-design.md (contracts §3, cache
  stability §4, gate §5, starvation §6, config §7, leaf decomposition
  §8). This gate tests the design, not any one leaf.
- E2E policy: repo AGENTS.md - hermetic, `e2e` build tag, no live LLM:
  scripted FakeLLM turns + daemon-log assertions. The capability-gate
  cases need a model config fixture: two model entries in a hermetic
  models.json5 (one with the `frontier` capability tag, one without) +
  one unknown entry.
- Manifest: `e2e/manifest.json` gains the `dynamic-tools` suite row
  (status implemented for the gate skeleton cases you write; the
  starvation + cache-continuity cases may be marked `todo` if they need
  the impl leaves' seams - record which). path_map: add
  `dynamic-tools` to the suite lists of the packages the design's
  leaves touch (`internal/agent/`, `internal/llm/`, `internal/config/`,
  `internal/tools/` - check how sibling path_map entries are shaped
  first; do NOT rewrite sibling WIP rows already in the working tree).

## Verified Anchors

- effective-mode seam pattern: internal/llm/schema_mode.go:36
  (EffectiveSchemaMode; the gate asserts the ANALOG for exposure, so
  compile RED is expected only if the suite references 00D's NEW
  symbols - PREFER black-box config+log assertions that compile today)
- cache invalidation line: internal/agent/loop.go:4461 area
  ("prefix cache invalidated")
- intent label source: internal/agent/intent_analyzer.go:372-377
- DefaultAlwaysFullTools: internal/config/schema.go:1956-1973
- tool_view always-full force-inclusion: internal/tools/registry.go:234

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/dynamic-tools/dynamic_tools_test.go  (new, e2e tag)
  TestDynamicToolsFlagOffByteIdentical
  TestCapabilityGate_SearchDowngradedForNonFrontier   // case 1
  TestCapabilityGate_MidSessionSwitchRevertsToD       // case 2
  TestCapabilityGate_RestoreOnlyAtEpisodeBoundary     // case 3
  TestCapabilityGate_UnknownModelIsNonFrontier        // case 4
  TestStarvationCanary_SearchViewPromotion            // todo-ok
  TestCacheContinuity_NoInvalidationWithinEpisode     // todo-ok
e2e/manifest.json (+suite row + path_map entries)
```

### What This Leaf Consumes

- 00D design (frozen contracts); 00A numbers (budget N=25 default).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Suite skeleton: dir + manifest row + path_map | e2e/suites/dynamic-tools/, e2e/manifest.json | `make e2e-fast-area AREA=dynamic-tools` (or the go test e2e equivalent) | flag/mode/config keys absent | config keys accepted; legacy payload asserted |
| T2 | Flag-off byte-identical case | dynamic_tools_test.go | same | tool_exposure values unknown to current config validation | payload equals legacy full/indexed |
| T3 | Capability-gate cases 1-4 | same | same | no frontier tag / mode downgrade exists | warn line + effective-mode behavior |
| T4 | Starvation + cache-continuity cases (todo-marked if impl seams missing) | same | `make e2e-fast` unaffected | n/a | canaries runnable |

Self-verification:

- [ ] Every case is RED for a missing-implementation reason, not a
      compile error (suite compiles: `go test -p 2 -tags e2e -run
      XXX ./e2e/suites/dynamic-tools/` builds)
- [ ] Manifest JSON valid; path_map entries additive-only (sibling WIP
      rows in the working tree left byte-identical)
- [ ] No implementation code anywhere in internal/ (tests only)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. RED output tails per case; 3. which cases are
   implemented vs todo-marked and why; 4. manifest diff (additive
   proof).

## Review Checklist (For Review Agent)

- [ ] Cases compile against CURRENT code (no new-symbol references)
- [ ] Case 1 asserts exactly-one warn (repeat not re-logged)
- [ ] Case 3 asserts restore is deferred to the boundary (the flap
      regression this branch must never ship)
- [ ] Starvation canary uses a non-core tool; meta-pair present
- [ ] Manifest additive-only (no sibling rows touched)
