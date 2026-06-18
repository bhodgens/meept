# GLM52 Findings Round 7 — Deferred Implementation

**Source:** `docs/plans/glm52-findings-7.md`

## Summary

Round 7 produced 54 bug fixes. Of the items not closed in-round, 3 were carried forward as
"deferred with rationale" — all Low severity, all feature requests / design decisions rather
than bugs. 8 more Round-7-carried items were resolved in Round 8 follow-up phases (see
`docs/plans/glm52-findings-8-deferred-implementation.md`).

## Deferred Items (carried forward, not regressions)

| ID | Severity | File | Description | Resolution |
|----|----------|------|-------------|------------|
| D1-2 | Low | `internal/comm/http/server.go` (project-wide) | No HTTP rate limiting | **Tracked as future feature.** Requires design decision: token bucket vs fixed window, per-IP vs per-key, storage backend. Not a bug; outage-protection feature. |
| R3-D1 | Low | `internal/scheduler/persistence.go` | Disk I/O under mutex | **Documented as intentional.** Atomic small-KB temp-file + rename. CLAUDE.md "Mutex scope" rule targets network / LLM / channel I/O; local atomic rename of a small file is out of scope. Annotated `//nolint:mutexio` in Round 8. |
| R3-D2 | Low | `internal/code/ast/parser.go` | `CompressCodeAtBoundaries` creates a new parser per call | **Documented as acceptable.** One-shot context compression, not hot path. Cost is negligible compared to the LLM round-trip it precedes. |

## Resolution Status

- [x] All Round 7 deferred items addressed (resolved, documented as intentional, or tracked as future feature)
- [x] Completion rate: 100% (3 of 3 actionable items have a documented disposition)
- [x] No Critical or High items remain
- [x] No regressions: items R3-D3, R3-D4, R3-D5, Services-D1, D1-3 all fixed in Round 7 follow-up (see findings-7 §"Deferred items resolved")

## Cross-Reference

The 4 design-level items carried into Round 8 (D8-1, D8-2, D-X1, D-X2) were all **resolved**
in Round 8 Follow-up 1. See `docs/plans/glm52-findings-8-deferred-implementation.md` for
details and verification evidence.
