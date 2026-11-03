<!-- C6 storage record-change events. Sourced from dsh storage-domain/src/events.ts
     and the platform audit. Plan author only. -->
# Storage Record-Change Events — Implementation Leaf (04)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST (00T-tests
> G5) and are committed — make them pass; do NOT delete, skip, weaken, or
> rewrite any gate test. Do NOT commit. Do NOT use read_file on existing source
> — explore with search_files or terminal cat. After writing a file, do NOT
> read it back — write once and stop. Report deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** A typed bus topic `storage.record.changed` carrying op-tagged
  put/delete record changes, published strictly after durable commit; wire it
  to the session store's post-commit + delete paths.
- **Dependencies:** 01 (S1 version context), 02 (S5 shared-file integration).
- **Estimated Context:** 45K
- **Concurrency Group:** C (after 02 REVIEWED, parallel with 03).
- **Audit references:** dsh storage-domain/src/events.ts,
  internal/bus/topic.go.

## Goal

dsh storage-domain declares the event source for the domain data form: "Every
durable write emits one event after the backend resolves durability, carrying
the new snapshot and an operation discriminant — never the old value (a
diffing consumer keeps its own previous snapshot)." The `DomainChanged` union is
`{domain, table, key}` + `operation: 'put'` (with `value`) or `'deleted'`
(tombstone, no value), emitted once per write strictly after durability
(events.ts:10-47). Meept has NO such event source (platform audit §6
PARTIAL — meept has OTLP metrics/traces but no domain record-change event).
Master C6 freezes the meept shape: topic `storage.record.changed`, payload
`{store,key,op:"put|delete",value}`, schema-validated, op-tagged, durable;
consumers GUI/TUI live refresh (out-of-tree). meept's bus offers typed topics
via `bus.NewTopic[T]` (`internal/bus/topic.go:18-20`). This leaf adds the
topic + a nil-safe `ChangeEmitter` seam and wires the FIRST durable consumer:
the session store's post-commit paths (SaveMessages → per-message `put`;
Delete → `delete`; ClearMessages → `delete`), mirroring the existing
`turnGossip` post-commit publish pattern (snapshot under the lock, publish
after unlock — never hold a mutex across the bus I/O).

## Context

- `internal/storage/` (created) — new package. `internal/storage` imports only
  `internal/bus` + stdlib. `internal/session` imports `internal/storage`
  (ONE-WAY — `storage` must NEVER import `session`).
- `internal/bus/topic.go` — `Topic[T]`, `NewTopic[T]`, `PublishT`/`SubscribeT`
  (typed, JSON-marshalled payloads). Declare the topic as a package-level var.
- `internal/session/store_sqlite.go` — `SaveMessages` (line 922-1022) commits
  in a transaction, then snapshots `s.turnGossip` under the lock and publishes
  after unlock (line 1002-1021). The emitter publish mirrors this exact point
  (one put per committed message). `Delete` (line 760-797) deletes the row,
  then fires the lifecycle hook post-commit — the emitter `delete` publishes
  beside it. Operator semantic: on `put`, `Value` carries the NEW snapshot
  (`json.RawMessage`); on `delete`, no value (tombstone).
- `/tmp/deepseek-harness/packages/storage/storage-domain/src/events.ts` —
  `DomainChangedBase {domain, table, key}`, `DomainChangedPut {operation:
  'put', value}`, `DomainChangedDeleted {operation:'deleted', value?: never}`.
  meept C6 maps domain+table → the single `Store` string; `key` → the record
  key; `op` → put/delete.

## Verified Anchors

- `internal/bus/topic.go:14-49` — `Topic[T]`/`NewTopic`/`PublishT`.
- `internal/bus/acp_topics.go:75-76` — package-level typed topic declarations
  precedent.
- `internal/session/store_sqlite.go:922-1022` — `SaveMessages` post-commit
  publish seam (mirror for the emitter).
- `internal/session/store_sqlite.go:760-797` — `Delete` post-commit seam.
- `internal/session/store_sqlite.go:1104-1133` — `ClearMessages` delete path.
- `/tmp/deepseek-harness/packages/storage/storage-domain/src/events.ts:10-47`
  — the `domain/changed` event vocabulary to port.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/storage/events.go
type ChangeOp string
const (
    ChangeOpPut    ChangeOp = "put"
    ChangeOpDelete ChangeOp = "delete"
)
type RecordChanged struct {
    Store string            `json:"store"`
    Key   string            `json:"key"`
    Op    ChangeOp          `json:"op"`
    Value json.RawMessage   `json:"value,omitempty"` // new snapshot on put; absent on delete
}
var StorageRecordChangedTopic = bus.NewTopic[RecordChanged]("storage.record.changed")

