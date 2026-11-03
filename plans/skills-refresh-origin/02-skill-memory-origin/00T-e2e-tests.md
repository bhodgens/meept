# 00T-e2e-tests — test-gate leaf (branch 02)

> **For the implementing agent:** TEST-IMPLEMENTER for branch 02's gate. Write tests ONLY,
> no implementation; RED for missing-implementation reasons. Do NOT commit / git add.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Hermetic e2e suite for origin/provenance (features F3, F4, F5)
- **Dependencies:** none
- **Estimated Context:** 35K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/104
- **Concurrency Group:** B

## Goal

Author e2e scenarios under `e2e/suites/skills-refresh-origin/` (build tag `e2e`, same
suite dir as branch 1) pinning: skill writes stamp the correct origin per path; the
memory_facts origin column is populated with legacy rows reading 'unknown'; and the doctor
origin-backfill check is report-only.

## Context

- Scenario shape: scratch daemon + a skill write through each funnel (tool-mediated = agent,
 CLI/manual = user, evolver = evolver) then read back the frontmatter; a memory fact write
 then read the origin column; a `meept doctor` run capturing the origin-backfill line.
- Migration concern: existing memory_facts rows must keep working with origin='unknown'
 (additive NOT-NULL DEFAULT column - assert no data loss).

## Verified Anchors

- internal/skills/lifecycle/writer.go:198 - WriteSkill (today: no origin, no notifier)
- internal/memory/fact_store.go:56 CREATE TABLE memory_facts; :130 Upsert; :161 INSERT
- cmd/meept/doctor.go:33 doctorCheck; :84 runDoctor
- internal/skills/models.go:241-266 SkillMetadata (no Origin field today)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | e2e F3: origin stamped per write path (agent/user/evolver) | Create: e2e/suites/skills-refresh-origin/origin_test.go; Modify: e2e/manifest.json | `make e2e-fast-area AREA=skills-refresh-origin` | FAIL - origin frontmatter / WriteSkillWithOrigin undefined | GREEN once 01-skill-origin-stamp lands | Given each write path; When a skill is written; Then frontmatter origin == expected value |
| T2 | e2e F4: memory_facts.origin populated; legacy rows 'unknown' | Create: e2e/suites/skills-refresh-origin/origin_test.go | same | FAIL - no origin column | GREEN once 02-memory-origin-column lands | Given a new fact write + a pre-migration row; When read; Then new has origin, legacy reads 'unknown' |
| T3 | e2e F5: doctor origin-backfill report-only | Create: e2e/suites/skills-refresh-origin/doctor_test.go | same | FAIL - no doctor check | GREEN once 03-doctor-report lands | Given 'unknown' rows exist; When `meept doctor` runs; Then it reports counts and does NOT block or write |

## Self-Verification Checklist

- [ ] Tests compile (go build -tags e2e ./e2e/suites/skills-refresh-origin/)
- [ ] RED for the right reason; manifest entry in same commit
- [ ] No implementation written; DO NOT commit / git add

## Review Checklist (For Review Agent)

- [ ] F3/F4/F5 rows each map to a test function
- [ ] Behavior assertions only (origin value, doctor output), not internals
- [ ] e2e tag present; hermetic

## Notes

- F4 must seed a pre-migration row (insert without origin) to prove the legacy-read path.
