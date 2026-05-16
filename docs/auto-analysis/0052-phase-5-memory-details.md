# Phase 5: Memory System -- Detailed Test Findings

**Tested**: 2026-05-16
**Binary**: `/Users/caimlas/go/bin/meept`

## Test Results

### Test 1: `memory store` subcommand
**Expected**: CLI subcommand to store a memory into the database.
**Actual**: `memory store` does NOT exist as a CLI subcommand. The CLI only has `memory [query]` (free-text search).
**Verdict**: FAIL -- Feature not implemented at CLI layer.

**Root cause**: `cmd/meept/memory.go` only defines `newMemoryCmd()` with a single `RunE` that calls `runMemorySearch()`. No subcommands for `store`, `recent`, `get-context`, `consolidate`, or `graph`.

### Test 2: `memory search "TypeScript"`
**Expected**: Return memories matching "TypeScript".
**Actual**: Returns "No memories found" (exit 0). There is no TypeScript memory in the store (only 1 task memory exists: "User prefers Go for backend development...").
**Verdict**: PASS (correct behavior given empty datastore for that query).

### Test 2b: `memory search "Go"`
**Expected**: Return the existing Go preference memory.
**Actual**: Returns 1 task memory with content "User prefers Go for backend development..." and relevance score **0.00**.
**Verdict**: PASS (search works) but ISSUE: relevance score is always 0.00.

### Test 2c: `memory search "Go backend"`
**Expected**: Return the Go preference memory.
**Actual**: Returns "No memories found". Only "Go" works as a search term.
**Verdict**: ISSUE -- Multi-word queries can fail with LIKE fallback since the pattern is `%Go backend%` but the stored content has "Go for backend" (not "Go backend").

### Test 3: `memory recent`
**Expected**: CLI subcommand to list recent memories.
**Actual**: Treated as a search query for "recent" -> "No memories found". No `recent` subcommand exists.
**Verdict**: FAIL -- CLI lacks `recent` subcommand.

### Test 4: `memory get-context`
**Expected**: CLI subcommand to get contextually relevant memories for a query.
**Actual**: Treated as a search query for "get-context" -> "No memories found". No `get-context` subcommand exists.
**Verdict**: FAIL -- CLI lacks `get-context` subcommand.

**Note**: `MemoryGetContextTool` exists at `internal/tools/builtin/memory.go:203` as an **agent tool** for runtime use, not a CLI command.

### Test 5: `memory consolidate`
**Expected**: CLI subcommand to trigger memory consolidation.
**Actual**: `consolidate` treated as search query -> "No memories found". No `consolidate` subcommand exists.
**Verdict**: FAIL -- CLI lacks `consolidate` subcommand.

**Note**: `Manager.Consolidate()` exists at `internal/memory/manager.go:898` and is wired into the scheduler at `internal/scheduler/jobs.go`. Also exists at `internal/selfimprove/learning.go:605` (LearningPipeline.Consolidate). Not exposed via RPC or CLI.

### Test 6: Personality loaded
**Expected**: Personality profile loaded from daemon logs.
**Actual**: Daemon log confirms:
```
level=INFO msg="Loaded personality profile" component=memory subsystem=personality path=/Users/caimlas/.meept/memory/personality/personality.md
level=INFO msg="Personality model loaded" component=memory
```
Personality file exists at `~/.meept/memory/personality/personality.md` (436 bytes, generic profile).
**Verdict**: PASS.

### Test 7: Duplicate detection
**Expected**: Storing the same memory twice should produce a duplicate (since no store command exists for this test).
**Verdict**: SKIPPED -- Cannot test because `memory store` CLI subcommand does not exist.

**Note**: The memory manager has duplicate detection capability via `SQLiteFTSStore.FindDuplicateGroups()` at `internal/memory/ftstore.go` (used by TaskMemory.FindDuplicates()), but it's only callable internally, not via CLI.

### Test 8: `memory graph status`
**Expected**: CLI subcommand to show knowledge graph status.
**Actual**: Treated as a search query -> "No memories found". No `graph` subcommand exists.
**Verdict**: FAIL -- CLI lacks `graph status` subcommand.

**Note**: `KnowledgeGraph.GetStats()` exists at `internal/memory/graph.go:995` and returns `*GraphStats` with NodeCount, EdgeCount, CommunityCount, AvgDegree, LastUpdated. The graph DB exists at `~/.meept/memory/graph/graph.db` with 0 edges (empty graph -- no edge creation has been triggered yet).

