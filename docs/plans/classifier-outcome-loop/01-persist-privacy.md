# Persist + Privacy - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below using TDD. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back
> to verify — write once and stop.

## Meta

- **Parent:** docs/plans/classifier-outcome-loop/master.md
- **Scope:** dispatch_log schema extension + salted input hashing +
  raw-text removal + error scrubbing. No behavior change outside the
  metrics row.
- **Dependencies:** none
- **Estimated Context:** ~60K
- **Concurrency Group:** A

## Goal

Extend the existing dispatch_log table (internal/metrics/store.go:314)
with the columns the outcome loop needs, stop persisting raw message
text (input_summary), and persist salted hashes + model provenance +
turn numbers instead. This is the headline privacy fix.

## Context

RecordDispatch (internal/agent/dispatcher.go:3015-3085) writes a
DispatchEntry on every dispatch when metricsStore is wired. The table
already exists with session_id/task_id keys and 30-day retention. The
raw input_summary column (first 100 chars of the verbatim user message,
extractSummary dispatcher.go:2665-2672 via handler.go:702) is a privacy
gap this leaf closes.

Key regions (cite-verified):
- store.go:314-328 dispatch_log schema; :344-346 indexes
- store.go:370-397 tolerate-duplicate-column ALTER pattern (llm_calls)
  ; :390-397 documents the index-after-column ordering bug
- store.go:551-560 retention purge list (dispatch_log already included)
- store.go:840-864 DispatchEntry + RecordDispatch INSERT
- dispatcher.go:3015-3085 recordDispatch; :3054-3057 error string
- dispatcher.go:2995-3000 SetMetricsStore (no-op when nil — PRESERVE)
- handler.go:702 extractSummary call site; :853-854 RecordDispatch call

## Interface Contracts (From Parent)

### What This Leaf Exposes

```sql
-- tolerate-duplicate-column ALTERs (pattern: store.go:370-388):
ALTER TABLE dispatch_log ADD COLUMN input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE dispatch_log ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE dispatch_log ADD COLUMN margin REAL;
ALTER TABLE dispatch_log ADD COLUMN turn_no INTEGER NOT NULL DEFAULT 0;
ALTER TABLE dispatch_log ADD COLUMN outcome TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE dispatch_log ADD COLUMN corrected_agent TEXT NOT NULL DEFAULT '';
-- indexes AFTER columns (store.go:390-397 ordering rule):
CREATE INDEX IF NOT EXISTS idx_dispatch_log_hash ON dispatch_log(input_hash);
CREATE INDEX IF NOT EXISTS idx_dispatch_log_outcome ON dispatch_log(outcome);
```

```go
// internal/metrics/store.go — DispatchEntry gains:
InputHash      string   `json:"input_hash" db:"input_hash"`
Model          string   `json:"model" db:"model"`
Margin         *float64 `json:"margin,omitempty" db:"margin"`
TurnNo         int      `json:"turn_no" db:"turn_no"`
Outcome        string   `json:"outcome" db:"outcome"`
CorrectedAgent string   `json:"corrected_agent,omitempty" db:"corrected_agent"`

// internal/metrics/hash.go (NEW):
// HashInput(saltID string, salt []byte, message string) string
//   = hex(SHA-256(saltID || 0x00 || message))[:16]
// LoadOrCreateSalt(configDir string) (saltID string, salt []byte, err error)
//   salt file: <configDir>/classifier_salt (0600, 32 random bytes);
//   saltID file: <configDir>/classifier_salt_id (0600, 16 hex chars).
//   Both created on first use; subsequent calls read.

// internal/agent/dispatcher.go — recordDispatch computes:
//   InputHash via HashInput (message = the raw input string),
//   Model from result/intent provenance (Intent.Model; "" for
//   deterministic doors), TurnNo via COUNT(*)+1 on session rows,
//   Outcome left at default 'pending', InputSummary forced to "".
```

### What This Leaf Consumes

