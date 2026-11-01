# Leaf 02 - Schema Migrations: trace_id/span_id on llm_calls and session_tool_calls

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.**
Read this document fully, then implement every task in order with TDD
(failing pin -> run to confirm failure -> minimal implementation -> run to
confirm pass). Pins are ADDED to existing test files; do not create new
unit test files.

- Parent: `master.md` (contract C3, C9 apply to you)
- Verify command: `go test -p 2 ./internal/metrics/... ./internal/session/... -count=1`
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** inspect existing files with
  `search_files` or `terminal cat` - NEVER pipe `read_file` output into
  `write_file`. After writing a file, do NOT read it back - write once and
  stop. Patch with full verbatim context strings.

## Meta

- **Parent:** master.md
- **Dependencies:** none (leaf 01 is concurrent, group B)
- **Estimated Context:** ~60K
- **Concurrency Group:** B

## Goal

Persist trace correlation at the SQL layer: add `trace_id` + `span_id`
columns to `llm_calls` (metrics.db, registry migration) and
`session_tool_calls` (sessions.db, migrationAddColumn), update the
fresh-database CREATE scripts, every query site, the Go structs, and add
roundtrip pin tests. NO behavior change beyond persistence: aggregations
must stay identical when trace_id is "" (legacy + default rows).

## Dependencies

- None. Leaf 01 is CONCURRENT (group B): it owns the LLMCallRecord field
  half. You own INSERT/SELECT/migrations. If your INSERT gains two new
  placeholders, the compile still works against leaf 01's fields; coordinate
  is NOT needed - the struct fields carry zero values until 01 lands, and
  your roundtrip pins set the fields directly.

## Estimated Context

~60K (two files dominate; migrations are declared-config edits; roundtrip
pins are small).

## Context

Read first:
- `internal/metrics/store.go:240-266` - llm_calls CREATE TABLE (fresh-database
  script; ADD the two columns inline here too).
- `internal/metrics/store.go:345-500` - initSchema -> storeMigrations
  registry (Versions 1-6 exist; next free Version is 7),
  `columnMissingGuard` (:434), `indexMissingGuard` (:441), and the ordering
  rule: an index step must come AFTER its column step (versions 4/6 show
  the pattern).
- `internal/metrics/store.go:757-810` - LLMCallRecord + RecordLLMCall
  INSERT (extend column list + placeholders with trace_id, span_id; leaf 01
  owns stamping the values).
- `internal/metrics/usage.go:65-70`, `internal/metrics/store.go:855-885` -
  existing aggregate SELECTs over llm_calls (must remain untouched - they
  do not project trace columns).
- `internal/session/store_sqlite.go:206-222` - session_tool_calls CREATE
  TABLE; `:142-160` migrationAddColumn usage pattern; `:2234-2330`
  SaveToolCalls/GetToolCalls/GetToolCallsForMessages; `:1830+` ForkSession's
  tool-call copy SELECT at :2017 and INSERT at :2028.
- `internal/session/store.go:29-38` - session.ToolCall struct; `:106` Store
  interface SaveToolCalls signature (unchanged - the struct gains fields).
- Roundtrip-pin prior art: existing store_sqlite_test.go SaveToolCalls tests.

## Verified Anchors

- internal/metrics/store.go:246 - llm_calls CREATE TABLE script
- internal/metrics/store.go:434-442 - columnMissingGuard / indexMissingGuard
- internal/metrics/store.go:460-486 - registry Versions 5 and 6 (next: 7, 8)
- internal/metrics/store.go:757 - LLMCallRecord (leaf 01 adds fields; your
  INSERT must compile against them - write yours defensively so a
  zero-value record inserts "")
- internal/metrics/store.go:778-805 - RecordLLMCall INSERT
- internal/session/store_sqlite.go:206-222 - session_tool_calls schema
- internal/session/store_sqlite.go:142-160 - migrationAddColumn pattern
- internal/session/store_sqlite.go:2234/2271/2306 - SaveToolCalls /
  GetToolCalls / GetToolCallsForMessages
- internal/session/store_sqlite.go:2017/2028 - ForkSession tool-call
  copy SELECT/INSERT column lists
- internal/session/store.go:29-38 - ToolCall struct

## Interface Contracts (From Parent)

### What This Leaf Exposes

C3 (schema + persistence half):

- `llm_calls` columns: `trace_id TEXT NOT NULL DEFAULT ''`,
  `span_id TEXT NOT NULL DEFAULT ''`; registry Version 7
  ("llm_calls.trace_id/span_id", two guarded ALTERs in one step OR one ALTER
  statement with both - follow migrate.ColumnsMissingGuard semantics and
  pin idempotence), Version 8 ("idx_llm_calls_trace",
  `CREATE INDEX IF NOT EXISTS idx_llm_calls_trace ON llm_calls(trace_id)`,
  guarded by indexMissingGuard). Fresh-db CREATE script gains both columns
  inline.
- `session_tool_calls` columns: same two, via `s.migrationAddColumn` ALTERs
  + inline CREATE script columns.
- `session.ToolCall` gains `TraceID string` / `SpanID string` (json
  `trace_id` / `span_id`, omitempty). SaveToolCalls persists; GetToolCalls,
  GetToolCallsForMessages, ForkSession copy SELECT read them back.