### Test 9: FTS5 warning (confirmed)
**Expected**: Daemon log shows FTS5 unavailable warning.
**Actual**: Confirmed. Every daemon restart logs:
```
level=WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=episodic error="no such module: fts5"
level=WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=task error="no such module: fts5"
```
This is a known issue documented in prior runs (#0002, #0043).
**Verdict**: CONFIRMED KNOWN ISSUE.

## Summary of Issues

### Issue 1: FTS5 unavailable (CRITICAL for search quality)
- **Location**: `internal/memory/ftstore.go:121`, `internal/memory/episodic.go:208-217`, `internal/memory/task.go:208-221`
- **Symptom**: Every query falls back to LIKE-based search. All relevance scores are 0.00 because LIKE queries don't return rank columns.
- **Root cause**: System SQLite built without FTS5 module.
- **Severity**: HIGH -- affects all search quality; relevance scores are meaningless.

### Issue 2: Relevance scores always 0.00 (secondary to FTS5)
- **Location**: `internal/memory/ftstore.go:456` + `pkg/sqlite/fts5.go:190`
- **Symptom**: `NormalizeRank(0.0)` returns `0.0` (line: `if rank >= 0 { return 0.0 }`).
- **Impact**: Users cannot distinguish good matches from bad ones in UI.
- **Severity**: MEDIUM (would self-resolve once FTS5 is available).

### Issue 3: Missing CLI subcommands for memory operations (HIGH)
- **Location**: `cmd/meept/memory.go`
- **Missing subcommands**: `store`, `recent`, `get-context`, `consolidate`, `graph`, `export`
- **Only available**: `memory [query]` (free-text search with `-n` limit flag)
- **RPC layer**: Has handlers for `memory.query`, `memory.recent`, `memory.export` but:
  - CLI doesn't call `memory.recent` (treats args as search query)
  - No CLI calls `memory.export`
- **Backend**: `Manager.Search()`, `Manager.GetRecent()` work correctly via RPC.
- **Severity**: HIGH -- core memory operations inaccessible to users.

### Issue 4: Knowledge graph empty
- **Location**: `internal/memory/graph.go`, `~/.meept/memory/graph/graph.db`
- **Symptom**: 0 edges, 0 nodes in memory graph.
- **Root cause**: Graph edge creation (`CreateTemporalEdges`, `CreateSimilarityEdges`) requires explicit triggers via agent tools or scheduler jobs, none of which have fired for the minimal test data.
- **Severity**: LOW (feature partially implemented but not auto-triggered).

### Issue 5: Personality profile generic
- **Location**: `~/.meept/memory/personality/personity.md`
- **Content**: Generic "General-purpose assistance" with no user preferences recorded.
- **Note**: "Creator Preferences" section is empty despite user having preferences (TypeScript, Go). Personality learning may not be wired to persist to file.

## Backend RPC Capabilities Present (but not reachable via CLI)

| RPC Method | Backend Support | CLI Exposure |
|---|---|---|
| `memory.query` | Yes (handler.go:77) | Yes (only one that works) |
| `memory.recent` | Yes (handler.go:120) | No |
| `memory.export` | Yes (handler.go) | No |
| `memory.store` (implicit via agent tools) | Yes (`memory_store` tool) | No |
| `memory.get-context` (agent tool) | Yes (`memory_get_context` tool) | No |
| `memory.consolidate` (agent tool) | Yes (`memory_consolidate` tool) | No |
| `memory.graph.*` | Yes (`KnowledgeGraph` API) | No |

## Files Modified/Referenced

- `/Users/caimlas/git/meept/cmd/meept/memory.go` -- only search CLI, missing subcommands
- `/Users/caimlas/git/meept/internal/memory/handler.go` -- backend RPC handlers
- `/Users/caimlas/git/meept/internal/memory/manager.go` -- core memory manager
- `/Users/caimlas/git/meept/internal/memory/ftstore.go` -- FTS5 fallback logic
- `/Users/caimlas/git/meept/internal/memory/episodic.go` -- episodic memory search
- `/Users/caimlas/git/meept/internal/memory/task.go` -- task memory search
- `/Users/caimlas/git/meept/internal/memory/graph.go` -- knowledge graph
- `/Users/caimlas/git/meept/internal/tools/builtin/memory.go` -- agent memory tools
- `/Users/caimlas/git/meept/pkg/sqlite/fts5.go` -- FTS5 utilities
