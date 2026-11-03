<!--
  03-session-storage branch orchestrator. Sourced from the root master
  contracts C5/C6/C7, the DSH audits (/tmp/dsh-audit/token-management.md §III,
  platform.md §1-§2, §6), and real dsh+meept source. Plan author only: no
  code, no commit. Created 2026-11-02.
-->

# Session Storage: Versioned Log, Projections, Cross-Session Reference, Storage Events — Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Your job: (1) dispatch the test-gate leaf, (2) validate tests in-session,
> (3) dispatch implementation agents, (4) review their work in-session,
> (5) re-dispatch if incomplete, (6) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 5 leaves (00T-tests, 01-session-version, 02-session-projection,
  03-session-reference, 04-storage-events)
- **Scope:** Add a versioned, forward-only session-format layer (C5), a
  session-projection fold registry with a per-session watermark cache (C5
  projections), the C7 cross-session bounded reference, and the C6 durable
  storage record-change events — all ADDITIVE over the existing opaque sqlite
  row store (never a rewrite).

## Goal

Meept models the session as opaque sqlite rows (`internal/session/`
`store_sqlite.go`: `sessions`, `session_messages`, `session_tool_calls` tables,
`migrate()` with ad-hoc `migrationAddColumn`). dsh models the session as a
versioned append-only event log whose migrations are a streaming adjacent
`vN→vN+1` catalog that refuses stored-newer formats and refuses gaps; projects
queryable views through pure fold-units driven over committed events with a
per-session watermark cache; recalls a sibling session's surface through a
byte-bounded cross-session snapshot; and emits a durable `domain/changed`
record-change event after every write. This branch ports the version-layer,
the projection substrate, the bounded cross-session reference, and the storage
record-change event source onto meept's existing store — the substrate lock to
unlock 04-session-modes (persisted per-session bools, master C5 consumers) and
GUI/TUI live refresh (master C6 consumers).

Per the platform audit (§1-§2), the goal is explicitly NOT a row-store
rewrite: "Session-log migration (C5) is additive versioning; DO NOT rewrite the
existing opaque sqlite store. Projections read the raw log." (master Notes #3,
Open Question #3.)

## Architecture

A thin version column + a build-static adjacent migration catalog sits under
the store (leaf 01). A projection registry folds registered pure units forward
over the session's committed rows, watermarked per-(session,key) in sqlite
(leaf 02); the registry exposes snapshot + change feed, and its projected
user/assistant surface is what the cross-session reference consumes (leaf 03).
Independently, a typed bus topic `storage.record.changed` carries a schema
`{store, key, op, value}` payload published strictly after durable commit
(leaf 04). The three store-touching leaves share ONE file
(`internal/session/store_sqlite.go`) as the integration surface, so their
dispatch is serialized 01 → 02 → (03‖04). Whole-value discipline carry-over:
a load-bearing projection state is the complete post-change state, never a
delta (dsh session-projection load-bearing rule).

## Interface Contracts

Root master.md holds the C5/C6/C7 prose once; this branch holds the concrete
Go shapes its leaves share. Leaves reference the IDs below — never restate the
full signatures in a leaf.

### S1: Session format version + forward-only migration catalog (Owner: 01-session-version)

```go
// internal/session/format_migration.go (NEW)
const SessionFormatVersion = 1 // SESSION_FORMAT_VERSION, current build

type SessionFormatMigration struct {
    Name        string
    FromVersion int
    ToVersion   int             // must equal FromVersion+1 (adjacent)
    MigrateUp   func(row SessionVersionedRow) error // per-row forward transform; nil for no-op
}

type SessionFormatCatalog struct{}
func NewSessionFormatCatalog(migrations []SessionFormatMigration) (*SessionFormatCatalog, error)
// Chain validation (dsh format/src/chain.ts:64-77): every v, 0<=v<current, has
// EXACTLY ONE adjacent entry; duplicates + unnamed entries reject; a migration
// whose FromVersion >= current rejects. Returns error on gap/dup, never partial.
func (c *SessionFormatCatalog) Plan(from int) ([]SessionFormatMigration, error)
// Refuses stored-newer (dsh chain.ts:79-87): returns ErrSessionFormatNewer when
// from > SessionFormatVersion; returns ErrSessionFormatGap when a needed
// intervening migration is missing.
// Store schema-version accessors on SQLiteStore (leaf 01 adds a
// `schema_version INTEGER NOT NULL DEFAULT <current>` column to `sessions`):
func (s *SQLiteStore) GetSchemaVersion(sessionID string) (int, error)
func (s *SQLiteStore) MigrateSchema(sessionID string) error // forward-only via catalog; refuses stored-newer + gaps
// Interface additions are appended to the Store interface in internal/session/store.go.
```

