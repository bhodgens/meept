# 03-doctor-report — implementation leaf (branch 02, contract C4)

> **For the implementing agent:** TDD; gate test exists (doctor scenario F5). Do NOT commit /
> git add.

## Meta

- **Parent:** orchestrator.md
- **Scope:** C4 - report-only `meept doctor` origin-backfill check
- **Dependencies:** 02-memory-origin-column (C3, REVIEWED); 00T-e2e-tests
- **Estimated Context:** 25K
- **GitHub Issue:** https://github.com/bhodgens/meept/issues/104
- **Concurrency Group:** B

## Goal

`meept doctor` gains one check, `origin-backfill`, that counts skills + memory facts by
origin and reports how many are still 'unknown' (the backfill gap). It is REPORT-ONLY: it
never blocks the doctor run and never rewrites history.

## Context

- Doctor surface: cmd/meept/doctor.go (doctorCheck :33, runDoctor :84, checks appended to
 the `checks []doctorCheck` slice; report-only checks set ok=true with a detail string -
 see the orphan-children check at doctor.go:159 for the report-only idiom).

## Verified Anchors

- cmd/meept/doctor.go:33 - type doctorCheck
- cmd/meept/doctor.go:84 - func runDoctor(fix, installMissing bool) error
- cmd/meept/doctor.go:159 - orphan-children report-only doctorCheck idiom
- C3 column (branch 02 leaf 02) supplies the memory_facts.origin to count

## Interface Contracts (From Parent)

### What This Leaf Exposes (C4, exact)

```
// runDoctor appends:
doctorCheck{Name: "origin-backfill", OK: true, Detail: "N unknown skill(s), M unknown fact(s); report-only"}
// OK is always true (report-only); Detail carries the counts.
```

### What This Leaf Consumes

- C3: memory_facts.origin (count 'unknown' rows).
- Skill frontmatter Origin (from C2 / branch 02 leaf 01) to count unknown skills.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | origin-backfill doctorCheck: count unknown skills + unknown facts | Modify: cmd/meept/doctor.go | gate F5 | RED - check missing | GREEN | `meept doctor` prints an origin-backfill line with counts |
| T2 | Report-only contract: ok=true regardless of counts; never writes | Modify: cmd/meept/doctor.go | gate F5 | RED | GREEN | doctor exits 0 even with unknown rows; no file/schema mutation |

## Self-Verification Checklist

- [ ] C4 exact; check is report-only (ok=true, detail carries counts)
- [ ] Reads both stores read-only; no writes; gofmt; DO NOT COMMIT

## Review Checklist (For Review Agent)

- [ ] C4 exact; matches the orphan-children report-only idiom (doctor.go:159)
- [ ] e2e F5 GREEN (doctor output captured, asserts counts + non-blocking exit)

## Notes

- Counts are advisory; the operator (or a future backfill pass) decides how to close the gap.
