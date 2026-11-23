# Docs: model-rating.md + AGENTS.md - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** docs/workflows/model-rating.md + AGENTS.md routing row + issue cross-link
- **Dependencies:** 01-e2e-suite REVIEWED (scenario names recorded)
- **Estimated Context:** 30K
- **Concurrency Group:** B* (after 01, or parallel with its scenario list INLINED)

## Goal

The repo requires a user-facing workflow doc + AGENTS.md summary per feature. This
leaf lands both, using judge naming throughout, and states the v1 producer-site
exclusions with reasons (issue #168 R11).

## Context

Doc pattern: docs/workflows/<feature>.md, terse, lowercase-ordered headings, what it
does / how to read it / how it is captured / config and retention notes. AGENTS.md is
size-gated at 16 KiB (root) - add ONLY the internal/modelrating row to the Key
Components table plus a one-line pointer; run `make agents-check` to prove the gate.
The terminology contract (master.md Terminology section) is copied into the doc's
opening so future contributors do not reintroduce reviewer overloading.

## Verified Anchors

- `docs/workflows/` - naming + structure pattern (read any recent workflow doc)
- `AGENTS.md` - Key Components table + invariant routing table (add row + pointer)
- `scripts/agents-check` / `make agents-check` - the gate that must stay green

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `docs/workflows/model-rating.md`: model judge concept, rubric scopes (model vs
  workflow) + attribution rule, defect categories + severities table, storage +
  indefinite retention note, four surfaces (endpoint/CLI/panel/menubar) with
  examples, producer-site inventory WITH v1 exclusions + reasons (review_tools.go
  inline review, pair_manager.go), e2e suite + scenario names from leaf 01
- `AGENTS.md`: one Key Components row (Model rating | internal/modelrating,
  internal/metrics/review_verdicts.go | docs/workflows/model-rating.md) + no other edits
- Issue #168 cross-link comment DRAFT (file docs/plans/model-rating-metrics-plan/issue-close-comment-draft.md
  is OUTSIDE the tree dir? NO - keep it out of the tree; the orchestrator drafts it
  in-session at close time instead. This leaf does NOT create extra .md inside the
  tree dir - the structural checker treats every tree .md as a leaf.)

### What This Leaf Consumes

- Branch 01-03 landed behavior + 01-e2e-suite scenario names

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Workflow doc | Create: docs/workflows/model-rating.md | `make agents-check` (pointer target exists) | pointer dangles | gate green | Given the feature; When a user reads the doc; Then judge concept, scopes, taxonomy, retention, surfaces, exclusions all covered with judge naming |
| T2 | AGENTS.md row | Modify: AGENTS.md | `make agents-check` | row/pointer missing | gate green; root under 16 KiB | Given the new package; When agents-check runs; Then routing row resolves; size gate passes |
| T3 | Feature-docs hook mapping | Modify: the hook's mapping file (grep scripts/ + .githooks/ for the workflow-docs map; repo rule requires feature code changes to map a doc) | `make lint-ci` (or the doc hook dry run) | mapping missing | hook satisfied | Given the doc; When the feature-docs hook runs; Then model-rating.md is mapped |

## Self-Verification Checklist

- [ ] `make agents-check` green; `make lint-ci` green (or the scoped equivalent)
- [ ] Judge naming everywhere; no reviewer overload
- [ ] No new .md files INSIDE docs/plans/model-rating-metrics-plan/
- [ ] Exclusions stated with reasons (R11)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. agents-check + lint-ci tails; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Doc covers R11-13 evidence (inventory, exclusions, same-commit pairing)
- [ ] AGENTS.md delta minimal (one row + pointer); size gate green
- [ ] No reviewer label; terminology section present

Output: APPROVED or specific gaps.

## Notes

If the feature-docs hook mapping lives in a generated file, follow the regeneration
path the repo uses (grep Makefile for the mapping target) - never hand-edit generated
output.