Consumers: 02 (projection requires version ≤ current before folding),
04 (emitter carries the store version context).

### S2: Session projection registry + per-session watermark cache (Owner: 02-session-projection)

```go
// internal/session/projection.go (NEW) + projection_cache.go (NEW)
type ProjectionDefinition[K comparable] struct {
    Key          K          // projection key this unit owns (e.g. "session-summary")
    StateVersion int        // bump on fold-semantics/serialized-shape change; persisted rows with older ver are DISCARDED at read, never migrated (dsh projection-cache spec.ts:24-27)
    Init         func(meta SessionVersionedMeta) SessionProjectionState
    Apply        func(state SessionProjectionState, msg Message) SessionProjectionState // MUST return the same state reference when the unit is not interested (zero downstream work, dsh index.ts:87-90)
    View         func(state SessionProjectionState) any // optional read projection; omitted for host-only units
}

type SessionHash struct{ Hash string } // load-bearing projection keys' View must be plain JSON (cache precondition)
type SessionProjectionRegistry struct {
    // drive is called ONCE per committed message by the store post-commit
    // (mirrors turnGossip: snapshot under the lock, fire after unlock).
}
func NewSessionProjectionRegistry(cache *ProjectionCache) *SessionProjectionRegistry
func (r *SessionProjectionRegistry) Register(def any) (handle SessionProjectionHandle, err error)
func (r *SessionProjectionRegistry) OnAppend(sessionID string, msg Message) // eager drive over committed row
func (r *SessionProjectionRegistry) Snapshot(sessionID string, keys ...string) (SessionProjectionSnapshot, error)
func (r *SessionProjectionRegistry) OnChanged(listener func(sessionID, key string, value any, seq int64)) (dispose func())

// ProjectionCache: watermark-cached per-session checkpoints, sqlite table
// session_projection_cache(session_id, key, ver, seq, val) — dsh cache spec.ts
// (sessionId,key,ver,seq,val); a row is never wrong, only possibly stale; seq
// says exactly how stale; ver mismatch discards at read (never a migration).
func NewProjectionCache(db *sql.DB) (*ProjectionCache, error)
func (c *ProjectionCache) Checkpoint(sessionID string, rows map[string]ProjectionCheckpointRow) error
func (c *ProjectionCache) LoadSession(sessionID string) (map[string]ProjectionCheckpointRow, error)
func (c *ProjectionCache) RestoreFloor(rows map[string]ProjectionCheckpointRow) (int64, error) // lowest usable seq (dsh index.ts:579-589)
func (c *ProjectionCache) CachedSnapshot(sessionID string, keys ...string) SessionProjectionSnapshot
```

Consumers: 03 (reads the projected user/assistant surface for the reference
snapshot).

### S3: Cross-session bounded reference (Owner: 03-session-reference)

```go
// internal/context/sessionref/ (NEW). Budget constants frozen from dsh
// (context/session-reference): MAX_REFERENCES=3; referenceContextFraction=0.2;
// per-source budget = max(DEFAULT_MAX_REFERENCE_BYTES=65_536,
// floor(contextWindowTokens * 4 * 0.2)) — 4 bytes/token sizing heuristic.
const MAX_REFERENCES = 3
const DefaultMaxReferenceBytes = 65_536
const ReferenceContextFraction = 0.2
const SourceKind = "session-reference" // injected source.kind, parity with dsh SessionReferenceSource

func ParseMentionText(text string) (clean string, refs []SessionReferenceInput, err error)
// Mention grammar `@[label](meept-session:<base64url ID>)`; a bare
// `meept-session:<base64url>` is also a reference. Malformed explicit mention
// fails; on health, err wraps ErrSessionRefInvalid.

type SessionReferenceInput struct{ SessionID string; Label string }
type SessionReferenceResolver struct{}
func NewSessionReferenceResolver(proj SessionProjectionSurface, spill *llm.SpillService) *SessionReferenceResolver
func (r *SessionReferenceResolver) Prepare(ctx context.Context, ownerID string, refs []SessionReferenceInput,
    contextWindowTokens int64) (*PreparedReference, error)
// Returns full snapshot data (projected user+assistant text ONLY — tools,
// reasoning, system/developer excluded), a Preview (drop-oldest non-checkpoint
// then head-tail truncate longest with an omitted-bytes notice, dsh
// projection.ts:94-146), stats, and the spill locator when truncated.
func (r *SessionReferenceResolver) ListCandidates(ctx context.Context, ownerID, query string, limit int) ([]SessionReferenceCandidate, error)
```

