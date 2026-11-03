<!-- C5 version layer. Sourced from dsh session-format/src/chain.ts and the
     platform audit §1. Plan author only. -->
# Session Format Version + Forward-Only Migration Catalog — Implementation Leaf (01)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST (00T-tests
> G1-G2) and are committed — your job is to make them pass; do NOT delete,
> skip, weaken, or rewrite any gate test (report suspected-wrong gate tests to
> the orchestrator instead). Do NOT commit — the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files —
> explore with search_files or terminal cat. After writing a file, do NOT read
> it back — write once and stop. Report what you built, files touched, and any
> deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Add the C5 version column + forward-only migration catalog over
  the existing opaque sqlite row store; refuse stored-newer formats and refuse
  gaps. Additive only — do not rewrite the store.
- **Dependencies:** 00T-tests (gate). Serialized FIRST under S5 (owns the
  `schema_version` column in `store_sqlite.go`).
- **Estimated Context:** 40K
- **Concurrency Group:** A (first — everything later touches this file).
- **Audit references:** platform audit §1 "MIGRATION catalog" (chain.ts),
  master C5.

## Goal

dsh compiles a unique complete adjacent migration chain: `defineSessionFormatMigration`
requires `to = from + 1` (chain.ts:30-32); `createSessionFormatChain` throws on
duplicates, on a missing intervening migration (<chain.ts:64-71>), and
`plan(from)` in <chain.ts:79-87> refuses "stored Session uses newer format";
the catalog status vocabulary is `current | migration-required | unsupported |
malformed` (types.ts). Meept's store (`store_sqlite.go` `migrate()`) already
uses `schema_version`-style additive columns via `migrationAddColumn`, but has
NO per-session format version and NO catalog. This leaf adds a per-session
`schema_version` column (default = the build's current version), a
build-static catalog of adjacent forward migrations, and store accessors that
plan a session's forward path or refuse (stored-newer → `ErrSessionFormatNewer`).
Non-migrating sessions stay byte-identical; the catalog is empty (zero
migrations) this branch and only gains entries as future branches add seams —
the refusal/validation machinery is the deliverable, not a schema rewrite.
meept's semantics are backward-compatible forward migration with a
fail-closed guard against unknown future shapes (honest reporting per platform
audit §1 NOTE).

## Context

- `internal/session/store_sqlite.go` — `migrate()` (line 110-310) runs
  `CREATE TABLE IF NOT EXISTS sessions(...)`, `migrationAddColumn` (line
  313-322), `migrationCreateIndex`, etc. The `Create`/`CreateForOwner` INSERTs
  (line 462-550) and `ForkSession` INSERT (line 1905-1911) enumerate columns —
  a new NOT NULL DEFAULT column must be added to those INSERT lists or the
  default back-fills (DEFAULT clause makes absence safe; still add it to the
  INSERTs for explicitness). `GetMostRecent` (line 593) and `getByColumn`
  (line 606) SELECT explicit column lists via `scanSession` (line 618) — do not
  add `schema_version` to the session-scanner unless a call site needs it
  (this leaf does NOT; version lives in its own read path).
- `internal/session/store.go` — the `Store` interface (line 80-174) to append
  the two accessors.
- `/tmp/deepseek-harness/packages/session/session-format/src/chain.ts` —
  Plan/refuse/null-shapes to port faithfully.
- `/tmp/deepseek-harness/packages/session/session-format-catalog/src/` —
  build-static first-party catalog shape.

## Verified Anchors

- `internal/session/store_sqlite.go:110-310` — `migrate()` additive schema +
  `migrationAddColumn`.
- `internal/session/store_sqlite.go:462-503` — `Create` INSERT enumerated
  columns (add `schema_version` to the list for explicitness).
- `internal/session/store_sqlite.go:1905-1911` — `ForkSession` INSERT.
- `internal/session/store.go:80-174` — `Store` interface.
- `/tmp/deepseek-harness/packages/session/session-format/src/chain.ts:64-77` —
  chain validation (gap/dup/too-far reject).
- `/tmp/deepseek-harness/packages/session/session-format/src/chain.ts:79-87` —
  `plan(from)` refuses stored-newer.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
const SessionFormatVersion = 1 // internal/session/format_migration.go
type SessionFormatMigration struct{ Name string; FromVersion, ToVersion int; MigrateUp func(SessionVersionedRow) error }
func NewSessionFormatCatalog(migrations []SessionFormatMigration) (*SessionFormatCatalog, error)
func (c *SessionFormatCatalog) Plan(from int) ([]SessionFormatMigration, error)
var ErrSessionFormatNewer = errors.New("session: stored format newer than this build")
var ErrSessionFormatGap   = errors.New("session: missing adjacent migration in chain")
func (s *SQLiteStore) GetSchemaVersion(sessionID string) (int, error)
func (s *SQLiteStore) MigrateSchema(sessionID string) error
```

### What This Leaf Consumes

- S1, S5 from parent — signatures once in orchestrate.md.
- gate tests 00T-tests G1-G2.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Define version constant + catalog + planning (adjacent-only, refuse newer/gaps) | Create `internal/session/format_migration.go` | `go test -p 2 ./internal/session -run TestFormatCatalog -v` | FAIL: `NewSessionFormatCatalog`/`ErrSessionFormatNewer`/`ErrSessionFormatGap` undefined | PASS: catalog validates the adjacent range (dup/gap/too-far reject); `Plan` returns `ErrSessionFormatNewer` for stored-newer and `ErrSessionFormatGap` for a missing intervening hop | Given a valid adjacent chain and a stored-newer version; When the catalog is built and `Plan` runs; Then build validates, `Plan(stored-newer)` errors `ErrSessionFormatNewer`, `Plan` across a missing hop errors `ErrSessionFormatGap` |
| T2 | Add `schema_version` column + create-time default | Modify `internal/session/store_sqlite.go:110-310` | `go test -p 2 ./internal/session -run TestSchemaVersion -v` | FAIL: `GetSchemaVersion` undefined | PASS: new sessions carry `schema_version = SessionFormatVersion`; existing rows back-fill the DEFAULT (no rewrite) | Given the schema is migrated; When a session is created; Then its `schema_version` reads `SessionFormatVersion` |
| T3 | Store accessors + forward-only `MigrateSchema` | Create `internal/session/schema_version_sqlite.go`; append methods to Store in `internal/session/store.go:80-174` | `go test -p 2 ./internal/session -run TestSchemaVersion -v` | FAIL: `GetSchemaVersion`/`MigrateSchema` missing on `Store` | PASS: `GetSchemaVersion` reads the row; `MigrateSchema` runs the `Plan` path and refuses stored-newer; a gap is returned as `ErrSessionFormatGap` | Given a stored-newer session; When `MigrateSchema` runs; Then it returns `ErrSessionFormatNewer` (never writes) |

T1-T3 must satisfy gate tests G1 (catalog) and G2 (store accessors). Edge
cases beyond the gate (e.g. a `MigrateUp` nil — a no-op hop) may be added as
new unit tests per repo policy.

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests G1-G2 pass GREEN
- [ ] `SessionFormatVersion = 1`; catalog empty this branch (no-op
      `MigrateUp`), validation machinery complete
- [ ] Store interface append does not break existing `SQLiteStore`
      implementations/mocks in the package tests
- [ ] Files at exact paths; no sibling-leaf regression in `store_sqlite.go`
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] S1 signatures match exactly (`Plan` refusal, `ErrSessionFormatNewer`/`Gap`)
- [ ] Additive-only: no existing column/table/driver semantics altered
- [ ] No debug artifacts or line-number corruption
- [ ] Convention: no mutex across I/O, no ignored errors, no bare panic

Output: APPROVED or specific gaps with file + line references.

## Notes

- `MigrateSchema` must write the new version LAST (persist rows first, stamp
  the version after the forward transform commits) so a crash leaves the old
  version stamped and the migration re-runs — dsh's whole-value/commit ordering
  intent.
- This branch ships ZERO migrations; the catalog's value is the fail-closed
  guard that projects/reads (leaf 02) refuse newer stored formats, not a
  rewrite ladder.

## Test Gate (branch F6a)

| Row | Feature | Unit coverage | E2E coverage | Status |
|-----|---------|--------------|--------------|--------|
| F6a | Schema version + migration catalog (refuse newer, gaps) | gate G1-G2 | gate G6 schema round-trip | PENDING |

Traceability: R6 (versioned session log) → [T1-T3]; master C5 → [T1-T3];
branch orchestrator F6a → gate G1-G2.