- `LLMCallRecord` persistence: RecordLLMCall writes TraceID/SpanID (fields
  themselves are leaf 01's).

### What This Leaf Consumes

- (none external; leaf 01's struct fields land in parallel - zero values
  are valid inputs)

## Tasks

### Task 1: metrics.db migrations + RecordLLMCall persistence

**Files:** Modify `internal/metrics/store.go`; pins in the existing metrics
store test file (grep `internal/metrics/*_test.go` for the migration/
RecordLLMCall tests and extend them).

1. Failing pins: (a) RecordLLMCall with TraceID/SpanID set -> row readable
   back with exact values (add a small query in the test; do NOT add a
   production getter unless one is needed by leaf 04 - prefer test-local
   SQL); (b) migration idempotence: apply schema twice (open, close, reopen
   same DB) -> no error, columns present, defaults "" on pre-existing rows;
   (c) fresh-db CREATE includes the columns (insert without explicit ALTER);
   (d) index idx_llm_calls_trace exists after initSchema on BOTH a fresh db
   and a migrated legacy db (create a legacy-shape db by opening without
   the new columns is not directly possible - instead assert on
   sqlite_master for the index and the migration registry reporting
   versions applied).
2. Implement: inline columns in the CREATE script; registry Version 7
   (column step, guarded) + Version 8 (index step, guarded); extend the
   INSERT statement + placeholders; extend the row write with
   record.TraceID / record.SpanID (zero -> "").
3. Verify: `go test -p 2 ./internal/metrics/... -count=1` -> PASS. Confirm
   existing aggregate tests (QueryLLMCallUsage, usage.go paths) still pass
   UNMODIFIED.

Given a legacy metrics.db without the columns; When initSchema runs twice;
Then Version 7 applies once, the columns exist with "" defaults on old rows,
the index exists, and a second run is a no-op.

### Task 2: sessions.db columns + struct + query sites

**Files:** Modify `internal/session/store.go` (ToolCall struct),
`internal/session/store_sqlite.go`; pins in `internal/session/store_sqlite_test.go`.

1. Failing pins: (a) SaveToolCalls with ToolCall{TraceID: "t32...", SpanID:
   "s16..."} -> GetToolCalls returns exact values; (b)
   GetToolCallsForMessages returns them; (c) ForkSession copies rows WITH
   trace columns intact; (d) legacy row ("" defaults) reads back as "".
2. Implement: struct fields; inline CREATE columns; migrationAddColumn
   ALTERs (both columns); extend SaveToolCalls INSERT + the ForkSession
   INSERT (:2028) and ALL THREE SELECT column lists (:2017, :2277, :2326)
   + their Scan argument lists.
3. Verify: `go test -p 2 ./internal/session/... -count=1` -> PASS (whole
   package, including tree/compaction tests that read tool calls).

Given a saved tool call with trace ids; When forked to a new session; Then
the copied rows carry identical trace_id/span_id.

### Task 3: Cross-package compile + regression gate

1. `go build ./...` -> clean (leaf 01 may or may not have landed; your
   changes must compile against BOTH zero-value and stamped records).
2. `go test -p 2 ./internal/metrics/... ./internal/session/... -count=1` -> PASS.
3. `make mutexio` and `make predid` -> clean.
4. `gofmt -l internal/metrics internal/session` -> empty.

## Self-Verification Checklist

- [ ] Verify commands pass with `-count=1`
- [ ] Fresh-database scripts contain the columns inline (no reliance on ALTER)
- [ ] Migrations are idempotent (registry guard pins prove it)
- [ ] Index created AFTER column step (Version 8 > Version 7)
- [ ] Every SELECT/INSERT on both tables includes the new columns exactly once
- [ ] Aggregation queries (usage.go, QueryLLMCallUsage) untouched
- [ ] `session.ToolCall` json tags exact: `trace_id`, `span_id`, omitempty
- [ ] Legacy rows read back "" everywhere (no NOT NULL violations)
- [ ] make mutexio / predid / gofmt / go vet clean
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] Version 7 and 8 declared exactly as C3; no version numbers skipped or
      reused; registry init panic message untouched
- [ ] RecordLLMCall INSERT extended in the same statement style (no second
      UPDATE round-trip)
- [ ] All three session SELECT column lists + Scan calls updated in step
      (a missed one breaks GetToolCalls at runtime - verify by reading each)
- [ ] ForkSession copy path preserves trace ids
- [ ] No changes outside internal/metrics, internal/session
- [ ] No debug artifacts; no line-number corruption; pins table-driven

Output: APPROVED or specific gaps with file + line references.

## Notes

- Known repo quirk: session store uses `migrationAddColumn` (tolerant,
  pre-registry style) - do NOT introduce the migrate.Registry there; match
  the local pattern (C3 pins this).
- `llm_calls` is append-only by convention; the migration adds DEFAULT ''
  columns - old rows legitimately read "" and every consumer must treat ""
  as "no trace" (this is also how leaf 04 distinguishes legacy rows).
- modernc.org/sqlite is the driver in use; no new dependencies.
