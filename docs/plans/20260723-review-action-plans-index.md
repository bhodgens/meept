# Codebase Review Action Plans — Index

**Date**: 2026-07-23  
**Source**: Two-subagent codebase review (deleg_d595cb82, deleg_f172d51f)  
**Status**: Plans authored, ready for execution

---

## Plan Trees Created

### ✅ Completed Plan Trees

| # | Plan Directory | Priority | Focus | Est. Effort | Status |
|---|----------------|----------|-------|-------------|--------|
| 1 | `20260723-panic-replacement/` | HIGH | Replace 5 panic() calls with error returns | ~4 hours | **READY** (master + 5 leaves) |
| 2 | `20260723-dispatcher-stop-wiring/` | MEDIUM | Wire Dispatcher.Stop() into daemon shutdown | ~30 min | **READY** (master + 1 leaf) |
| 3 | `20260723-config-extraction/` | MEDIUM | Extract hardcoded URLs/timeouts into config | ~1-2 hours | **READY** (master + 1 leaf) |
| 4 | `20260723-memorystore-compaction/` | HIGH | Implement 3 missing MemoryStore compaction methods | 6-12 hours | **PARTIAL** (master only, needs leaves) |

### 📋 Remaining Items (Not Yet Planned)

| # | Issue | Priority | Estimated Effort | Notes |
|---|-------|----------|------------------|-------|
| 5 | RPC reasoning agent_id mapping | MEDIUM | 1-2 hours | Build conversation→loop registry map |
| 6 | WebSocket bus-to-event forwarding | MEDIUM | 2-3 hours | Real-time event streaming to web clients |
| 7 | Test coverage gaps (cluster/daemon/comm/stt/tts) | MEDIUM | Days per package | Critical infrastructure under-tested |
| 8 | Employee manager lifecycle completion | BLOCKING | Weeks | Hire/Retire/List/Get/Pause/Resume stubbed |
| 9 | Cluster peer fetch implementation | HIGH | 4-8 hours | Distributed workspace sync blocked |
| 10 | Large file refactoring (>2000 lines) | LOW | Days | dispatcher, HTTP server, employee manager, session store |
| 11 | Godoc documentation pass | LOW | 1-2 days | Systematic documentation of public APIs |

---

## GitHub Issues to Create

The following issues should be created on GitHub based on architectural findings:

### Issue 1: Import Cycle Forcing Code Duplication

**Title**: Refactor import cycle between internal/comm/http and internal/employee

**Body**:
```
## Problem

`internal/employee/api_sanitize.go` duplicates sanitization regexes from `internal/comm/http/server.go` to avoid an import cycle:

```go
// Package employee — api_sanitize.go contains regex patterns used by
// api_handlers.go to sanitize error messages before sending them to HTTP
// clients. The patterns mirror internal/comm/http/server.go's sanitization
// regexes but are duplicated here to avoid an import cycle (internal/comm/http
// imports internal/employee when wiring the handler).
```

This creates maintenance burden — changes must be mirrored in two places.

## Proposed Solution

Extract shared sanitization utilities into a new package (e.g., `internal/sanitize/`) that both `comm/http` and `employee` can import without creating a cycle.

## Impact

- **Current**: Duplicated code, risk of drift
- **After**: Single source of truth, easier maintenance
- **Effort**: ~1 hour (extract functions, update imports)

## Files Affected

- `internal/employee/api_sanitize.go` (remove duplication)
- `internal/comm/http/server.go` (import shared package)
- New: `internal/sanitize/sanitize.go` (shared utilities)
```

**Labels**: `refactor`, `tech-debt`, `good-first-issue`

---

### Issue 2: MemoryStore vs SQLiteStore Feature Parity Gap

**Title**: MemoryStore lacks compaction methods (NavigateToBranch, InsertCompaction, ReparentAfterCompaction)

**Body**:
```
## Problem

MemoryStore returns "not implemented" errors for three critical compaction-related methods:

- `NavigateToBranch()` — line 787 in `internal/session/session.go`
- `InsertCompaction()` — line 908
- `ReparentAfterCompaction()` — line 913

SQLiteStore implements these fully, creating a feature parity gap where in-memory sessions lack full functionality.

## Impact

- Users expecting feature parity between storage backends will be surprised
- Branch navigation and memory optimization don't work with MemoryStore
- Testing against MemoryStore doesn't validate compaction logic

## Proposed Solution

Implement the three missing methods in MemoryStore to match SQLiteStore behavior. See plan tree at `docs/plans/20260723-memorystore-compaction/`.

## Acceptance Criteria

- [ ] `NavigateToBranch()` navigates session message tree to specified branch
- [ ] `InsertCompaction()` inserts compacted message nodes correctly
- [ ] `ReparentAfterCompaction()` restructures tree after compaction
- [ ] Tests verify all three methods work correctly
- [ ] No regressions in existing session operations

## References

- Architectural review finding #2
- Plan tree: `docs/plans/20260723-memorystore-compaction/`
```

