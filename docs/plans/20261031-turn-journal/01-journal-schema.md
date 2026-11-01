# Journal Schema + Store - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The turnjournal package skeleton: journal schema, SQLite store,
  and the opt-in config flag (default false).
- **Dependencies:** R0-research.md (cite findings doc where a finding drove a
  decision)
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** master.md Requirements R2, R8

## Goal

Create `internal/agent/turnjournal` with the durable step-ledger store
(`turnjournal.db` over SQLite, parks.db conventions) and the opt-in
`daemon.turn_journal_enabled` config flag (default FALSE). This leaf ships the
package's data layer only: schema, append, queries, close. The loop seam,
rehydrate, and e2e coverage are leaves 02/03/04.

## Context

- This repo is a Go monorepo (meept). SQLite stores follow one house pattern:
  open with `_pragma=` DSN keys (modernc.org/sqlite ignores `_journal_mode=`
  style keys - they are silently dropped), WAL journal mode, 5s busy timeout,
  NORMAL synchronous, a per-statement context timeout, and a best-effort
  construction at daemon wiring (a failed open logs a Warn and the feature
  disables - it never fails the daemon).
- The park store is the template: internal/agent/park_store_sqlite.go.
- Config is JSON5; templates live in config/ and are copied by make install.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. No approximate pointers. -->

- internal/agent/park_store_sqlite.go:117-129 - SQLiteParkStore type +
  NewSQLiteParkStore with the `_pragma=` DSN; the construction/migration
  pattern to mirror.
- internal/agent/park_store_sqlite.go:155 - `opTimeout = 5 * time.Second`
  bounding every store statement.
- internal/agent/park_store_sqlite.go:129 - the store lives over the daemon's
  parks.db; the journal gets its OWN file (turnjournal.db), not a shared
  handle - same rationale as effects.db (internal/daemon/components.go:2610
  area).
- internal/daemon/components.go:2610 - parks.db construction site (best-effort
  open + Warn on failure); leaf 02/03 wire the journal store beside it.
- internal/config/home.go - MeeptHome/MeeptPath resolution; the DataDir config
  already carries the daemon data directory (used by parks.db).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Contract C1 (store) and C5 (config flag), exactly as frozen in ../master.md:
