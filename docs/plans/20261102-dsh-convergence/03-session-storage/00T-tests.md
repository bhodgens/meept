# 03-Session-Storage Test Leaf — Gate Tests (00T)

> **For the test implementer:** You write TESTS ONLY for the
> 03-session-storage branch. You do NOT write or modify implementation files.
> Your tests must fail RED for MISSING-IMPLEMENTATION reasons (build tags /
> missing symbols are fine; syntax errors are not). Run them once to confirm
> they fail for the right reason. Do NOT commit. Do NOT git add. The
> orchestrator validates your tests against the branch Test Gate matrix and
> commits them at TESTS_VALIDATED.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Full unit + e2e gate suite for all four implementation leaves
  (C5 version, C5 projections, C7 session reference, C6 storage events).
- **Dependencies:** none (gates the whole branch).
- **Estimated Context:** 40K
- **Concurrency Group:** Phase 0 (before any implementation).
- **Audit references:** dsh chain.ts, session-projection/src/index.ts,
  session-projection-cache/src/spec.ts, context/session-reference/*,
  storage-domain/src/events.ts.

## Goal

Before ANY implementation dispatches, this gate proves every feature the
branch promises has a test that fails for missing-implementation  reasons
and will pass once the leaf contracts S1-S4 are satisfied. New feature tests
live here (root AGENTS.md e2e policy): the pure catalog/fold/budget math gets
in-package unit tests (sanctioned by the branch "Coding Conventions"), and the
store + bus surfaces get hermetic e2e tests under `e2e/suites/session-storage/`.

## Context

The implementing leaves create:
- `internal/session/format_migration.go` — `SessionFormatCatalog`, `Plan`,
  `ErrSessionFormatNewer`, `ErrSessionFormatGap`, `SessionFormatVersion`.
- `internal/session/projection.go` + `projection_cache.go` — registry,
  `SessionProjectionCache`, `session_projection_cache` table.
- `internal/context/sessionref/` — mention parser, resolver, budget.
- `internal/storage/events.go` + `emitter.go` — `RecordChanged`,
  `StorageRecordChangedTopic`, `ChangeEmitter`.

All signatures you test against are pinned in the branch orchestrator's
Interface Contracts S1-S4 — read them there; do not invent shapes. Do NOT use
read_file on existing implementation source — use search_files or terminal cat
if you must look.

## Interface Contracts (From Parent)

### What This Leaf Consumes

- S1 `SessionFormatCatalog.Plan(int)`, `ErrSessionFormatNewer`,
  `ErrSessionFormatGap`, `SQLiteStore.GetSchemaVersion`,
  `SQLiteStore.MigrateSchema` — [01-session-version.md]
- S2 `SessionProjectionRegistry` (Register/OnAppend/Snapshot/OnChanged),
  `SessionProjectionCache` (Checkpoint/LoadSession/RestoreFloor/
  CachedSnapshot) — [02-session-projection.md]
- S3 `ParseMentionText`, `SessionReferenceResolver.Prepare/ListCandidates`,
  constants MAX_REFERENCES=3 / DefaultMaxReferenceBytes=65536 /
  ReferenceContextFraction=0.2 — [03-session-reference.md]
- S4 `RecordChanged{Store,Key,Op,Value}`, `StorageRecordChangedTopic`,
  `ChangeEmitter.Publish` — [04-storage-events.md]

## Tasks

The e2e suite lives under `e2e/suites/session-storage/` with the `e2e` build
tag and must register a `path_map` entry in `e2e/manifest.json` (same commit).
Use the scratch-MEEPT_HOME harness (`harness.WithPreBootHook`) and assert
through the live store + bus surfaces. E2E may spin its own SQLiteStore +
MessageBus in-process instead of the full daemon where the surface under test
is the store/bus (simpler and sufficient for these contracts).

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| G1 | Unit: version constants + catalog chain validation (duplicate gap newer) | Create `internal/session/format_migration_test.go` | `go test -p 2 ./internal/session -run TestFormatCatalog -v` | FAIL: undefined `NewSessionFormatCatalog`/`ErrSessionFormatNewer` | PASS: catalog rejects gaps/dups/too-far; Plan(refuses-newer) returns `ErrSessionFormatNewer` | Given a catalog with an incomplete adjacent range; When `NewSessionFormatCatalog` validates and `Plan` runs on a stored-newer version; Then it errors on the gap and returns `ErrSessionFormatNewer` for stored-newer |
| G2 | Unit: store schema-version accessors refuse stored-newer | Create `internal/session/schema_version_test.go` | `go test -p 2 ./internal/session -run TestSchemaVersion -v` | FAIL: `GetSchemaVersion`/`MigrateSchema` undefined | PASS: `GetSchemaVersion` reads the persisted column; `MigrateSchema` refuses a version > current | Given a session row; When `GetSchemaVersion` then `MigrateSchema` on a stored-newer version; Then version reads back and migration returns an error |
| G3 | Unit: projection registry fold-on-append + state-version discard | Create `internal/session/projection_test.go` | `go test -p 2 ./internal/session -run TestProjection -v` | FAIL: `SessionProjectionRegistry`/`SessionProjectionCache` undefined | PASS: a registered unit folds a message on `OnAppend`; a cache row with a stale `ver` is discarded on read | Given a registered `session-summary` unit; When one message is appended and a cache row with an older state-version is loaded; Then the fold reflects the message and the stale row is discarded, never migrated |
| G4 | Unit: mention grammar + reference budget math + truncate | Create `internal/context/sessionref/` `sessionref_test.go` (package in `internal/context/sessionref`) | `go test -p 2 ./internal/context/sessionref -v` | FAIL: package/symbols undefined | PASS: `ParseMentionText` yields refs; budget = max(65536, floor(window*4*0.2)); head-tail truncate reports omitted bytes | Given a text with `@[lbl](meept-session:...)`; When parsed and budgeted with a small window; Then refs parse, budget floors at 65536, truncation reports exact omitted bytes |
| G5 | E2E: storage.record.changed put/delete post-commit ordering | Create `e2e/suites/session-storage/` `storage_events_test.go` (e2e tag) + register in `e2e/manifest.json` | `make e2e-fast-area AREA=session-storage` | FAIL: `StorageRecordChangedTopic`/`ChangeEmitter` undefined | PASS: a put event for a saved message and a delete event for a removed session arrive after the matching commit | Given a wired emitter and subscribed topic; When the store commits a message then deletes a session; Then put arrives (op put, new value) and delete arrives (op delete) strictly after the commit |
| G6 | E2E: projection + reference read a real second session | Create `e2e/suites/session-storage/` `projection_reference_test.go` (e2e tag) | `make e2e-fast-area AREA=session-storage` | FAIL: undefined registry/resolver symbols | PASS: a second session's projected user/assistant surface is bounded to budget and, on truncation, spills to `$MEEPT_HOME/spill` + returns a locator | Given two sessions; When the resolver `Prepare`s a reference to session B from A; Then the preview is within budget, excludes tool content, and a truncated preview carries a spill locator |

## Self-Verification Checklist

- [ ] Every test compiles and fails RED for missing-implementation reasons
      (missing symbol / empty package — not a syntax error)
- [ ] Every test maps to exactly one coverage-matrix row (F6a-F8)
- [ ] e2e-suite manifest (`e2e/manifest.json`) `path_map` entry added for
      `e2e/suites/session-storage/`
- [ ] `e2e` build tag used; `make e2e-fast-area AREA=session-storage` runs
- [ ] No implementation files written or modified
- [ ] No line-number corruption; no `     N|` prefixes

**DO NOT COMMIT.** The orchestrator handles all git operations after validation.

## Completion Report Format

1. **Files touched:** [every created test path]
2. **Verify command + output:** [exact commands; last 20 lines each]
3. **RED confirmation:** [state of each suite — missing-symbol failure]
4. **Deviations:** [none / list with rationale]

## Review Checklist (For the Control Agent)

- [ ] Tests compile and fail for missing-implementation reasons only
- [ ] Every coverage-matrix row (F6a-F8) has ≥1 unit OR e2e test mapped
- [ ] E2E exercises the real store + bus surface, not mocks of it
- [ ] No tautologies; assertions on behavior (values, state transitions)
- [ ] No implementation files authored by this leaf
- [ ] Manifest entry present; no debug artifacts

Output: VALIDATED + the filled coverage matrix, or a list of specific gaps.

## Test Gate

This test leaf IS the branch's gate: it implements the coverage matrix the
orchestrator must validate (tests-first-gate.md). The matrix below is the
validation target — every row must reach VALIDATED before any implementation
leaf dispatches.

| # | Feature (from plan) | Impl leaf | This leaf's coverage | Status |
|---|---------------------|-----------|----------------------|--------|
| F6a | SchemaVersion + forward-only migration catalog (refuse stored-newer, gaps) | 01 | G1 (unit) + G2 (unit store accessors) | PENDING |
| F6b | Projection registry fold-units | 02 | G3 (unit) + G6 (e2e drive-over-append) | PENDING |
| F6c | Per-session projection watermark cache | 02 | G3 (unit) + G6 (e2e cache round-trip) | PENDING |
| F7 | Cross-session bounded reference (3/cap, 0.2x/64KiB budget, user/assistant, drop+head-tail truncate, spill+locator) | 03 | G4 (unit) + G6 (e2e prepare over real session) | PENDING |
| F8 | storage.record.changed put/delete post-commit | 04 | G5 (unit) + G5 (e2e subscribe + order) | PENDING |

Traceability: R4 (e2e+unit tests prerequisite) → [G1-G6]; master Test Gate rows
F6/F7/F8 → [00T-tests].

## Notes

- Keep the pure math (catalog chain, budget, truncate-omitted-bytes) as
  unit tests (fast, deterministic); keep the store/bus wiring as e2e.
- A test asserting the whole-value rule ("projection state is the complete
  post-change state, never a delta") belongs under G3.
