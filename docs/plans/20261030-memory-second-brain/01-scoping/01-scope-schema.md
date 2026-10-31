# Scope Schema + Recall Filter - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The branch gate tests (00T-tests.md)
> ALREADY EXIST and are committed - your job is to make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong gate
> tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add memory.Scope, storage column + metadata key, global backfill, and the additive WithScopeFilter recall option.
- **Dependencies:** 00T-tests.md (gate, committed RED)
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** S1, S2 of branch Test Gate

## Goal

Give every stored memory a scope (project/user/global) and make recall
scope-aware via an additive search option, so callers that do not pass the
option behave byte-identically to today (existing callers compile and pass)
and scoped callers get isolation. Legacy rows backfill to 'global' so no
existing memory disappears.

## Context

- internal/memory is ONE package (~31k LOC). Storage backends relevant here:
  episodic FTS5 store (ftstore.go), fact store (fact_store.go), task store
  (task.go), dual store (dual_store.go). Memory rows carry a Metadata map -
  the scope metadata key works everywhere; the SQLite column exists where a
  queryable column is natural (episodic + fact stores).
- Store migrations in this repo: additive ALTER TABLE with idempotent guards;
  see migrate.go for the established pattern.
- FTS5 external-content tables: adding a plain column to the content table is
  safe; NEVER alter the FTS index table directly.

## Verified Anchors

- internal/memory/types.go:54-61 - AgentID/SessionID/TaskID/BotID attribution
  fields (WRITTEN on store via manager.go:553, NEVER filtered at recall -
  this leaf reconciles with them before adding any new scope storage).
- internal/memory/scoped_manager.go - the only working tag-filter precedent
  (bot_id); WithScopeFilter's pattern reference.
- internal/memory/types.go - Memory struct (add Scope field).
- internal/memory/manager.go - Manager.Search / GetContext entry points
  (variadic option threading).
- internal/memory/ftstore.go + internal/memory/fact_store.go - SQLite
  schemas; ALTER TABLE + backfill sites.
- internal/memory/migrate.go - established migration pattern to follow.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type Scope string
const (
    ScopeProject Scope = "project"
    ScopeUser    Scope = "user"
    ScopeGlobal  Scope = "global"
)
// Memory.Scope Scope `json:"scope,omitempty"`
// column: scope TEXT NOT NULL DEFAULT 'global' (episodic + fact stores)
// backfill: existing rows -> 'global' (the column default covers it; an
// explicit UPDATE ... WHERE scope IS NULL pins the migration)
type SearchOption func(*searchOptions)
func WithScopeFilter(maxScope Scope, projectID, userID string) SearchOption
```

### What This Leaf Consumes

- C1/C2 from ../../master.md (verbatim shapes above are the contract).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Scope type + Memory field + metadata stamping on Store | Modify: internal/memory/types.go, internal/memory/manager.go; Test: gate tests (pre-committed) | `go test -p 2 ./internal/memory/ -run TestScope` | Scope undefined | stores stamp metadata["scope"] from mem.Scope; empty Scope stores as "global" | Given Memory{Scope:"project"}; When Store; Then stored row metadata scope=project |
| T2 | SQLite column + idempotent migration + backfill | Modify: internal/memory/ftstore.go, internal/memory/fact_store.go, internal/memory/migrate.go | `go test -p 2 ./internal/memory/ -run TestScope` | column absent | fresh + legacy dbs both end with scope column, all rows 'global' | Given legacy db (no scope column); When store opens; Then ALTER adds column, backfill=global, integrity_check passes |
| T3 | WithScopeFilter predicate on Search/GetContext | Modify: internal/memory/manager.go (+ searchOptions struct) | `go test -p 2 ./internal/memory/ -run TestScopeFilterMatrix` | WithScopeFilter undefined | matrix: global always visible; project only on project match; user on user match (empty userID = single-user match); no option = TODAY's behavior (no filtering) | Given rows in all three scopes; When Search with each filter combo; Then only matching rows return |
| T4 | Dual-store + task-store option passthrough | Modify: internal/memory/dual_store.go, internal/memory/task.go (if they own Search signatures) | `go test -p 2 ./internal/memory/...` | compile or filter gap | option flows through every Search surface; derived-store (embedding-space) queries receive the same filter | Given a Search via the dual store with WithScopeFilter; Then results obey the same predicate |

## Self-Verification Checklist

- [ ] All gate tests pass GREEN; no gate test modified (`git diff` empty on them)
- [ ] Zero-diff behavior for callers NOT passing the option (existing tests
      untouched and green)
- [ ] Migration idempotent: open-close-open on a legacy db does not duplicate
      or error
- [ ] All files at exact specified paths; no scope creep
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify command]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Gate tests GREEN, untouched
- [ ] Predicate matches C2 exactly (empty userID matches in single-user; no
      option = unfiltered)
- [ ] Migration idempotent; backfill explicit; FTS index table untouched
- [ ] No filter applied when option absent (byte-identical legacy recall)
- [ ] No debug artifacts; gofmt clean

Output: APPROVED or specific gaps with file + line references.

## Notes

- The scope column is defense-in-depth for query-time filtering; the
  metadata key is the portable truth (dual-store/memvid paths read metadata).
  Keep both in sync on write.
- Do NOT touch ScopedMemoryManager (bot_id) - separate mechanism.
- FTS5 prefix-query gotcha if you touch query sanitization: `"term"*` (star
  OUTSIDE quotes) - pkg/sqlite SanitizeQueryOpPrefixes is the reference.