Existing: Store.RecordDispatch, DispatchEntry, extractSummary, the
purge loop. No new dependencies.

## Tasks

### Task 1: Schema migration + indexes

Store.ensureSchema (or equivalent, grep the CREATE TABLE dispatch_log
site): add the six tolerate-duplicate ALTERs, then the two indexes
AFTER columns exist (ordering rule). Test: open a fresh DB, insert via
RecordDispatch, SELECT the new columns; open the SAME db file again
(idempotent second migration).

### Task 2: DispatchEntry + INSERT extension

Extend the struct (db tags) and the INSERT column list. Keep the
HasPartsInt conversion pattern (:850-851). Test: round-trip a record
with all new fields; verify margin NULL for a non-Door-1 entry (nil
pointer) and a float for Door 1.

### Task 3: Salt management (hash.go)

LoadOrCreateSalt per the contract. Test: first call creates files with
0600; second call returns identical salt; corrupt/short salt file ->
regenerate (log warn).

### Task 4: recordDispatch writes hash/model/turnNo; input_summary ""

- Compute InputHash via HashInput; hash is of the FULL raw input
  string (not the summary).
- Model: copy from the DispatchResult's Intent.Model (already exists,
  dispatcher.go:110-117). Empty for deterministic branches — that is
  correct provenance.
- TurnNo: query COUNT(*) WHERE session_id=? then +1. (Session rows are
  bounded by 30-day retention; the COUNT is index-backed. Acceptable
  at meept traffic volumes.)
- InputSummary: force "" in the entry (the extractSummary call at
  handler.go:702 stays — the summary is still used for the LOG LINE;
  it just stops reaching the DB).
- Error scrubbing: truncate dispatchErr.Error() to 200 chars; drop the
  error text entirely if it matches a key/token shape
  (regexp: `(?:sk-|api[_-]?key|bearer|token)[^\s]{8,}` case-insensitive
  on a lowercase copy) — replace with "scrubbed".

Test: recordDispatch with metricsStore wired — row has 16-hex
input_hash, empty input_summary, turn_no increments across two calls
in one session; error case with "Bearer abc123def456..." stores
"scrubbed". Also: metricsStore nil -> no panic, no row.

### Task 5: Salt wiring in daemon

Where SetMetricsStore is called (daemon components), load the salt once
and hand it to the dispatcher (new field or setter:
SetInputHasher(fn func(message string) string) — dispatcher stays
crypto-agnostic; the daemon owns the salt). Nil hasher -> InputHash ""
(preserves the no-op-when-unwired invariant).

## Self-Verification Checklist

- [ ] Migration idempotent (fresh DB + reopen both fine)
- [ ] input_summary ALWAYS "" in new rows; old rows keep their values
- [ ] Hash = 16 hex chars, salted; salt files 0600
- [ ] Error scrubbing active; 200-char cap
- [ ] metricsStore nil -> no-op preserved
- [ ] Tests mirror llm_calls_test.go patterns; purge unaffected
- [ ] gofmt/vet clean; ASCII; no TODOs

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** document any.

## Review Checklist (For Review Agent)

- [ ] Schema delta matches Contract 1 exactly (column names/types/order)
- [ ] Indexes created after columns
- [ ] input_summary removal complete (INSERT no longer writes it)
- [ ] Salt lifecycle per contract; no salt in git
- [ ] recordDispatch nil-safety preserved
- [ ] Tests: migration idempotency, hash determinism, turn_no
      increment, error scrub, nil-store no-op

Output: APPROVED or specific gaps.

## Notes

- The dispatcher must stay crypto-agnostic: it calls a hasher function
  injected by the daemon. This keeps internal/agent free of salt/file
  I/O and keeps the multi-user disabled path untouched.
- turn_no via COUNT(*) is deliberately simple; if it ever shows up in
  profiles, a per-session counter table is the follow-up (not now).
- design.md S1/S4 are the authoritative sections.
