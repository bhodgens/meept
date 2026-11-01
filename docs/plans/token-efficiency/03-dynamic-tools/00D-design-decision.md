# Design Decision - As-Needed Tool Definitions - Design Leaf (GATED)

> **For the implementing agent:** DESIGN DOC ONLY. Do NOT commit. Gated:
> dispatch only after ../master.md Q1/Q2 are answered AND 00A numbers exist.
> The dispatch context carries the ratified shape; do not re-litigate it.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** docs/research/dynamic-tools-design.md implementing the user-ratified shape with pinned contracts
- **Dependencies:** 00A report; Q1/Q2 answers recorded in ../orchestrator.md
- **Estimated Context:** 50K
- **Concurrency Group:** solo

## Goal

Turn the ratified shape (hybrid D, single `tool_exposure` knob, capability
gate on `search` - see ./orchestrator.md "Ratified Shape") into an
implementable design: data flow (who predicts, when the list swaps, how
misses recover), exact contract signatures, cache-stability proof sketch
(episode-boundary rule from the parent's Interface Contracts), the frontier
tier signal (exact models.json5 key + fail-safe rule for unknown models +
mid-session downgrade/restore semantics), config surface, and a leaf
decomposition proposal for the orchestrator. Config-surface rule: the whole
`tool_exposure` mechanism ships behind a feature flag defaulting OFF
(byte-identical legacy behavior when off, per docs/agents/opt-in-defaults.md);
when the flag is ON, the mode default is `predicted` (D). Do not conflate the
two defaults.

## Context

- 00A numbers: /tmp/pi-eval/meept-token-audit.md (or archived copy under
  docs/research/ if the orchestrator archived it).
- Existing seams to build on: FilteredToolRegistry (executor.go:1927),
  tool_view meta-tool, intent analyzer (intent_analyzer.go:257),
  StabilizeToolPrefix (loop.go:4442), SetSchemaMode (registry.go:218).
- Precedent for classifier-emitted routing: docs/generated/bus-topology.md
  (how intent results flow today).

## Verified Anchors

- internal/agent/loop.go:4437-4448 - GetDefinitions + StabilizeToolPrefix call site
- internal/agent/executor.go:1927 - FilteredToolRegistry
- internal/agent/intent_analyzer.go:257 - AnalyzeTrueIntent

## Interface Contracts (From Parent)

### What This Leaf Exposes

docs/research/dynamic-tools-design.md with sections: Ratified Shape; Data
Flow (sequence, episode boundaries); Contracts (Go signatures); Cache
Stability (why the prefix survives); Capability Gate (frontier tier signal,
mid-session downgrade-to-D on switch to a lesser model, restore-at-episode-
boundary on switch back, fail-safe for unknown models); Starvation Recovery;
Config; Leaf Decomposition (proposed leaves with file targets + est. context).

### What This Leaf Consumes

- Q1/Q2 answers; 00A tables

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Design doc | Create: docs/research/dynamic-tools-design.md | cross-refs resolve (grep every file:line cited) | n/a | all sections present; every cited anchor verified | Given the ratified shape; When written; Then the orchestrator can decompose leaves without further design questions |
| T2 | Leaf decomposition proposal | same doc | n/a | n/a | proposed leaves honor 128K/25-min sizing (skill rules) | Given the design; When decomposed; Then each leaf <=3 files where possible, single language |

## Self-Verification Checklist

- [ ] Every file:line anchor re-verified at authoring time (grep-confirmed)
- [ ] Design honors the episode-stability rule (no per-turn list churn)
- [ ] Recovery path concrete (what the model does when a needed tool is missing)

**DO NOT COMMIT.**

## Completion Report Format

1. Doc path; 2. proposed leaf list; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Matches ratified shape exactly (no quiet redesign)
- [ ] Cache-stability section names the exact mechanism (StabilizeToolPrefix interaction)
- [ ] Anchors spot-checked (3 random citations)
