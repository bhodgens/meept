# FTS5 Not Available - Memory Uses Slower LIKE-Based Search
**Date**: 2026-05-15
**Phase**: 0
**Severity**: medium
**Component**: internal/memory/ftstore.go
**Evaluation Dimension**: efficiency

## Description
SQLite is compiled without FTS5 support, causing the memory subsystem to fall back to LIKE-based search. This affects both episodic and task memory subsystems and degrades search performance, especially as the memory store grows.

## Reproduction
Start the daemon and observe the startup logs:
```
WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=episodic error="no such module: fts5" hint="Install SQLite with FTS5 support for better search performance"
WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=task error="no such module: fts5" hint="Install SQLite with FTS5 support for better search performance"
```

## Evidence
From daemon startup output:
```
time=2026-05-16T00:19:57.216-06:00 level=WARN msg="FTS5 not available, using LIKE-based search (slower)" component=memory subsystem=episodic error="no such module: fts5" hint="Install SQLite with FTS5 support for better search performance"
time=2026-05-16T00:19:57.216-06:00 level=INFO msg="FTS store initialized" component=memory subsystem=episodic table=episodic_memories path=/Users/caimlas/.meept/memory/episodic/episodic_memories.db fts5=false
```

The `fts5=false` flag in the initialization log confirms FTS5 is disabled.

## Root Cause
The Go SQLite driver (likely `modernc.org/sqlite` or `mattn/go-sqlite3`) is compiled without FTS5 module support. This is a build-time configuration issue.

## Impact on Platform Quality
- Memory search is slower, especially with large stores
- No full-text ranking, reducing search quality
- LIKE search cannot handle stemming, tokenization, or relevance scoring
- Will degrade progressively as memory accumulates

## Proposed Fix
1. Build with a SQLite driver that includes FTS5 (e.g., `mattn/go-sqlite3` with `CGO_ENABLED=1` and FTS5 build tags, or use `sqlite-vec` extension)
2. Alternatively, use `modernc.org/sqlite` which includes FTS5 by default in recent versions
3. Add a build-time check or CI test that verifies FTS5 availability

## Classification
[ ] Harness bug  [ ] Model quality issue  [ ] Communication issue  [x] Efficiency issue  [ ] Design gap  [ ] Both
