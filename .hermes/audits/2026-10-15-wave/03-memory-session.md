## bughunt Audit: MEMORY + SESSION STORE (2026-10-15)

### Findings

**F1: Backend value "local" missing from switch-case consumers**
- **Severity**: HIGH
- **Location**: `internal/memory/manager.go:222-226`
- **Mechanism**: The uncommitted diff introduces `MemoryBackendLocal = "local"` as the new default. The switch in `Initialize()` handles `"local"` and `"sqlite"` together (both map to `wantSQLite = true`), and `""`/`"memvid"` map to `wantMemvid`. However, external consumers comparing `config.MemoryBackend*` values (e.g., in RPC handlers, test files, or UI sections) may still compare against `"sqlite"` only, missing the new `"local"` value. The UI section (`sections_memory.go:21`) lists `"local"`, `"sqlite"`, `"memvid"` as options, but if any code path does `if backend == "sqlite"` it won't match `"local"`.
- **Test**: Run `grep -rn 'Backend.*==.*"sqlite"' internal/` and check if any consumer misses `"local"`.
- **Evidence**: SOURCE -- `internal/config/schema.go` adds `MemoryBackendLocal`; `internal/memory/manager.go` switch handles it; `internal/configui/sections_memory.go` lists it.
- **Confidence**: medium

**F2: FTS5 query sanitization strips dangerous chars but NOT all FTS5 syntax**
- **Severity**: MEDIUM
- **Location**: `pkg/sqlite/fts5.go:122-135` (`sanitizeTerm`)
- **Mechanism**: `sanitizeTerm` removes `"`, `'`, `*`, `:`, `(`, `)` and control chars. However, FTS5 also treats `NOT`, `-`, and `phrase` quotes specially. A user query like `foo NOT bar` becomes `foo NOT bar` (NOT is not stripped), which is then quoted as `"foo" NOT "bar"` in the MATCH expression -- the bare `NOT` outside quotes is an FTS5 boolean operator, not a literal term. This allows boolean syntax injection.
- **Test**: Call `SanitizeQueryOp("foo NOT bar", "AND")` and verify the output doesn't contain bare `NOT`.
- **Evidence**: LIVE -- `sanitizeTerm` does not strip `NOT`, `-`, or other FTS5 boolean operators.
- **Confidence**: high

**F3: LIKE escape in ftstore.go uses backslash but queries use `ESCAPE '\\'` -- Go raw string literal ambiguity**
- **Severity**: LOW
- **Location**: `internal/memory/ftstore.go:551-556` (`escapeLikeWildcards`)
- **Mechanism**: `escapeLikeWildcards` escapes `\`, `%`, `_` with backslash. The queries in `episodic.go:245` and `task.go:248` use `ESCAPE '\\'` (Go raw string). The raw string `'\\'` is two backslashes to SQLite, meaning the escape character is `\` (first backslash escapes the second). This is correct, but the comment in `episodic.go:237-239` says "ESCAPE '\\' uses a Go raw string literal, so backslash has no special meaning" -- this is misleading; the raw string *does* contain a backslash, which SQLite interprets as the escape char. The code works, but the comment is wrong and could confuse future maintainers.
- **Test**: Search with a user query containing `\%` and verify it's escaped to `\\%` in the LIKE pattern.
- **Evidence**: SOURCE -- `ftstore.go:553` escapes `\` to `\\`; query uses `ESCAPE '\\'`.
- **Confidence**: medium

**F4: rows.Close() missing on error paths in several query locations**
- **Severity**: MEDIUM
- **Location**: `internal/memory/dual_store.go:545-550`, `internal/memory/ftstore.go:296-300`
- **Mechanism**: In `dual_store.go:545`, `rows, err := db.QueryContext(...)` is followed by error check but no `rows.Close()` if err is non-nil. Same pattern in `ftstore.go:296`. While some locations use `defer rows.Close()` (e.g., `task.go:266`), others don't.
- **Test**: Code review of all `rows, err :=` in internal/memory/ to verify Close on every path.
- **Evidence**: SOURCE -- `dual_store.go:545` has no defer or explicit Close on error path.
- **Confidence**: high

**F5: Consolidation tick can run concurrently with itself if StartPeriodicConsolidation called twice**
- **Severity**: LOW
- **Location**: `internal/memory/manager.go:1252-1267` (`StartPeriodicConsolidation`)
- **Mechanism**: `StartPeriodicConsolidation` calls `consolidator.StartPeriodicConsolidation`. The consolidator's internal ticker goroutine is guarded by a stop-once flag, but if `StartPeriodicConsolidation` is called twice with different contexts/intervals, the second call may start a *new* ticker before the first one fully settles. The consolidator's `StartPeriodicConsolidation` uses its own `sync.Once` for the ticker, so duplicate calls are idempotent -- but the manager's `mu.RLock` in `StartPeriodicConsolidation` only checks `initialized` and `consolidator != nil`, not whether the ticker is already running.
- **Test**: Call `StartPeriodicConsolidation` twice in quick succession and check for duplicate ticker goroutines.
- **Evidence**: SOURCE -- manager's `StartPeriodicConsolidation` has no guard against double-start.
- **Confidence**: medium

**F6: Date bucketing in distill_async.go uses LOCAL calendar date but other locations may use UTC**
- **Severity**: LOW
- **Location**: `internal/memory/distill_async.go:370,527`
- **Mechanism**: The L0 distillation frame and day files are named by LOCAL calendar date. However, `handler.go:271,295` stamps timestamps with `time.Now().UTC()`, and `dual_store.go` converts to UTC for storage. If a memory's timestamp is UTC but the consolidation/day-bucket logic expects LOCAL, memories created near midnight UTC may be bucketed to the wrong calendar day.
- **Test**: Create a memory at UTC midnight and check which day frame it lands in.
- **Evidence**: SOURCE -- `handler.go` uses `.UTC()`, `distill_async.go` uses LOCAL.
- **Confidence**: medium

### Checked and Clean
- `internal/session/store.go` -- reviewed; no obvious mutex-I/O violations or rows leaks in the core store paths.
- `internal/memory/ftstore.go` -- FTS5 schema creation and trigger setup reviewed; `escapeLikeWildcards` works correctly though comment is misleading.
- `internal/memory/vector/` -- not directly audited but no backend-selection switch references found.
- `internal/memory/bridges_store.go` -- bridge recall uses `hashBridgeEmbedding` consistently; no hash-space mismatch found.

### Gates run
- `grep -rn 'MemoryBackendLocal|MemoryBackendSQLite|MemoryBackendMemvid' internal/` -- found 20+ references
- `grep -rn 'switch.*Backend' internal/` -- found switch in `manager.go:222`
- `grep -rn 'rows := \|rows, err := ' internal/memory/ internal/session/` -- found 30+ query sites
- `go vet ./internal/memory/...` -- (not run per rules; audit is read-only)

### Not covered
- `internal/session/` deep dive into all query paths for rows.Close() -- time-boxed to memory package primary paths.
- Full concurrency race analysis with `-race` flag -- would require running tests, which is prohibited.
- `internal/config/memory_backend_test.go` -- untracked test file; not executed.
