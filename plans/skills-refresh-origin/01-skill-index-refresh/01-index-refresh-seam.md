# 01-index-refresh-seam — implementation leaf (branch 01, contract C1)

> **For the implementing agent:** Implement ALL tasks with TDD. The gate tests ALREADY
> EXIST at e2e/suites/skills-refresh-origin/ (refresh scenario + hash scenario) and are
> committed - make them pass. Do NOT delete, skip, weaken, or rewrite any gate test.
> Do NOT commit / git add. Explore with search_files / terminal cat - do not use
> read_file on existing sources, do not re-read a file you just wrote.

## Meta

- **Parent:** orchestrator.md
- **Scope:** C1 - SkillIndex MarkDirty/IsDirty/Refresh + turn-boundary refreshIfNeeded
- **Dependencies:** 00T-e2e-tests (TESTS_VALIDATED)
- **Estimated Context:** 45K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/103
- **Concurrency Group:** A

## Goal

Expose a bounded, cache-stable way for the model to see skills authored mid-session.
`SkillIndex` gains a dirty flag and an idle `Refresh()`; the capabilities builder refreshes
at the next turn boundary (after tool results join, not mid-stream). Refresh is byte-stable
when the skill set is unchanged, so the loop-economics stable prefix survives no-op refreshes.

## Context

- The index is built at boot: internal/daemon/components.go:3713
 `capBuilder.Build(c.AgentRegistry.ListSpecs())` -> capabilities rendered into the prompt
 at internal/agent/prompt.go:391 and :520 (`b.Build()`).
- Rebuild primitive to reuse: internal/skills/capability_index.go:331 `Rebuild()` (do NOT
 fork a second rebuild path).
- SkillIndex lives at internal/skills/index.go:63.
- Branch 2 (C2) will attach a change-notifier on lifecycle.Writer that calls MarkDirty() -
 so this leaf must land MarkDirty FIRST (cross-branch contract).

## Verified Anchors

- internal/skills/index.go:63-71 - type SkillIndex / func NewSkillIndex
- internal/skills/capability_index.go:331 - func (ci *CapabilityIndex) Rebuild()
- internal/agent/capabilities_builder.go:20,220 - NewCapabilitiesBuilder / BuildFromRegistry
- internal/agent/prompt.go:391,520 - builder.Build() composition points (turn boundary)
- internal/daemon/components.go:3712-3713 - boot-time capBuilder construction

## Interface Contracts (From Parent)

### What This Leaf Exposes (C1, exact)

```
func (i *SkillIndex) MarkDirty()          // O(1); set flag under the index mutex
func (i *SkillIndex) IsDirty() bool
func (i *SkillIndex) Refresh() error      // rescan tiers, rebuild in place; byte-identical
                                         // when the skill set is unchanged
// on the capabilities builder:
func (b *CapabilitiesBuilder) refreshIfNeeded(idx *skills.SkillIndex)
// called at the next turn boundary in prompt.go (after tool results join), not mid-stream
```

### What This Leaf Consumes

- C2 notifier target: after this leaf lands, branch 2's Writer.SetChangeNotifier will call
 `idx.MarkDirty()`. No other branch-2 surface is consumed by this leaf.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add MarkDirty/IsDirty to SkillIndex (mutex-guarded flag) | Modify: internal/skills/index.go; Test: (gate) | `make e2e-fast-area AREA=skills-refresh-origin` | RED - undefined methods | GREEN | Given a SkillIndex; When MarkDirty then IsDirty; Then true; after Refresh, false |
| T2 | Add Refresh() that rescan + rebuilds in place, byte-stable when unchanged (reuse capability_index.go:331 Rebuild + the index hash from loop-economics) | Modify: internal/skills/index.go | same | RED | GREEN | Given unchanged set; When Refresh; Then identical hash + IsDirty false |
| T3 | Turn-boundary refreshIfNeeded on CapabilitiesBuilder, wired at prompt.go composition (boundary only) | Modify: internal/agent/capabilities_builder.go, internal/agent/prompt.go | `make test` + gate suite | RED | GREEN | Given a dirty index at turn boundary; When Build; Then the refreshed index is rendered; mid-stream Build does not refresh |
| T4 | No-op byte-stability pin (e2e F2) passes; stable prefix hash unchanged | Verify gate | `make e2e-fast-area AREA=skills-refresh-origin` | n/a | GREEN | covered by T2/T3 behavior |

## Self-Verification Checklist

- [ ] C1 signatures exact as pinned
- [ ] Refresh bounded + byte-stable; Rebuild primitive reused, not forked
- [ ] refreshIfNeeded only at the turn boundary (not mid-stream)
- [ ] No new default flipped; refresh is a no-op when no index wired
- [ ] gofmt clean; typed-nil guards on any new setter
- [ ] DO NOT COMMIT

## Review Checklist (For Review Agent)

- [ ] C1 exact; byte-stability asserted by the gate, not hand-waved
- [ ] No second rebuild path; loop-economics prefix untouched on no-op
- [ ] Conventions honored; no line-number corruption; no scope creep

## Notes

- If a refresh races an in-flight Build, prefer the existing index mutex - do not add a
 second lock that could invert ordering with Build().