type RecordKind string // "run_started" | "step_started" | "step_completed" | "run_closed"
type Record struct { TurnID, SessionID, StepID string; StepIndex int; Kind RecordKind; ResultPayload []byte; Timestamp time.Time }
type SQLiteJournal struct{ ... }
func NewSQLiteJournal(dbPath string, logger *slog.Logger) (*SQLiteJournal, error)
func (j *SQLiteJournal) Append(ctx context.Context, rec Record) error
func (j *SQLiteJournal) OpenRuns(ctx context.Context) ([]OpenRun, error)
func (j *SQLiteJournal) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error)
func (j *SQLiteJournal) CloseRun(ctx context.Context, turnID string) error
func (j *SQLiteJournal) Close() error
type OpenRun struct { TurnID, SessionID string; TurnPayload []byte }
// plus a small JournalStore interface over these methods (consumers and
// tests program against it).
Schema + DSN exactly as in ../master.md C1.
Config: daemon.turn_journal_enabled bool, JSON5 key turn_journal_enabled
under the daemon block, default false.
```

### What This Leaf Consumes

- None from sibling leaves. Uses stdlib database/sql + the module's existing
  sqlite driver (whichever import parks.db uses - copy it exactly).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Package + schema + open: create the turnjournal package with the `turn_journal` table (columns/keys/CHECK exactly per master C1) and NewSQLiteJournal with the parks DSN form | Create: internal/agent/turnjournal/journal.go, internal/agent/turnjournal/store_sqlite.go; Test: internal/agent/turnjournal/journal_test.go | `go test -p 2 ./internal/agent/turnjournal/ -run TestNewSQLiteJournal -v` | package does not exist | test creates a store over a temp file, queries sqlite_master for `turn_journal`, asserts the CHECK rejects a step_completed row with NULL payload (error expected on insert) | Given a temp db path; When NewSQLiteJournal runs; Then the table exists with the frozen columns and an invalid insert is rejected |
| T2 | Append + read paths: Append (all four kinds), OpenRuns (runs with no run_closed row), CompletedSteps (step_id -> result payload map for step_completed rows), CloseRun (writes the run_closed row), Close | Test: internal/agent/turnjournal/journal_test.go (extend) | `go test -p 2 ./internal/agent/turnjournal/ -v` | methods do not exist | table-driven tests cover: append-then-read roundtrip; OpenRuns returns only dangling runs; CompletedSteps maps only step_completed rows; CloseRun makes a run disappear from OpenRuns | Given a store with seeded rows; When each method runs; Then the documented result set returns; Given op timeout; When the store is busy; Then Append returns a wrapped timeout error, never blocks past 5s |
| T3 | JournalStore interface + nil-safety: extract the small consumer-facing interface; nil receiver methods are safe no-ops returning a descriptive error (turnjournal: not initialized) - parks-store pattern | Create: internal/agent/turnjournal/store.go; Test: internal/agent/turnjournal/journal_test.go (extend) | `go test -p 2 ./internal/agent/turnjournal/ -v` | interface does not exist | a var of type JournalStore holds *SQLiteJournal; calling methods on a nil *SQLiteJournal returns the descriptive error without panicking | Given JournalStore iface; When SQLiteJournal assigned; Then it satisfies the interface; Given nil store; When Append called; Then error "turnjournal: not initialized", no panic |
| T4 | Retention: PruneClosed(ctx, olderThan time.Duration) deletes rows of runs closed before the cutoff (R0 finding F6 - journal GC); wired call sites are leaf 03's concern | Test: internal/agent/turnjournal/journal_test.go (extend) | `go test -p 2 ./internal/agent/turnjournal/ -run TestPruneClosed -v` | method does not exist | seeded old-closed run + fresh-closed run: prune removes only the old one's rows | Given two closed runs at different timestamps; When PruneClosed(7*24h) runs; Then only the older run's rows are gone and OpenRuns is unchanged |
| T5 | Config flag: add `turn_journal_enabled` (bool, default false) to the daemon config block; document it in the config template | Modify: internal/config (daemon config struct file - locate via search_files for the Daemon block), config/meept.json5 (template, if the daemon block is templated there); Test: internal/config (package config test file) | `go test -p 2 ./internal/config/ -run TestTurnJournalFlag -v` | field does not exist | default-constructed config has TurnJournalEnabled == false; a JSON5 snippet with `turn_journal_enabled: true` parses to true | Given a fresh config; When no key is present; Then flag is false (R8/R0-D3); Given the key true; When parsed; Then flag is true |

**Worked example (canonical row expansion for T1):**

1. Write failing test:

```go
func TestNewSQLiteJournal(t *testing.T) {
    dir := t.TempDir()
    j, err := NewSQLiteJournal(filepath.Join(dir, "turnjournal.db"), nil)
    if err != nil { t.Fatalf("open: %v", err) }
    defer j.Close()
    var name string
    err = j.db.QueryRow(
        "SELECT name FROM sqlite_master WHERE type='table' AND name='turn_journal'",
    ).Scan(&name)
    if err != nil || name != "turn_journal" {
        t.Fatalf("table missing: %v %q", err, name)
    }
}
```

2. Verify failure: `go test -p 2 ./internal/agent/turnjournal/ -run TestNewSQLiteJournal -v`
   Expected: FAIL - package does not exist.
3. Minimal implementation: journal.go (types), store_sqlite.go (NewSQLiteJournal
   mirroring internal/agent/park_store_sqlite.go:121-129 DSN + schema exec).
4. Verify pass: same command. Expected: PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C1 signatures, schema, DSN)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify (NO loop wiring, NO
      rehydrate, NO components.go changes in this leaf)
- [ ] Flag defaults false and is respected nowhere else yet (wiring is 02/03)
- [ ] gofmt clean; pure ASCII; `go vet ./internal/agent/turnjournal/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Interface contracts match exactly (signatures, types, paths, schema
      columns and CHECK constraint byte-equal to master C1)
- [ ] DSN uses the `_pragma=` form (a `_journal_mode=`-style key would be
      silently ignored - WAL silently off)
- [ ] Every statement bounded by an op-timeout context
- [ ] Conventions followed; no bugs; no predictable IDs; no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- result_payload is opaque JSON written by leaf 02 (step result blob); this
  leaf treats it as bytes.
- run_started rows carry TurnPayload (the class payload blob) so leaf 03's
  rehydrate can requeue without reconstructing history - mirrors why the park
  store persists turn_payload (internal/agent/parked_turn.go:77-79).
- If the sqlite driver import differs from expectation, copy whatever
  park_store_sqlite.go imports - never introduce a second driver.