Consumers: none downstream inside this tree (standalone capability, master C7);
the agent turn-preparation wires `Prepare` to inject the snapshot after the
citing message (leaf 03 integration step).

### S4: Storage record-change events (Owner: 04-storage-events)

```go
// internal/storage/events.go (NEW). Typed bus topic, schema mirrors master C6
// {store,key,op:"put|delete",value} and dsh storage-domain events.ts
// DomainChanged {domain,table,key,operation,value}.
type ChangeOp string
const (
    ChangeOpPut    ChangeOp = "put"
    ChangeOpDelete ChangeOp = "delete"
)
type RecordChanged struct {
    Store string    `json:"store"`
    Key   string    `json:"key"`
    Op    ChangeOp  `json:"op"`
    Value json.RawMessage `json:"value,omitempty"` // new snapshot on put; absent on delete
}
// internal/storage/events.go declares the typed topic:
var StorageRecordChangedTopic = bus.NewTopic[RecordChanged]("storage.record.changed")

// internal/storage/emitter.go (NEW)
type ChangeEmitter struct{ bus *bus.MessageBus } // nil-setters ignored
func NewChangeEmitter(b *bus.MessageBus) *ChangeEmitter
func (e *ChangeEmitter) Publish(source string, rc RecordChanged) // no-op while bus lands nil; never blocks commit
```

Consumers: GUI/TUI live refresh (out-of-tree; the topic is the contract);
leaf 04 wires it to the session store's post-commit + delete paths.

### S5: Shared-file integration (serialization)

`internal/session/store_sqlite.go` is the shared integration surface for leaves
01, 02, and 04. Dispatch is therefore SERIALIZED: 01 → 02 → (03 ‖ 04). Each
leaf touches a disjoint set of functions/columns in that file:
01 adds the `schema_version` column + `GetSchemaVersion`/`MigrateSchema`;
02 adds the projection drive call beside the existing `turnGossip` post-commit
publish in `SaveMessages`; 04 adds the emitter publish beside the same point
and in `Delete`/`ClearMessages`. An implementer modifying the file after a
prior leaf landed MUST re-read that file first (stale-read rule) and never
revert a sibling leaf's change.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-tests.md | test leaf | none | 40K | Phase 0 |
| 01 | 01-session-version.md | leaf | 00T (gate) | 40K | A |
| 02 | 02-session-projection.md | leaf | 01 (S1) | 55K | B (after 01) |
| 03 | 03-session-reference.md | leaf | 02 (S2, S3) | 60K | C (after 02, with 04) |
| 04 | 04-storage-events.md | leaf | 01, 02 (S1, S5) | 45K | C (after 02, with 03) |

Same-letter children dispatch together; group order is the store_sqlite.go
serialization order S5.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

1. Dispatch 00T-tests.md as a subagent: "Write tests ONLY. No implementation.
   Tests must fail RED for missing-implementation reasons. Do NOT commit. Do
   NOT git add." Brief carries this orchestrator's S1-S5 contracts + the
   coverage matrix rows F6a-F8 below.
2. Validate IN-SESSION (never delegate the review): run the tests, map every
   test back to a matrix row, apply the delta protocol (update the LESSER
   artifact), check the e2e exercises the real contract surface (bus + store),
   not mocks.
3. Commit gate tests; set children TESTS_VALIDATED. Implementation may then
   dispatch, in group order.

### Phase 1: Dispatch Concurrency Group A (leaf 01)