**Labels**: `bug`, `session-management`, `enhancement`

---

### Issue 3: Uneven Test Distribution Across Critical Packages

**Title**: Increase test coverage for critical infrastructure packages (cluster, daemon, comm, stt, tts)

**Body**:
```
## Problem

While the codebase has 596 test files for 867 implementation files (~69% ratio), several critical infrastructure packages appear under-tested:

- `internal/cluster/` — Distributed consensus logic
- `internal/daemon/` — Daemon lifecycle management
- `internal/comm/telegram/` — Telegram integration
- `internal/stt/` — Speech-to-text processing
- `internal/tts/` — Text-to-speech processing

These packages handle core infrastructure but lack comprehensive automated verification.

## Impact

- Bugs in distributed consensus may not be caught until production
- Daemon lifecycle issues could cause unexpected crashes
- Integration failures with external services (Telegram, STT/TTS providers) go undetected

## Proposed Solution

Add comprehensive test suites for each under-tested package:

1. **Unit tests** for core logic
2. **Integration tests** for external service interactions (with mocks)
3. **Edge case tests** for failure modes

## Acceptance Criteria

For each package:
- [ ] Core logic has >80% line coverage
- [ ] Critical paths have integration tests
- [ ] Error handling verified with edge cases
- [ ] `go test ./...` passes with `-race` flag

## Effort Estimate

- cluster: 2-3 days
- daemon: 1-2 days
- comm/telegram: 1 day
- stt: 1 day
- tts: 1 day
- **Total**: ~1 week

## References

- Architectural review finding #10
```

**Labels**: `testing`, `infrastructure`, `quality`

---

### Issue 4: Incomplete Feature Phases Creating Stubbed API Surfaces

**Title**: Complete employee manager lifecycle methods (currently return ErrNotImplemented)

**Body**:
```
## Problem

The employee system was developed in phases (Phase 1-7 mentioned in comments), with several phases incomplete. Core lifecycle methods return `ErrNotImplemented`:

- `Hire()` 
- `Retire()`
- `List()`
- `Get()`
- `Pause()`
- `Resume()`
- `AmendConstitution()`

The GoalLoop driver and audit enforcement still return this error, making core employee management features non-functional.

## Impact

- **BLOCKING**: Users cannot manage employees through the API
- API surface exists but underlying functionality is stubbed
- Confuses users and developers who expect working features
- Blocks downstream features that depend on employee lifecycle

## Current State

File: `internal/employee/manager.go` line 45 defines `ErrNotImplemented` which is returned by all lifecycle methods.

## Proposed Solution

Complete the employee manager lifecycle implementation. This is a major feature requiring multi-phase implementation (estimated weeks of work).

Suggested phases:
1. **Phase 1**: Basic CRUD (List, Get) — read-only operations
2. **Phase 2**: State transitions (Pause, Resume) — simple state changes
3. **Phase 3**: Lifecycle management (Hire, Retire) — full creation/deletion
4. **Phase 4**: Constitution amendments — complex validation and updates

## Acceptance Criteria

- [ ] All 7 lifecycle methods implemented and functional
- [ ] Tests verify each method works correctly
- [ ] Integration with GoalLoop driver complete
- [ ] Audit enforcement works with completed methods
- [ ] Documentation updated to reflect completed features

## Effort Estimate

- Phase 1: 2-3 days
- Phase 2: 2-3 days
- Phase 3: 3-5 days
- Phase 4: 5-7 days
- **Total**: 2-4 weeks

## References

- Architectural review finding #5 (BLOCKING)
- Related: Employee system design docs
```

**Labels**: `blocking`, `feature-incomplete`, `employee-system`, `high-priority`

---

## Execution Order Recommendation

1. **Week 1**: Execute panic-replacement plan (HIGH priority, prevents crashes)
2. **Week 1**: Execute dispatcher-stop-wiring (MEDIUM, quick win)
3. **Week 1-2**: Execute config-extraction (MEDIUM, improves flexibility)
4. **Week 2-3**: Execute memorystore-compaction (HIGH, feature parity)
5. **Week 3**: Address RPC agent_id mapping + WebSocket forwarding (MEDIUM)
6. **Week 4+**: Begin employee manager completion (BLOCKING, long-term)
7. **Ongoing**: Increase test coverage incrementally
8. **As needed**: Create GitHub issues for remaining items

---

## Next Actions

1. Review plan trees for completeness
2. Execute panic-replacement plan first (highest impact, prevents crashes)
3. Create GitHub issues for architectural concerns
4. Schedule remaining plan execution based on priority
