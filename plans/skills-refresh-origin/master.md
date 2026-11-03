# skills-refresh-origin — Root Orchestrator

> **For the executing agent:** You are the ROOT orchestrator for this tree.
> Job: (1) dispatch implementation agents for the child branches, (2) review their
> work in-session, (3) re-dispatch if incomplete, (4) track completion. Do NOT
> implement code yourself. Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 branch orchestrators (01-skill-index-refresh, 02-skill-memory-origin)
- **Scope:** Close two adopted capabilities from the 2026-11-01 harness audit —
  mid-session skill-index visibility (#103) and skill+memory provenance/#origin tags (#104).
- **GitHub issues:** #103 (5a refresh), #104 (5b origin). Tree = both.

## GitHub Issues

| Issue | Title | Branch |
|-------|-------|--------|
| [bhodgens/meept#103](https://github.com/bhodgens/meept/issues/103) | harness-adopt: skill index refresh-while-idle + byte-stable rebuild | 01-skill-index-refresh |
| [bhodgens/meept#104](https://github.com/bhodgens/meept/issues/104) | harness-adopt: origin/provenance tags on skill + memory writes (+ doctor backfill report) | 02-skill-memory-origin |

Source material: `docs/research/2026-11-01-sausheong-harness-audit.md` (adoption candidates
5a/5b; harness refs `runtime/skills_refresh.go`, `persistence/annotations.go`,
`tools/skills.go`, `tools/memory.go`, `execution/permission.go`).

## Goal

Today a skill the agent authors mid-session via `skills_create`/`skills_patch`
(internal/daemon/components.go:7219/7225, through lifecycle.Writer) is invisible to the
model on the next turn: the capabilities/skill prompt index is built ONCE at boot
(internal/daemon/components.go:3713 -> agent/prompt.go:391,520 `Build()`), and
`CapabilityIndex.Rebuild` (internal/skills/capability_index.go:331) is the only rebuild,
at startup. And skill/memory writes carry no authorship: the on-disk skill has no
`origin` field and `memory_facts` has no `created_by`/`origin` column
(internal/memory/fact_store.go:56), so curation cannot distinguish user- vs
agent- vs evolver-authored records.

This tree delivers, in two concurrent branches:
- **Branch 1 (#103):** a dirty-marking + idle `Refresh()` seam on `SkillIndex` and a
  turn-boundary refresh in the prompt path, so a mid-session skill write is visible next
  turn without a restart, and a no-op refresh is byte-identical (cache-stable).
- **Branch 2 (#104):** an `origin` frontmatter field on skills (stamped by every write
  path: user / agent / evolver / review) and an `origin` column on `memory_facts`,
  plus a report-only `meept doctor` backfill check.

Both branches keep the stable-prefix work (plans/loop-economics) intact: a refresh or
stamp that does NOT change the rendered index must not bust the cached prompt prefix.

## Architecture

Branch 1 and Branch 2 are file-disjoint at the leaf level EXCEPT the skill write funnel
(`lifecycle.Writer`), which BOTH touch: branch 2 stamps `origin` and attaches a
change-notifier, branch 1 consumes that notifier to mark the index dirty. The shared
seam is `lifecycle.Writer` — pinned once in the Interface Contracts below so the two
branches meet without coordination. Branch 1's index/prompt refresh seam (C1) is consumed
by branch 2's writer-marking leaf, so branch 1's `01-index-refresh-seam` must reach
REVIEWED before branch 2's `01-skill-origin-stamp` dispatches (cross-branch contract).

Control flow: skill write -> `Writer.WriteSkillWithOrigin(...)` -> notifier ->
`SkillIndex.MarkDirty()` -> next turn boundary, `Build()` sees dirty -> `SkillIndex.
Refresh()` -> capabilities rebuilt -> prompt index updated (byte-stable when unchanged).
Memory: `memory_store`/fact tools -> `FactStore.Upsert(f{Origin})` -> column stamped.

## Interface Contracts

### C1: SkillIndex refresh + dirty seam

```
// Owner: 01-skill-index-refresh/01-index-refresh-seam.md
// Consumed by: 02-skill-memory-origin/01-skill-origin-stamp.md (notifier target)
type SkillIndex struct { /* internal/skills/index.go:63 */ ... }
func (i *SkillIndex) MarkDirty()          // set flag; O(1), no rescan
func (i *SkillIndex) IsDirty() bool
func (i *SkillIndex) Refresh() error      // rescan tiers, rebuild index in place;
                                          // byte-identical output when the skill set is unchanged
// agent/prompt.go turn-boundary hook:
func (b *CapabilitiesBuilder) refreshIfNeeded(idx *skills.SkillIndex) // called at the
// next turn boundary (after tool results join), not mid-stream
```

### C2: origin type + Writer write-path

```
// Owner: 02-skill-memory-origin/01-skill-origin-stamp.md
// Consumed by: skill tools (components.go:7219/7225), evolver (evolver.go:819), CLI (cmd/meept/skills.go)
type Origin string
const (
    OriginUnknown Origin = "unknown" // legacy / unstamped
    OriginUser    Origin = "user"    // CLI / manual / mcp-surface write
    OriginAgent   Origin = "agent"   // tool-mediated write
    OriginEvolver Origin = "evolver" // internal/skills/lifecycle/evolver.go
    OriginReview  Origin = "review"  // a review-pass write
)
func (w *lifecycle.Writer) SetChangeNotifier(fn func(skillName string)) // fires post-write
func (w *lifecycle.Writer) WriteSkillWithOrigin(name, content string, o Origin) error
// existing WriteSkill(name, content) (writer.go:198) becomes:
func (w *lifecycle.Writer) WriteSkill(name, content string) error  // == WriteSkillWithOrigin(..., OriginUnknown)
// SkillMetadata (models.go:241) gains:
Origin string `yaml:"origin,omitempty"`
```

### C3: memory origin column

```
// Owner: 02-skill-memory-origin/02-memory-origin-column.md
// memory_facts (fact_store.go:56) gains:  origin TEXT NOT NULL DEFAULT 'unknown'
type MemoryFact struct { /* ... */ Origin string }
func (s *FactStore) Upsert(ctx context.Context, f MemoryFact) error // stamps f.Origin; legacy rows read 'unknown'
```

### C4: doctor origin backfill report

```
// Owner: 02-skill-memory-origin/03-doctor-report.md
// runDoctor (cmd/meept/doctor.go:84) appends one doctorCheck:
// name "origin-backfill", ok = (no 'unknown' rows), detail = per-origin counts
// Report-only: NEVER blocks, NEVER auto-writes.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-skill-index-refresh/orchestrator.md | branch | none | 40K | A |
| 02 | 02-skill-memory-origin/orchestrator.md | branch | C1 (01's 01 leaf) | 55K | B |

Branch 1 (01) and Branch 2's file-disjoint leaves dispatch in parallel; Branch 2's
`01-skill-origin-stamp` waits on Branch 1's `01-index-refresh-seam` (cross-branch C1).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill `hierarchical-plan-execution`.

### Phase 0: Test Gate (code branches, before ANY implementation dispatch)

Dispatch each branch's `00T-e2e-tests.md` first (hermetic e2e suite under
`e2e/suites/skills-refresh-origin/`, `e2e` build tag; add/ensure the manifest
`path_map` entry for `internal/skills/` and `internal/memory/` in the same commit).
Validate in-session against each branch's Test Gate matrix; apply the delta protocol;
commit the gate tests; set both branches TESTS_VALIDATED. Then implementation may dispatch.
See references/tests-first-gate.md. (Repo e2e policy: new FEATURE tests live in the
hermetic e2e tier, not new unit-test files.)

### Phase 1: Dispatch Concurrency Group A + B (parallel, after both gates VALIDATED)

Dispatch `01-skill-index-refresh/01-index-refresh-seam.md` (A) and
`02-skill-memory-origin/{02-memory-origin-column,03-doctor-report}.md` (B) in parallel.
`02-skill-memory-origin/01-skill-origin-stamp.md` (B) dispatches only AFTER
`01-index-refresh-seam.md` is REVIEWED (it consumes C1).

### Phase 2: Review and Commit Each Child

Review in-session (main model - never a delegated reviewer). Check leaf spec, contracts
C1-C4, `git diff` shows no gate-test modification. Pass: commit + status REVIEWED.
Gaps: re-dispatch with findings, max 3 cycles, then escalate.

### Phase 3: Integration Review

After ALL leaves REVIEWED: verify C1/C2/C3/C4 hold across branches, run the full e2e
suite (`make e2e-fast-area AREA=skills-refresh-origin`), `make graphs-check` (bus/RPC
surfaces unchanged expected), no import cycles. Normalize formatting, run the
line-number corruption check, commit integration changes, set leaves COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:
- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts C1-C4 satisfied (signatures, types, exact file paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] TDD followed; code follows project conventions
- [ ] No scope creep; no debug artifacts, TODOs, placeholders
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Stable-prefix invariant: a no-op refresh / an unchanged stamp does not bust the
      cached prompt prefix (plans/loop-economics)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept). Run `gofmt` before reporting.
- **Naming:** follow per-package AGENTS.md; exported types get doc comments.
- **Imports:** group stdlib / this-module / third-party; no unused imports.
- **Error handling:** wrap with `fmt.Errorf("%w")`, return early; do not ignore errors.
- **Typed-nil guards:** concrete pointers into interface fields only behind an
  explicit nil check (internal/daemon/AGENTS.md typed-nil rule); pin with a nil-dependency
  test through the real wiring function.
- **Opt-in defaults:** any new config key ships default-off (docs/agents/opt-in-defaults.md).
- **e2e:** new feature assertions in `e2e/suites/` (build tag `e2e`), manifest `path_map`
  entry in the same commit; no new unit-test files for the feature.
- **UI:** all user-facing strings lowercase (UI convention).

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | #103: "mid-session skill authoring visible next turn, no restart" | 01/01-index-refresh-seam T1-T3 | e2e `skills-refresh-origin` refresh scenario |
| R2 | #103: "no-op refresh is byte-identical (cache-stable)" | 01/01 T3 | e2e prefix-hash assertion |
| R3 | #104: "skill frontmatter origin, all write paths" | 02/01-skill-origin-stamp T1-T3 | e2e origin-stamp scenario |
| R4 | #104: "memory_facts origin column, legacy=unknown" | 02/02-memory-origin-column T1-T3 | e2e origin-column scenario |
| R5 | #104: "doctor reports backfill gap, report-only" | 02/03-doctor-report T1-T2 | e2e doctor scenario |
| R6 | "evolver curation consumes origin (trust ordering documented, not gated)" | 02/01 T4 | evolver test asserts OriginEvolver stamp |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01/00T-e2e-tests | PENDING | 0 | |
| 01/01-index-refresh-seam | PENDING | 0 | |
| 02/00T-e2e-tests | PENDING | 0 | |
| 02/01-skill-origin-stamp | PENDING | 0 | waits on 01/01 (C1) |
| 02/02-memory-origin-column | PENDING | 0 | |
| 02/03-doctor-report | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED.
Gate rule: implementation leaves stay PENDING until their branch's `00T` reaches
TESTS_VALIDATED; 02/01 additionally waits on 01/01 REVIEWED.

## Integration Test Plan

- `make e2e-fast-area AREA=skills-refresh-origin` — all scenarios GREEN.
- `make test` (canonical, `-p 2`) — existing suite still green (no regressions).
- `make graphs-check` — bus/RPC/HTTP topology unchanged (expected: no new topics).
- Prefix-stability: e2e asserts `capability index hash` unchanged across a no-op refresh.

## Test Gate

Test leaves: `01/00T-e2e-tests.md`, `02/00T-e2e-tests.md` (hermetic e2e). Dispatched in
Phase 0, validated by the control agent (root orchestrator, in-session).

| # | Feature | Source | E2E test | Status |
|---|---------|--------|----------|--------|
| F1 | index refresh reflects mid-session write | 01/01 T1-T2 | refresh-scenario | PENDING |
| F2 | no-op refresh byte-stable | 01/01 T3 | refresh-scenario (hash) | PENDING |
| F3 | skill frontmatter origin stamped per path | 02/01 T1-T3 | origin-scenario | PENDING |
| F4 | memory_facts.origin column + legacy unknown | 02/02 T1-T3 | origin-scenario | PENDING |
| F5 | doctor origin backfill report (report-only) | 02/03 T1-T2 | doctor-scenario | PENDING |

Delta log: (none yet).

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda.
Never edit a dispatched leaf document mid-execution.

## Notes

- `lifecycle.Writer` is the shared seam; do NOT let two leaves rewrite its signature in
  different shapes — pin to C2 exactly.
- `OriginUnknown` backfill is deliberate: existing skills/facts read `unknown`, and the
  doctor surfaces the count rather than silently rewriting history.
- Do NOT flip any default: `origin` stamping is additive (a new optional frontmatter key +
  a new NOT-NULL-default column); refresh defaults ON only when a compactor/index is already
  wired (mirrors today's surface), no new opt-in knob unless the reviewer wants one.
