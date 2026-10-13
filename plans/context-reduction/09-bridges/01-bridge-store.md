# B01 — Bridge Store: BridgeShard + Bridge CRUD (P10)

**Proposal:** P10 (branch 09). **Depends on:** none (first leaf of the branch).
**Files (3):**
- Modify: `internal/memory/vector/shard_manager.go` (add `ShardTypeBridge`)
- Create: `internal/memory/bridges_store.go` (typed CRUD over the shard)
- Test: `internal/memory/bridges_store_test.go`

**Problem:** Bridging facts need a home in the existing flat cosine vector
store. `vector/store.go` already serializes float32 vectors into SQLite blobs
and computes cosine in Go; `shard_manager.go` provides typed shards
(consolidated/recent/project/code/archive) with LRU. This leaf adds the shard
type + typed accessors ONLY — no generation, no recall changes.

**Contracts (branch #1, root #4):**
- Metadata keys EXACTLY: `unit_type` ("bridge"), `evidence_ids` (JSON array of
  source memory ID strings), `created_at` (RFC3339), `verified` ("true"/"false").
- Write-time validation: empty `evidence_ids` → reject with error (branch rule:
  no provenance, no ship).
- Shard load behavior: BridgeShard always-loaded (bridge counts are ~25-35% of
  memory units, small); document the choice in a comment.
- Gossip: bridges must NOT enter dual_store replication. If shard registration
  automatically enrolls shards in gossip, exclude ShardTypeBridge explicitly and
  leave a comment naming the exclusion site.

### Tasks

**Task 1 — failing tests (bridges_store_test.go):**
(i) `TestBridgeShardRoundTrip`: Put a bridge (text, embedding, metadata) →
search by near-identical embedding → hit returns all four metadata keys intact.
(ii) `TestBridgeWriteRequiresEvidence`: empty evidence_ids → error, nothing
persisted. (iii) `TestBridgeShardExcludedFromGossip`: whatever the dual_store
gossip enrollment surface is (locate with search_files), assert ShardTypeBridge
is absent. Run: `go test -p 2 ./internal/memory/ -run TestBridge` — FAIL.

**Task 2 — implement:** add `ShardTypeBridge` to the shard-type enum + factory
path in shard_manager.go; implement bridges_store.go with `PutBridge`,
`DeleteBridge`, `ListBridgesByEvidence(evidenceID string)` (needed by B03
invalidation), and a metadata-scanning helper. Reuse existing vector.Store
primitives; no new embedding model calls here.

**Task 3 — full package:** `go test -p 2 ./internal/memory/...` green.

**Task 4 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] Metadata keys exactly unit_type/evidence_ids/created_at/verified
- [ ] Evidence-less writes rejected at the store boundary
- [ ] Gossip exclusion verified by test, not assertion

## Review Checklist (for orchestrator)
- [ ] No recall-path changes in this leaf
- [ ] Shard enum addition compiles clean across all shard switch statements
