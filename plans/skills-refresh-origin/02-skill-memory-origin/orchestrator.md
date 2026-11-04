# 02-skill-memory-origin — Branch Orchestrator

> **For the executing agent:** Branch orchestrator for #104 (skills 5b origin/provenance).
> Dispatch this branch's test gate + implementation leaves, review in-session, commit.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 4 documents (00T-e2e-tests, 01-skill-origin-stamp, 02-memory-origin-column, 03-doctor-report)
- **Scope:** origin/provenance on skill + memory writes: frontmatter field + NOT-NULL
 column + report-only doctor backfill check.

## GitHub Issues

- https://github.com/bhodgens/meept/issues/104 - this branch implements that issue.

## Goal

Skill and memory records carry no authorship, so curation cannot distinguish user- vs
agent- vs evolver-authored content. This branch adds an origin value to every durable
write (skill frontmatter + memory_facts column), stamps it on every write path, and
surfaces the backfill gap (rows still 'unknown') report-only in meept doctor. No
auto-rewrite of history; no behavior gate on trust ordering (documented, not enforced).

## Architecture

Two funnels, one per durable store. Skill funnel: lifecycle.Writer
(internal/skills/lifecycle/writer.go:198 WriteSkill) gains WriteSkillWithOrigin + a
change-notifier; every caller (skill tools at components.go:7219/7225, evolver at
evolver.go:819, CLI at cmd/meept/skills.go) passes its Origin. SkillMetadata
(internal/skills/models.go:241) gains Origin `yaml:"origin,omitempty"`. Memory funnel:
memory_facts (internal/memory/fact_store.go:56) gains origin TEXT NOT NULL DEFAULT
'unknown'; Upsert (fact_store.go:130) stamps f.Origin. Doctor (cmd/meept/doctor.go:84)
appends a report-only origin-backfill check.

## Interface Contracts

OWNS C2, C3, C4; consumes C1 (from branch 1) in the notifier attachment. All pinned in
../../master.md.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-tests.md | leaf (test gate) | none | 35K | B |
| 01 | 01-skill-origin-stamp.md | leaf | 00T + branch 1 01 (C1) | 50K | B |
| 02 | 02-memory-origin-column.md | leaf | 00T | 30K | B |
| 03 | 03-doctor-report.md | leaf | 02 (C3) | 25K | B |

01 dispatches only after branch 1's 01-index-refresh-seam is REVIEWED (it attaches the
notifier to C1 MarkDirty). 03 dispatches after 02 (reads the column it creates).

## Dispatch Protocol

### Phase 0: Test gate
Dispatch 00T-e2e-tests.md: hermetic e2e scenarios (a) skill write stamps the right
origin per path; (b) memory_facts.origin populated + legacy rows read 'unknown';
(c) doctor origin-backfill report-only. Validate in-session; commit; mark TESTS_VALIDATED.

### Phase 1: Implementation
Dispatch 02-memory-origin-column and (after C1 ready) 01-skill-origin-stamp in parallel;
then 03-doctor-report. All gated on TESTS_VALIDATED.

### Phase 2: Review + commit
Review in-session per leaf; git diff shows no gate-test modification; commit each leaf.

## Review Checklist

- [ ] C2/C3/C4 signatures exact; Origin enum values exactly as pinned
- [ ] EVERY skill write path stamps origin (tools, evolver, CLI); evolver writes OriginEvolver
- [ ] memory_facts migration is additive (NOT NULL DEFAULT 'unknown'), legacy rows read unknown
- [ ] doctor check is report-only: never blocks, never writes
- [ ] No new default flipped; stable prefix unaffected by an unchanged stamp
- [ ] e2e policy + typed-nil + gofmt honored; no line-number corruption

## Coding Conventions

Go, gofmt; wrap errors with %w; migration additive only; doctor report-only; e2e
assertions in e2e/suites/ (build tag e2e); user-facing strings lowercase.

## Requirement Traceability

| Req | Plan element | Verified by |
|-----|--------------|-------------|
| R3 skill frontmatter origin, all write paths | 01 T1-T3 | e2e origin scenario |
| R4 memory_facts.origin + legacy unknown | 02 T1-T3 | e2e origin scenario |
| R5 doctor backfill report-only | 03 T1-T2 | e2e doctor scenario |
| R6 evolver consumes origin (trust documented) | 01 T4 | evolver test (OriginEvolver stamp) |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-tests | COMPLETE | 0 | gate GREEN; F4/F5 design fixes recorded in master delta log |
| 01-skill-origin-stamp | COMPLETE | 0 | C2 landed; stamps verified |
| 02-memory-origin-column | COMPLETE | 0 | C3 landed; in-place migration proven |
| 03-doctor-report | COMPLETE | 0 | C4 landed; report-only (mode=ro) verified |

Status: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED |
REVIEWED | COMPLETE | BLOCKED.

## Integration Test Plan

make e2e-fast-area AREA=skills-refresh-origin (origin + doctor scenarios GREEN); make
test (no regressions); migration is additive (existing memory_facts rows keep working,
origin reads 'unknown').

## Test Gate

Test leaves: 00T-e2e-tests.md (hermetic e2e). Dispatched first, validated in-session.

| # | Feature | Source | E2E test | Status |
|---|---------|--------|----------|--------|
| F3 | skill frontmatter origin per path | 01 T1-T3 | origin-scenario | COMPLETE |
| F4 | memory_facts.origin + legacy unknown | 02 T1-T3 | origin-scenario | COMPLETE |
| F5 | doctor backfill report-only | 03 T1-T2 | doctor-scenario | COMPLETE |

Delta log: (none yet).

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated addenda.

## Notes

- C1 dependency: 01-skill-origin-stamp attaches its change-notifier to branch 1's
 SkillIndex.MarkDirty — do NOT define a second dirty seam.
- origin trust ordering (user > evolver > agent > review) is DOCUMENTED, not a gate:
 the evolver reads the field; it does not block or quarantine automatically.
