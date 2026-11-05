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

