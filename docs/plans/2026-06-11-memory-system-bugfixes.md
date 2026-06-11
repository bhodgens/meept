# Plan: Memory System Bugfixes

**Date**: 2026-06-11
**Priority**: Critical — memory system bugs cause data corruption, panics, and silent failures.
**Scope**: 10 CRITICAL, 14 HIGH bugs across 8 source files.

---

## Summary

Deep audit of the memory system found 10 CRITICAL and 14 HIGH bugs across core manager, tools/daemon wiring, vector/embedding, graph, and scoped manager subsystems. This plan groups fixes by file domain to avoid merge conflicts.

---

## Group 1: Core Manager (`internal/memory/manager.go`)

### C1: `GetExpiredMemories` — NULL scan crash
**Line**: ~1345
**Problem**: `last_accessed_at` scanned into plain `string`, but column is nullable. SQL NULL → scan error.
**Fix**: Use `sql.NullString` for `last_accessed_at` in the scan.

### C2: `GetVersionHistory` — `created_at` scan type mismatch
**Line**: ~1242
**Problem**: `created_at` scanned into `mem.CreatedAt` (time.Time) directly — depends on driver behavior.
**Fix**: Scan into `sql.NullString` then parse, matching the pattern used in `GetByID`.

### H3: `Delete()` swallows real errors
**Line**: ~1394-1410
**Problem**: Errors from `episodic.Delete()` that are not "not found" are silently discarded, then task memory is tried. This masks real DB failures.
**Fix**: Check if error is `ErrNotFound` before falling through. Only fall through on not-found.

### H4: `GetByID()` only searches episodic, not task
**Line**: ~1263-1299
**Problem**: Only queries `episodic_memories` table. Task memories are invisible.
**Fix**: Fall through to task memory on not-found, same pattern as `Delete()`.

### H5: `searchViaSQLite` silently swallows errors
**Line**: ~592-608
**Problem**: Search errors from episodic/task are logged as warnings but don't propagate. Caller gets partial/empty results with no indication of failure.
**Fix**: Return first error encountered. Partial results are worse than an error signal.

---

## Group 2: Tools & Wiring (`internal/tools/builtin/memory.go`)

### C1: `MemoryRecallTool` category mismatch
**Line**: ~660-666
**Problem**: `MemoryRetainTool` stores with category `"hindsight:medium"` (or `"hindsight:high"`), but `MemoryRecallTool` searches with category `"hindsight"`. Exact match fails. Additionally, the SQLite search path ignores the Category field entirely — it's dead code in the query.
**Fix**: Use `LIKE 'hindsight%'` or prefix match, or strip importance suffix before searching.

### C2: `MemoryRecallTool` limit type assertion
**Line**: ~655
**Problem**: `args["limit"].(int)` — JSON unmarshals numbers as `float64`, so this assertion always fails, defaulting to 10 regardless of what the agent requested.
**Fix**: Assert `float64` then cast to int: `limitRaw := int(args["limit"].(float64))`.

### H3: `MemoryGetVersionTool` ignores `version` parameter
**Line**: ~323-326, 332-374
**Problem**: `version` is declared in Parameters but never read from args in Execute. The tool always returns current version.
**Fix**: Read `version` from args. If specified, query version history and return that specific version.

### H5: `MemoryReflectTool` no nil guard on llmClient
**Line**: ~873
**Problem**: If `llmClient` is nil (misconfigured wiring), `t.llmClient.Chat()` panics.
**Fix**: Nil-check `llmClient` at start of Execute and return error.

---

## Group 3: Vector/Embedding (`internal/memory/vector/`)

### C1: Sentence transformer model download is dead code
**File**: `sentence_transformer.go`
**Problem**: `_ = downloader` discards the model downloader. `generateHashEmbedding` produces fake hash-based vectors that aren't meaningful embeddings.
**Fix**: Actually use the downloader to fetch/load the model, or at minimum document this is a placeholder and log a warning.

### C2: `ShardManager.GetShard()` lock ordering / deadlock
**File**: `shard_manager.go:150-182`
**Problem**: `unlockAndUnloadShard` releases the mutex during `unloadShard()`, then re-acquires. If another goroutine triggers eviction simultaneously, double-unload can occur.
**Fix**: Use a dedicated eviction path that doesn't unlock, or use a separate lock for the eviction victim.

### C3: `ShardManager.Search()` passes `efSearch=0`
**File**: `shard_manager.go:247`
**Problem**: `shard.Search(ctx, queryEmb, k/len(shardTypes), 0)` — the third argument is `efSearch`. HNSW with `ef=0` returns no results.
**Fix**: Pass `max(k, 100)` or similar reasonable default for efSearch.

### C4: `CrossShardJoin` data corruption on scan errors
**File**: `cross_shard_join.go:114-122`
**Problem**: When first `rows.Scan` fails, a second `rows.Scan` is called on the same row, reading wrong columns into wrong fields. This silently corrupts data.
**Fix**: On scan error, skip the row (continue) rather than attempting a second scan.

### H5: `Store.embeddingCache` is unbounded
**File**: `store.go:33`
**Problem**: `sync.Map` grows without bound — no eviction, no size limit. Long-running daemons leak memory.
**Fix**: Add LRU eviction with configurable max size, or use a bounded cache.

### H7: Ollama provider `float32` vs `float64`
**File**: `embedding.go:218`
**Problem**: `ollamaEmbeddingResponse.Embedding` is `[]float32`, but Ollama API returns JSON numbers which `json.Unmarshal` decodes as `float64`. Silent truncation or zero-fill.
**Fix**: Change to `[]float64` and convert to `[]float32` after unmarshal.

---

## Group 4: Graph (`internal/memory/graph.go`)

### C1: Graph edge ID collisions from truncation
**Line**: ~231, ~289
**Problem**: Edge ID uses only first 8 chars of source/target UUIDs. Two different edges with matching 8-char prefixes silently overwrite each other via `INSERT OR REPLACE`.
**Fix**: Use full UUIDs or add a random suffix for uniqueness.

### C2: Graph cache TOCTOU race
**Line**: ~580-586
**Problem**: Cache check under RLock, releases lock, then re-acquires for DB fetch — another goroutine can invalidate cache between check and use.
**Fix**: Use single lock scope or atomic value for cache.

### H1: `CreateSimilarityEdges` naive word-overlap
**Line**: ~942
**Problem**: Placeholder implementation using word overlap — not real similarity. Harmless but misleading.
**Fix**: Add log warning that this is a placeholder, or skip if no embedding provider is configured.

---

## Group 5: Scoped Manager (`internal/memory/scoped_manager.go`)

### H3: ScopedManager filters client-side, silently truncating
**Line**: ~41-47
**Problem**: `filterResults()` drops non-matching results client-side after fetching from DB. If limit=10 and all 10 belong to other bots, returns empty slice with no indication.
**Fix**: Inject bot_id filter into the SQL query via metadata filtering instead of post-hoc client filtering.

---

## Implementation Order

1. **Group 1** (manager.go) — core data path fixes
2. **Group 2** (memory.go tools) — agent-facing tool fixes
3. **Group 3** (vector/) — vector search fixes
4. **Group 4** (graph.go) — knowledge graph fixes
5. **Group 5** (scoped_manager.go) — scoped filtering fix

Groups 1-3 are CRITICAL and should be done first. Groups 4-5 are lower priority.

## Verification

- [ ] `go build ./...` compiles
- [ ] `go test ./internal/memory/... -v` passes
- [ ] `go test ./internal/tools/builtin/... -v` passes
- [ ] `go test ./...` passes
