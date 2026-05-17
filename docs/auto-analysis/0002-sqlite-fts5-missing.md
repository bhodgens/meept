# SQLite FTS5 Not Available - Memory Search Degrades

**Date**: 2026-05-15
**Phase**: 0 (prerequisite setup)
**Severity**: medium
**Component**: `internal/memory/ftstore.go`

## Description

The Go SQLite driver used by meept does not include FTS5 support. Both episodic and task memory subsystems fall back to LIKE-based search, which is significantly slower and less accurate for natural language queries.

## Reproduction

1. Start the meept daemon
2. Observe logs:
```
level=WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=episodic error="no such module: fts5"
level=WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=task error="no such module: fts5"
```

## Evidence

```
level=INFO msg="FTS store initialized" component=memory subsystem=episodic table=episodic_memories.db fts5=false
level=INFO msg="FTS store initialized" component=memory subsystem=task table=task_memories.db fts5=false
```

## Root Cause

The project uses Go's `database/sql` with a SQLite driver that is compiled without FTS5. The `ftstore.go` code correctly detects this and falls back, but the fallback is a LIKE-based search that won't support proper full-text search semantics (stemming, ranking, phrase matching).

This could be because:
1. The `mattn/go-sqlite3` driver is used without the `_tags=fts5` build tag
2. Or `modernc.org/sqlite` is used, which may not bundle FTS5 by default

## Proposed Fix

1. Check which SQLite driver is in use (`go.mod`)
2. If `mattn/go-sqlite3`: add `-tags=fts5` to build commands in Makefile
3. If `modernc.org/sqlite`: check if FTS5 is available with a different build tag or version
4. Add FTS5 availability check to `go test` CI

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