Dispatch 01-session-version.md via `delegate_task`: goal = "Implement all tasks
from [leaf]"; context = leaf text + S1/S5 contracts + coding conventions +
relevant source INLINED (store_sqlite.go migrate(), store.go Store interface,
dsh chain.ts Plan/refuse shapes). Include the no-commit, no-git-add,
no-read_file-on-source, write-once-and-stop rules. The gate tests already exist
and are committed.

### Phase 2: Review and Commit Each Child

Review in-session (main model). Check leaf spec, contracts, Review Checklist;
run the branch gate suite; `git diff` must show no gate-test modification (any
change = NEEDS_FIXES). Pass: commit (explicit pathspecs) + status REVIEWED.
Gaps: re-dispatch with findings, max 3 cycles, then escalate to parent.

### Phase 3: Integration Review + Dispatch Remaining Groups

After leaf 01 REVIEWED, dispatch group B (leaf 02). After 02 REVIEWED, dispatch
group C (leaves 03 and 04 in parallel). After all children REVIEWED: verify
S1-S5 across the combined leaves, `go build ./...`, run `internal/session` +
`internal/storage` + `internal/context/sessionref` tests with `go test -p 2`,
then the e2e suite `make e2e-fast-area AREA=session` (manifest path_map entry
added by 00T). `make graphs` + `make graphs-check` (bus topic and Store
interface surface changed). No import cycles. Gaps: re-dispatch the failing
leaf with contract fixes (uncommitted). Pass: normalize formatting, run the
line-number corruption check, commit integration, set children COMPLETE,
report COMPLETE to ../../master.md.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts S1-S4 satisfied exactly (signatures, types, paths)
- [ ] All specified files created/modified at exact paths; `store_sqlite.go`
      shows NO sibling-leaf regression
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
      (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing; NEW feature tests in the hermetic e2e tier /
      the gate suite — no new unit files for features unless the leaf says so
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on every Set*/wiring seam, pkg/id.Generate never
      UnixNano/rand, no bare panic)
- [ ] Whole-value rule honored for load-bearing projections (complete
      post-change state, never delta)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing capability has a user-facing surface or the deferral
      is documented (root AGENTS.md wiring requirement)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN)
- **Naming:** snake_case topics/payload JSON keys; exported Go types per repo
- **Imports:** stdlib group first, then third-party; no unused (U1000-strict)
- **Error handling:** wrap with context; no ignored errors; sentinel errors
  (`ErrSessionFormatNewer`, `ErrSessionFormatGap`) for refuse paths so callers
  use `errors.Is`
- **Bus/typed topics:** publish via `bus.PublishT`/`SubscribeT` with a
  package-level `bus.Topic[T]` var; the payload must stay JSON/schema-valid
- **Testing:** NEW feature tests in hermetic e2e tier (see 00T); unit pins
  allowed for the pure catalog/fold math (01/02 catalog + budget logic are pure
  functions, and the root AGENTS.md e2e policy allows unit tests for
  in-package pure helpers with an explicit note); table-driven
- **Formatting:** gofmt before reporting; ASCII only in new .md
- **Security:** pkg/id.Generate for ids; path-sanitize spill session ids
  (reuse `sanitizeSpillSessionID`, llm/spill.go:33-43); untrusted framing for
  any injected cross-session content (REFERENCE_WARNING, dsh spill.ts:8-10)

## Requirement Traceability

| Req | Source (verbatim where available) | Plan element | Verified by |
|-----|-----------------------------------|--------------|-------------|
| R4 | "END TO END and unit tests for all features as a prerequisite" | 00T-tests (phase-0 gate) | Test Gate matrix |
| R6 | items 1-10,13,14: moderate features (versioned session log(5), storage events(6), session-ref(2)) | leaves 01-04 (C5/C6/C7) | per-leaf Test Gate |
| — | master C5 "add Version int to store + migration catalog (adjacent, forward-only; refuse stored-newer)" | 01-session-version | gate unit + e2e |
| — | master C5 "SessionProjection registry fold-units derive queryable views" | 02-session-projection | gate unit + e2e |
| — | master C7 "max 3 refs, 0.2x context window with 64KiB floor; project user+assistant text only; drop-oldest then head-tail truncate; full transcript spilled + locator on truncation" | 03-session-reference | gate unit + e2e |
| — | master C6 "after every persistent-store commit, emit storage.record.changed {store,key,op,value}, schema-validated, op-tagged, durable; consumers GUI/TUI live refresh" | 04-storage-events | gate unit + e2e |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests | PENDING | 0 | phase-0 gate |
| 01-session-version | PENDING | 0 | serialized first (S5) |
| 02-session-projection | PENDING | 0 | after 01 |
| 03-session-reference | PENDING | 0 | after 02, parallel with 04 |
| 04-storage-events | PENDING | 0 | after 02, parallel with 03 |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T-tests reaches
TESTS_VALIDATED.

