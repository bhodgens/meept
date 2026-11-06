# 01-skill-index-refresh — Branch Orchestrator

> **For the executing agent:** Branch orchestrator for #103 (skills 5a refresh).
> Dispatch this branch's test gate + implementation leaf, review in-session, commit.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 2 documents (00T-e2e-tests, 01-index-refresh-seam)
- **Scope:** Mid-session skill-index visibility: dirty-marking + idle Refresh() seam.

## GitHub Issues

- https://github.com/bhodgens/meept/issues/103 - this branch implements that issue.

## Goal

A skill the agent authors mid-session via skills_create/skills_patch is invisible to the
model until a restart because the capabilities/skill prompt index is built once at boot
(internal/daemon/components.go:3713 -> agent/prompt.go:391,520 Build()). This branch adds
a bounded, cache-stable refresh so the next turn after a skill write sees the new index,
without busting the stable prompt prefix when the skill set is unchanged.

## Architecture

`SkillIndex` (internal/skills/index.go:63) grows MarkDirty()/IsDirty()/Refresh(). The skill
write path (lifecycle.Writer, consumed by branch 2's C2 notifier) marks the index dirty.
At the next turn boundary (agent/prompt.go, after tool results join, not mid-stream), the
capabilities builder calls Refresh() only when IsDirty(). Refresh rescan is bounded and
byte-stable: unchanged skill set => identical index => the loop-economics stable prefix
survives. `CapabilityIndex.Rebuild` (capability_index.go:331) is the existing rebuild
mechanism; Refresh() is the idle-triggered call into it + SkillIndex.

## Interface Contracts

Sees contracts C1 (this branch OWNS C1) and C2 (consumed from branch 2) in ../../master.md.
This leaf produces C1 exactly as pinned in the master; branch 2's 01-skill-origin-stamp
attaches its notifier to the C1 MarkDirty().

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-tests.md | leaf (test gate) | none | 30K | A |
| 01 | 01-index-refresh-seam.md | leaf | 00T validated | 45K | A |

## Dispatch Protocol

### Phase 0: Test gate
Dispatch 00T-e2e-tests.md: hermetic e2e suite under e2e/suites/skills-refresh-origin/
(build tag `e2e`), scenarios: (a) skill write then next-turn index reflects it; (b) no-op
refresh leaves the capability index hash unchanged. Validate in-session; commit; mark this
branch TESTS_VALIDATED.

### Phase 1: Implementation
Dispatch 01-index-refresh-seam.md ONLY after TESTS_VALIDATED. It owns C1.

### Phase 2: Review + commit
Review in-session; git diff shows no gate-test modification; commit.

## Review Checklist

- [ ] C1 signatures exact (MarkDirty/IsDirty/Refresh on *SkillIndex; refreshIfNeeded on
     *CapabilitiesBuilder called at the turn boundary, not mid-stream)
- [ ] Refresh is bounded + byte-stable (asserted by the e2e hash scenario)
- [ ] No new default flipped; refresh runs only when an index is already wired
- [ ] Convention + typed-nil rules honored; e2e policy honored (suite, not new unit files)
- [ ] No line-number corruption in source

## Coding Conventions

Go, gofmt; wrap errors with %w; typed-nil guard on the refresh seam; e2e assertions in
e2e/suites/ (build tag e2e); user-facing strings lowercase; opt-in-defaults policy.

## Requirement Traceability

| Req | Plan element | Verified by |
|-----|--------------|-------------|
| R1 mid-session write visible next turn | 01 T1-T2 | e2e refresh scenario |
| R2 no-op refresh byte-stable | 01 T3 | e2e prefix-hash assertion |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-tests | COMPLETE | 0 | gate GREEN (F1/F2) |
| 01-index-refresh-seam | COMPLETE | 1 | C1 landed + daemon wiring closed in-session (rescan source + per-turn builder attach) |

Status: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED |
REVIEWED | COMPLETE | BLOCKED.

## Integration Test Plan

make e2e-fast-area AREA=skills-refresh-origin (refresh scenarios GREEN); make test (no
regressions); prefix-hash assertion in the e2e suite proves stable-prefix survival.

## Test Gate

Test leaves: 00T-e2e-tests.md (hermetic e2e). Dispatched first, validated in-session.

| # | Feature | Source | E2E test | Status |
|---|---------|--------|----------|--------|
| F1 | index refresh reflects mid-session write | 01 T1-T2 | refresh-scenario | COMPLETE |
| F2 | no-op refresh byte-stable | 01 T1-T3 | refresh-scenario (hash) | COMPLETE |

Delta log: (none yet).

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- Refresh must run at a turn BOUNDARY (idle), never mid-stream — mirrors the harness
 runtime/skills_refresh.go pattern and the loop-economics stable-prefix work.
- Reuse CapabilityIndex.Rebuild (capability_index.go:331) as the rebuild primitive; do
 not fork a second rebuild path.
