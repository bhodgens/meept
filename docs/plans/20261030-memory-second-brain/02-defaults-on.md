# Defaults-On Flip - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The parent gate tests (00T) ALREADY
> EXIST - your job includes making the memory-defaults suite GREEN. Do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use read_file
> on existing source files. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Flip every memory subsystem default to ON in code defaults + shipped config template; update the two governance docs in the same commits.
- **Dependencies:** 00T-e2e-memory-pipeline.md (TESTS_VALIDATED)
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Audit references:** F1 of master Test Gate; R1, R2

## Goal

A stock install records every turn (L0), distills claims (L1) and scenes
(L2), extracts ambient claims, votes usefulness, indexes knowledge, and
generates bridges - all on by default, per user decisions D1-D3 (frozen in
../master.md). The template config mirrors the code defaults with drift
annotations, and the governance docs record the authorization.

## Context

- internal/config/schema.go holds defaults via struct zero values + gendoc
  comments (1511-1655 region). Flipping means explicit defaults: either
  default-true handling in the config loader or inverted field semantics.
  PREFER: keep field semantics (enabled=false means off) and set explicit
  default values in the loader/applyDefaults path - grep for the existing
  defaults application site (config.Load / applyDefaults) rather than
  changing field types.
- config/meept.json5 is the shipped template; scripts/config-drift-check.py
  classifies template-vs-installed drift via `// drift:` annotations.
- The defaults must flip ATOMICALLY as one product decision: one commit for
  code defaults, one for template + docs (concern-split, same wave).
- models.json5 memory_model slot: distill/extraction chatter resolves there;
  default-on daemons WITHOUT a memory_model fall back to the general
  provider - verify the fallback resolves on a models-less host by SKIPPING
  distill jobs with a Warn (not error loops) when no model is available.

## Verified Anchors

- internal/config/schema.go:1511-1655 - all memory config structs (L0Enabled,
  Usefulness, Bridges, Knowledge, AmbientExtraction).
- internal/config/schema.go:1534-1535 - `//gendoc:example [memory]
  l0_enabled = true` already exists - keep gendoc accurate after the flip.
- config/meept.json5 - template to mirror (grep for "memory" block).
- docs/workflows/memory.md - behavior doc; every flipped knob's section says
  "default false/opt-in" today - update each.
- docs/agents/opt-in-defaults.md - governance doc listing default-false
  knobs; add the D1-D3 authorization record.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C4 (verbatim from ../master.md):
// memory.l0_enabled = true
// memory.usefulness.enabled = true
// memory.epistemic.ambient_extraction.enabled = true
// memory.knowledge.enabled = true
// memory.bridges.enabled = true
```

### What This Leaf Consumes

- C4 from ../master.md.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Code defaults flip in the loader defaults path | Modify: internal/config/ (defaults application site) | `go test -p 2 ./internal/config/` and the memory-defaults e2e suite | default boot has all knobs false | zero-config load yields all five knobs true | Given no memory block in config; When loaded; Then all five enabled |
| T2 | Template config mirror + drift annotations | Modify: config/meept.json5 | `python3 scripts/config-drift-check.py` (report-only) | template shows false/absent | template sets all five true; installed-home drift annotated `// drift:` | Given the template; When drift check runs; Then no UNCLASSIFIED drift |
| T3 | Docs update in the same wave | Modify: docs/workflows/memory.md, docs/agents/opt-in-defaults.md | grep review (orchestrator in-session) | docs say default-off | every flipped knob's doc says default-on with D1-D3 citation | Given each knob section; When read; Then default-on + authorization recorded |
| T4 | Models-less host degradation pin | Modify: the L1 handler site (internal/memory/distill_async.go or its handler); Test: gate suite T1 already covers boot; add queue-stats assertion to the suite? NO - suite edits belong to gate leaf; instead verify via existing suite + manual scratch boot | `go test -p 2 ./e2e/suites/memory-defaults/` (GREEN) + scratch-boot log check | daemon error-loops or F-D8 kill on models-less default boot | Warn once per cycle, jobs stay queued (retry), no error spam | Given default boot with no memory_model resolvable; When L1 job runs; Then Warn + retry/backoff, turn path unaffected |

## Self-Verification Checklist

- [ ] All five knobs true on zero-config load (print the loaded config in a
      scratch-boot check)
- [ ] memory-defaults e2e suite GREEN (T1 assertions incl. knowledge tool
      registration)
- [ ] Template + docs updated; drift check clean
- [ ] No knob semantics changed - only defaults
- [ ] gofmt clean; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output:** [defaults print + suite output, last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Exactly C4's five knobs flipped - nothing else silently flipped
- [ ] Fallback semantics for models-less hosts: Warn + queue retry, never
      turn-path failure (quota-reset-resilience spirit)
- [ ] Docs + template in the same wave; D1-D3 authorization cited
- [ ] gendoc comments still accurate (`make docs-generate`/`-check` if the
      gendoc lines changed)
- [ ] No debug artifacts

Output: APPROVED or specific gaps.

## Notes

- The F-D8 classifier gate is UNRELATED to memory defaults - do not touch it.
  The gate suite already boots with classifier_boot_fail_fast=false on
  models-less scratch hosts.
- Useful knobs NOT flipped (still opt-in, deliberate): none - D3 is "all".
  If a knob proves unsafe at review time (e.g. knowledge code-indexing on
  huge repos), STOP and escalate to the orchestrator instead of descoping
  silently.
- Bridge knobs: bridges default-on per D1; leaf 07 owns the flip-back.