## Integration Test Plan

- `go build ./...` then `go test -p 2 ./internal/session ./internal/storage ./internal/context/sessionref` — all GREEN.
- `make e2e-fast-area AREA=session` — the 00T e2e suite (version refusal,
  projection watermark, reference budget+spill, storage events) exercises the
  real store + bus surface (manifest `path_map` entry added by 00T).
- Cross-boundary: leaf 02's `OnChanged` fires for a message appended via the
  store; leaf 03's `Prepare` consumes a projected surface; leaf 04's
  `StorageRecordChangedTopic` deliveries arrive AFTER the matching commit
  (assert order against store state).
- `make graphs` + `make graphs-check` (new `storage.record.changed` topic +
  Store interface surface changed).
- No import cycles: `session` imports `internal/storage` (emitter); `storage`
  MUST NOT import `session` (keeps the leaf-04 seam one-directional).

## Test Gate

Test leaf: [00T-tests.md] — dispatched in Phase 0, validated by the control
agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec §) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------|-----------|-----------|--------|
| F6a | SchemaVersion column + forward-only migration catalog (refuse stored-newer, refuse gaps) | 01 T1-T4 | gate unit (catalog Plan/refuse, store accessors) | gate e2e (schema round-trip) | PENDING |
| F6b | SessionProjection registry fold-units (register, on-append drive, snapshot, onChanged) | 02 T1-T4 | gate unit (fold + state-version discard) | gate e2e (drive over store append) | PENDING |
| F6c | Per-session projection watermark cache (checkpoint/load, restore floor, cached snapshot) | 02 T5-T6 | gate unit (watermark + stale-ver discard) | gate e2e (cache round-trip via store) | PENDING |
| F7 | Cross-session bounded reference (3-ref cap, 0.2x/64KiB budget, user/assistant projection, drop-then-head-tail truncate, spill+locator) | 03 T1-T6 | gate unit (budget math, mention grammar, truncate) | gate e2e (prepare over a real second session + spill) | PENDING |
| F8 | storage.record.changed put/delete op-tagged events post-commit | 04 T1-T4 | gate unit (emitter nil-safety, payload shape) | gate e2e (subscribe + assert post-commit order) | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda (dated, appended to the dispatch brief). Never edit a dispatched leaf
document mid-execution.

## Open Questions

1. C7's exact injection point in the agent turn-prep path: the resolver's
   `Prepare` output (direct content + snapshot context) is wired by the leaf's
   integration step; the precise line in `internal/agent` turn-preparation is
   chosen by the implementer from the negotiated seam (see leaf 03). If a clean
   seam exceeds the 3-file budget, the leaf documents the deferral and the
   resolver still ships + is gate-tested standalone.
2. First projection unit default: `session-summary` (name, last_activity,
   message_count, has_response) feeds the TUI/GUI session list and is the fold
   the 03 reference reads for labels. Additional units are future work.

## Notes

- Additive only. The existing opaque store, all its columns, and `migrate()`
  are untouched semantically; leaves ADD columns/functions/tables.
- The whole branch sources from the dsh audits; constants (3, 0.2, 64KiB, 4
  bytes/token) are frozen from `/tmp/deepseek-harness/packages/context/
  session-reference/` (config.ts:4,8; index.ts:55,387; projection.ts:94-180,
  spill.ts:8-29) and the store-shape from
  `/tmp/deepseek-harness/packages/session/` (chain.ts, projection/src/index.ts,
  projection-cache/src/spec.ts, storage-domain/src/events.ts). The audit's
  file:line anchors are the re-read point, not the final say — verify any
  constant against the source before hard-coding.
- `internal/session` imports `internal/storage` (one-way). `internal/storage`
  imports only `internal/bus` + stdlib.
