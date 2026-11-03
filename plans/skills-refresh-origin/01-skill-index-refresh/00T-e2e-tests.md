# 00T-e2e-tests — test-gate leaf (branch 01)

> **For the implementing agent:** You are the TEST-IMPLEMENTER for this branch's gate.
> Write tests ONLY - no implementation. Tests must fail RED for missing-implementation
> reasons (the Refresh/MarkDirty seam does not exist yet). Do NOT commit, do NOT git add.
> If a gate test looks wrong, report it - do not weaken it.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Hermetic e2e suite for the skill-index refresh capability (features F1, F2)
- **Dependencies:** none
- **Estimated Context:** 30K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/103
- **Concurrency Group:** A

## Goal

Author the e2e scenarios under `e2e/suites/skills-refresh-origin/` (build tag `e2e`) that
pin branch 1's behavior, plus the manifest `path_map` entry. These tests are the gate:
they must be RED until branch 1's 01-index-refresh-seam lands.

## Context

- e2e harness: `e2e/harness` (hermetic scratch daemon; scratch-bin reaper). Existing
 suites are single-package `//go:build e2e` dirs; the manifest is `e2e/manifest.json`
 (keys: version, path_map, suites, scenarios).
- Scenario shape: start a scratch daemon, create a skill on disk via the write funnel
 (or a direct lifecycle.Writer call the harness exposes), run one chat turn that observes
 the index, assert visibility; then a no-op second refresh and assert the index hash.
- Stable prefix: the capability-index hash (plans/loop-economics) is the byte-stable
 artifact under test; F2 asserts it is UNCHANGED across a no-op refresh.

## Verified Anchors

- e2e/manifest.json (path_map has `internal/skills/` -> check current suites; add
 `skills-refresh-origin` to that list in the same commit)
- e2e/harness (daemon.go scratch-bin reaper; suites call it the way e2e/suites/smoke does)
- internal/skills/index.go:63 SkillIndex (the thing under test; today has no Refresh)
- internal/agent/capabilities_builder.go:20 NewCapabilitiesBuilder / :220 BuildFromRegistry

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | e2e suite package + scenarios F1 (mid-session skill write visible next turn) | Create: e2e/suites/skills-refresh-origin/refresh_test.go; Modify: e2e/manifest.json (path_map + suites) | `make e2e-fast-area AREA=skills-refresh-origin` | FAIL - SkillIndex.Refresh/MarkDirty undefined (no refresh seam) | GREEN once 01-index-refresh-seam lands | Given a scratch daemon; When a skill is written mid-session and a turn runs; Then the next turn's prompt carries the new skill index |
| T2 | e2e scenario F2 (no-op refresh is byte-stable) | Create: e2e/suites/skills-refresh-origin/refresh_test.go (second test func) | `make e2e-fast-area AREA=skills-refresh-origin` | FAIL - same as T1 | GREEN when refresh exists and is byte-stable | Given an unchanged skill set; When Refresh() runs; Then the capability-index hash is identical to pre-refresh |

## Self-Verification Checklist

- [ ] Tests compile (go build -tags e2e ./e2e/suites/skills-refresh-origin/)
- [ ] RED for the RIGHT reason (missing implementation, not syntax)
- [ ] Manifest path_map entry added in the same tree
- [ ] No implementation code written (tests only)
- [ ] Do NOT commit / git add

## Review Checklist (For Review Agent)

- [ ] F1/F2 rows of the branch Test Gate matrix each map to a test function
- [ ] Assertions on observable behavior (index visibility, hash equality), not internals
- [ ] e2e build tag present; hermetic (no external network)

## Notes

- Keep the suite self-contained: skill creation happens in the scratch home dir.
- The manifest entry is a same-commit requirement (repo e2e policy) - include it in T1.
