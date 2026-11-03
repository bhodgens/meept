<!-- C5 projections. Sourced from dsh session-projection/src/index.ts,
     session-projection-cache/src/spec.ts, platform audit §2. Plan author only. -->
# Session Projection Registry + Per-Session Watermark Cache — Implementation Leaf (02)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST (00T-tests
> G3, G6) and are committed — make them pass; do NOT delete, skip, weaken, or
> rewrite any gate test. Do NOT commit. Do NOT use read_file on existing source
> — explore with search_files or terminal cat. After writing a file, do NOT
> read it back — write once and stop. Report deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** A session-projection fold registry driven forward over committed
  session rows, plus a per-session watermark cache in sqlite. Enables the
  queryable views master C5 promises (session-list, TUI/GUI) and the surface
  leaf 03 reads.
- **Dependencies:** 01-session-version (S1 — fold requires version ≤ current
  before reading a session's rows).
- **Estimated Context:** 55K
- **Concurrency Group:** B (after leaf 01 REVIEWED; owns S5 slice of
  `store_sqlite.go` beside `turnGossip`).
- **Audit references:** platform audit §2 (session-projection +
  session-projection-cache).

## Goal

dsh's projection seam: a `ProjectionDefinition` is a PURE fold
(`init`/`apply`/optional `view`) registered on a registry that the framework
DRIVES forward over every committed session event, with a per-(session,key)
watermark cache whose rows are `(key → {ver, seq, val})` and "never wrong, only
possibly stale: seq says exactly how stale, and a ver mismatch against the live
unit's stateVersion discards it at read time (never a migration)"
(projection-cache spec.ts:24-27). Load-bearing rule: "a state-carrying log
event MUST carry the complete post-change state, never a bare delta"
(index.ts:13). Meept has NO fold registry (platform audit §2 GAP: FULL). meept's
events are its committed `session_messages` rows, so the registry folds `Message`
values. First fold-unit: `session-summary` (name, last_activity, message_count,
has_response) feeding the TUI/GUI session list and the labels leaf 03 reads.

## Context

- `internal/session/store_sqlite.go` — `SaveMessages` (line 922-1022): locks,
  inserts row-by-row in a transaction, `tx.Commit()`, then snapshots
  `s.turnGossip` under the lock, releases, and publishes outside it (line
  1002-1021). This is EXACTLY the post-commit seam the projection drive reuses:
  publish each written `Message` to the registry after unlock, mirroring the
  gossip pattern. `Delete` (line 760-797) fires the lifecycle hook post-commit.
  `GetMessages` (line 1025-1086) is the read source a cold/hydrate fold replays.
- `internal/session/projection_cache_sqlite.go` (created) — the watermark
  cache table.
- `/tmp/deepseek-harness/packages/session/session-projection/src/index.ts` —
  registry contract (`register`, `snapshot`, `cachedSnapshot`, `restore`,
  `restoreFloor`, `onChanged`, `checkpoint`).
- `/tmp/deepseek-harness/packages/session/session-projection-cache/src/spec.ts`
  — `(sessionId,key,ver,seq,val)` rows + `invalidRecords: 'backup-and-skip'`
  disposition (a stale/unreadable cache costs a longer tail replay, never a
  wrong value).

## Verified Anchors

- `internal/session/store_sqlite.go:1002-1021` — post-commit publish seam
  (gossip) to mirror for projection drive.
- `internal/session/store_sqlite.go:922-1022` — `SaveMessages` commit points.
- `internal/session/store_sqlite.go:760-797` — `Delete` post-commit hook seam.
- `/tmp/deepseek-harness/packages/session/session-projection/src/index.ts:87-90`
  — `apply` returns the SAME state reference when uninterested (zero work).
- `/tmp/deepseek-harness/packages/session/session-projection/src/index.ts:550-561`
  — `checkpoint` returns detached structured clones (never the live cell ref).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/session/projection.go
type ProjectionDefinition[K comparable] struct {
    Key          K
    StateVersion int   // bump to invalidate persisted rows; discard, NEVER migrate
    Init         func(SessionVersionedMeta) SessionProjectionState
    Apply        func(state SessionProjectionState, msg Message) SessionProjectionState // same-ref when uninterested
    View         func(state SessionProjectionState) any // optional; must be plain JSON
}
type SessionProjectionRegistry struct{}
func NewSessionProjectionRegistry(cache *SessionProjectionCache) *SessionProjectionRegistry
func (r *SessionProjectionRegistry) Register(def any) (SessionProjectionHandle, error)
func (r *SessionProjectionRegistry) OnAppend(sessionID string, msg Message) // eager drive post-commit
func (r *SessionProjectionRegistry) Snapshot(sessionID string, keys ...string) (SessionProjectionSnapshot, error)
func (r *SessionProjectionRegistry) OnChanged(listener func(sessionID, key string, value any, seq int64)) (dispose func())

// internal/session/projection_cache.go
func NewSessionProjectionCache(db *sql.DB) (*SessionProjectionCache, error)
func (c *SessionProjectionCache) Checkpoint(sessionID string, rows map[string]ProjectionCheckpointRow) error
func (c *SessionProjectionCache) LoadSession(sessionID string) (map[string]ProjectionCheckpointRow, error)
func (c *SessionProjectionCache) RestoreFloor(rows map[string]ProjectionCheckpointRow) (int64, error)
func (c *SessionProjectionCache) CachedSnapshot(sessionID string, keys ...string) SessionProjectionSnapshot
```

### What This Leaf Consumes

- S1 from 01 — a read of a session refuses folds when `GetSchemaVersion` >
  `SessionFormatVersion`.
- S5 from parent — `store_sqlite.go` shared file; add the drive call beside
  `turnGossip`, never revert 01's column.
- gate tests 00T-tests G3, G6.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Registry + fold-unit contract + `session-summary` unit | Create `internal/session/projection.go` | `go test -p 2 ./internal/session -run TestProjection -v` | FAIL: registry/definition symbols undefined | PASS: `Register` returns a handle; `OnAppend` folds a `Message`; an uninterested unit returns the same state ref | Given a registered `session-summary` unit; When one message is appended; Then the fold reflects it and an uninterested unit's state is the same reference |
| T2 | Drive registry on post-commit append | Modify `internal/session/store_sqlite.go` in `SaveMessages` (mirror gossip at line 1002-1021) | `go test -p 2 ./internal/session -run TestProjection -v` | FAIL: `OnAppend` undefined / store unused symbol | PASS: after `tx.Commit()` + unlock, each written message is driven into the registry (non-blocking, best-effort, never fails the save) | Given a store wired with a registry; When `SaveMessages` commits; Then the drive fires after unlock, post-commit |
| T3 | Watermark cache: checkpoint/load/restore-floor/cached-snapshot | Create `internal/session/projection_cache_sqlite.go` (+ table in `migrate()` via `schema_version`-style additive CREATE IF NOT EXISTS) | `go test -p 2 ./internal/session -run TestProjection -v` | FAIL: cache symbols undefined / table missing | PASS: `Checkpoint` upserts `(session_id,key,ver,seq,val)`; `LoadSession` returns rows; `RestoreFloor` returns the lowest usable seq (one-below anchor, dsh index.ts:579-589); `CachedSnapshot` serves only ver-matching rows | Given persisted rows with one stale `ver`; When restored; Then the stale row's key refolds from `init` (discard, never migrate) |
| T4 | Schema-version guard on projection read | Modify `internal/session/projection.go` to check `GetSchemaVersion` before folding; Modify `internal/session/store.go` if a mock needs the accessor | `go test -p 2 ./internal/session -run TestProjection -v` | FAIL: guard absent (fold over a newer-version session) | PASS: a session with `schema_version > SessionFormatVersion` raises the refusal error and is NOT folded | Given a session at a newer stored format; When `Snapshot`/`OnAppend` is called; Then it errors instead of folding |

T1-T4 must satisfy gate G3 (fold + state-version discard) and G6 (reference
reads a second session's projected surface). Beyond-gate additions (e.g. a
`session-summary` View emitting the session-list row) flow through review.

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests G3, G6 pass GREEN
- [ ] Whole-value rule: load-bearing fold state is the complete post-change
      value, never a delta
- [ ] Cache rows carry `ver`; a `ver` mismatch discards at read (never migrates)
- [ ] Drive is post-commit, outside the store lock, best-effort (mirrors gossip)
- [ ] Files at exact paths; no sibling-leaf regression in `store_sqlite.go`
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] S2 signatures match exactly; `Apply` same-reference contract honored
- [ ] Cache table additive; `invalidRecords: backup-and-skip` disposition
      honored (a corrupt row backs-aside, never fails the boot)
- [ ] Schema-version guard present (leaf 01 S1 consumed correctly)
- [ ] No mutex across I/O; no ignored errors; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- `checkpoint` rows are DETACHED copies (structured clone of state), never the
  live cell reference (dsh index.ts:550-561) — reach the live ref and a later
  read corrupts every snapshot.
- The cache is disposable derived data: on a boot, a stale/unreadable cache
  costs a longer tail replay, never a wrong value or a boot failure.

## Test Gate (branch F6b, F6c)

| Row | Feature | Unit coverage | E2E coverage | Status |
|-----|---------|--------------|--------------|--------|
| F6b | Projection registry fold-units | gate G3 | gate G6 (drive over store append) | PENDING |
| F6c | Per-session watermark cache | gate G3 (watermark + stale-ver discard) | gate G6 (cache round-trip) | PENDING |

Traceability: R6 (session-log projection) → [T1-T4]; master C5 projections →
[T1-T4]; branch F6b/F6c → gate G3, G6.
