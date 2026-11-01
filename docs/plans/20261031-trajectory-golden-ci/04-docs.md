# Trajectory Grading Documentation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. This leaf is docs-only. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat.

## Meta

- **Parent:** ../master.md
- **Scope:** docs/workflows/trajectory-grading.md documenting the golden
  workflow, per the feature-docs rule (internal/<pkg> ->
  docs/workflows/<pkg>.md, here named for the feature).
- **Dependencies:** 01-comparator.md, 02-fixture-capture.md,
  03-e2e-suite.md (all REVIEWED - docs describe landed behavior)
- **Estimated Context:** 30K
- **Concurrency Group:** E
- **Audit references:** Requirement R10; master policy section

## Goal

Give users and future agents one page explaining: what golden-path
trajectory grading is in meept, how goldens are captured (FakeLLM-
scripted ONLY), what the comparator modes mean, how the CI gate bites,
and how to regenerate a golden for an intentional behavior change
(investigate-then-regenerate, never blind-regen).

## Context

- docs/workflows/ - feature spec home; existing pages follow a
  prose + command-example pattern (see docs/workflows/e2e-testing.md
  for the tier table style).
- docs/workflows/eval.md - existing eval doc; cross-reference it for
  outcome grading (Judge()) so the two docs divide cleanly: outcome
  vs sequence.
- AGENTS.md e2e Testing Policy section - the doc must not contradict
  it (hermetic tier, manifest rule, no new unit test files for features).
- This is a docs-only leaf: verify cross-references resolve instead of
  running tests.

## Verified Anchors

- [docs/workflows/eval.md] - existing outcome-grading doc to cross-link
- [docs/workflows/e2e-testing.md] - tier/manifest documentation style
- [AGENTS.md - e2e Testing Policy] - policy the doc must align with
- [Makefile - golden-regenerate targets] - commands to document (leaf 02
  output)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- docs/workflows/trajectory-grading.md containing, at minimum:
  - What is graded (normalized tool-call sequences; C1-C3 semantics in
    user terms)
  - Match modes table (exact / in_order / any_order, threshold)
  - Capture policy: goldens come ONLY from FakeLLM-scripted hermetic
    runs; live tier never sequence-gated
  - CI behavior: member of the required e2e-fast aggregate gate;
    blocking within the suite
  - Regenerate workflow: `make golden-regenerate SCENARIO=<name>` /
    `make golden-regenerate-all`; investigate-then-regenerate rule
  - Fixture format reference (C4)
  - Cross-links: docs/workflows/eval.md (outcome grading),
    docs/workflows/e2e-testing.md (tiers)

### What This Leaf Consumes

- Landed behavior from leaves 01-03 (write from the tree's contracts
  section and the landed code, not from memory)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Author docs/workflows/trajectory-grading.md with all six required sections listed in the exposure contract; ASCII only | Create: docs/workflows/trajectory-grading.md | `grep -c '^## ' docs/workflows/trajectory-grading.md` >= 6 and all relative links resolve (python3 link check or manual grep of target files) | file missing | doc complete, links resolve | Given the landed feature; When the page is read; Then a user can capture, gate, and regenerate without reading source |
| T2 | Cross-link from the ecosystem: add a pointer to trajectory-grading.md from docs/workflows/eval.md (one line, "sequence grading" see-also) so outcome vs sequence grading is discoverable | Modify: docs/workflows/eval.md (one line) | `grep -n 'trajectory-grading' docs/workflows/eval.md` | no pointer | pointer present | Given eval.md; When a reader looks for sequence grading; Then the see-also line routes them |

**Worked example (canonical row expansion):**

T1:
1. Read landed code: internal/eval/trajectorydiff exported API,
   e2e/harness/golden.go, the suite, the Makefile targets.
2. Write the page from observed behavior (commands verbatim from the
   Makefile; fixture keys verbatim from C4).
3. Verify: `grep -c '^## ' docs/workflows/trajectory-grading.md` >= 6;
   every relative link target exists.

Tests-first gate branch: n/a - docs-only leaf.

## Self-Verification Checklist

- [ ] Both tasks completed; all six sections present
- [ ] Every command in the doc matches the landed Makefile/code verbatim
- [ ] Cross-references resolve (no dead relative links)
- [ ] Policy statements match master.md exactly (scripted-only goldens,
      blocking gate, investigate-then-regenerate)
- [ ] ASCII only
- [ ] No scope creep - no edits to other docs beyond the T2 one-liner

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Doc describes LANDED behavior (spot-check 2 commands against code)
- [ ] No contradictions with AGENTS.md or docs/workflows/eval.md
- [ ] Links resolve; ASCII only
- [ ] Regenerate workflow documented with the investigate-first rule

Output: APPROVED or specific gaps.

## Notes

- Keep the doc user-shaped: the "how do I regenerate after an
  intentional change" section is the one users will actually need -
  lead with the rule, then the command.
- Do not duplicate e2e-testing.md's tier table; link it.