// internal/storage/emitter.go
type ChangeEmitter struct{ /* nil-safe bus */ }
func NewChangeEmitter(b *bus.MessageBus) *ChangeEmitter
func (e *ChangeEmitter) Publish(source string, rc RecordChanged) // no-op while bus lands nil; never blocks the commit
```

### What This Leaf Consumes

- S4 from parent — signatures + topic name once in orchestrator.md.
- S5 from parent — `store_sqlite.go` shared file; add the emitter publish
  beside `turnGossip`/lifecycle post-commit points, never revert 01/02's work.
- master C6 prose — the `{store,key,op,value}` contract.
- gate tests 00T-tests G5.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | RecordChanged payload + op type + typed topic | Create `internal/storage/events.go` | `go test -p 2 ./internal/storage -run TestStorageEvents -v` | FAIL: `RecordChanged`/`StorageRecordChangedTopic` undefined | PASS: the payload shape is `{store,key,op,value}`; `Op` is `"put"`/`"delete"`; the topic name is `storage.record.changed` | Given a constructed topic; When the payload is JSON-marshalled; Then it matches `{store,key,op,value}` and the topic name is exact |
| T2 | Nil-safe ChangeEmitter over the bus (non-blocking publish) | Create `internal/storage/emitter.go` | `go test -p 2 ./internal/storage -run TestStorageEvents -v` | FAIL: `ChangeEmitter`/`Publish` undefined | PASS: `Publish` sends a typed `RecordChanged` on `StorageRecordChangedTopic`; a nil-bus emitter is a no-op; a nil `Value` on delete is omitted | Given a wired bus and a nil-bus emitter; When `Publish` is called; Then the typed topic receives the payload and the nil case no-ops without a panic |
| T3 | Semver/closed path: schema-validate the payload on publish (op must be put/delete) | Modify `internal/storage/events.go` (or `emitter.go`) | `go test -p 2 ./internal/storage -run TestStorageEvents -v` | FAIL: op validation absent | PASS: an unknown `Op` is rejected (log + skip) rather than emitted | Given a publish with an unknown op; When published; Then it is validated/skipped, never emitted unvalidated |
| T4 | Wire emitter into the session store: put on SaveMessages, delete on Delete + ClearMessages | Modify `internal/session/store_sqlite.go` (beside `turnGossip` at line 1002-1021; beside the lifecycle fire in `Delete` ~line 779-796 and `ClearMessages` ~line 1113-1126); modify `internal/session/store.go` for a `SetStorageEventEmitter` typed-nil setter if needed | `go test -p 2 ./internal/session -run TestStorageEvents -v` ; `go test -p 2 ./internal/session -run TestSaveMessages -v` | FAIL: emitter not wired / `SetStorageEventEmitter` missing | PASS: a committed message publishes a `put` (new value) after the commit; a deleted session publishes a `delete` after the row is gone; a cleared session publishes deletes; publishing is post-commit, outside the lock, best-effort, never fails the save/delete | Given a store wired with an emitter and a subscribed `StorageRecordChangedTopic`; When a message is saved then a session deleted; Then `put` arrives after the commit (value present) then `delete` (no value) |

T4 must satisfy gate G5 (order: put then delete arrive strictly after the
matching commit). The emitter seam is one-directional: `session` → `storage`.

## Self-Verification Checklist

- [ ] All tasks implemented; gate test G5 passes GREEN
- [ ] Topic name `storage.record.changed` exact; payload `{store,key,op,value}`
- [ ] Publish is post-commit, outside the store lock, best-effort (mirrors
      gossip); a nil-bus emitter no-ops
- [ ] `internal/storage` does NOT import `internal/session` (import-cycle gate)
- [ ] Files at exact paths; no sibling-leaf regression in `store_sqlite.go`
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] S4 signatures + topic name match exactly
- [ ] Post-commit ordering: event strictly after the durable commit (asserted)
- [ ] `storage` ⟉ `session` import; typed-nil guards on the emitter setter
- [ ] Op-tagged; `put` carries the new snapshot, `delete` is a tombstone
- [ ] No mutex across I/O; no ignored errors; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- "Durable" means the event fires after a commit the store cannot later roll
  back — same guarantee as the `tx.Commit()` boundary, never a pre-commit
  preview. A diffing consumer keeps its OWN previous snapshot (dsh events.ts:
  4-6) — the event is always the new value, never the old.
- Consumers (GUI/TUI live refresh) live OUTSIDE this tree and subscribe to
  `StorageRecordChangedTopic`; the topic is the contract. Root integration
  (`docs/generated/bus-topology.md`) regenerates via `make graphs` after this
  leaf lands.

## Test Gate (branch F8)

| Row | Feature | Unit coverage | E2E coverage | Status |
|-----|---------|--------------|--------------|--------|
| F8 | storage.record.changed put/delete op-tagged events post-commit | gate G5 (emitter nil-safety, payload shape) | gate G5 (subscribe + assert post-commit order) | PENDING |

Traceability: R6 (storage events, master C6) → [T1-T4]; branch F8 → gate G5.
