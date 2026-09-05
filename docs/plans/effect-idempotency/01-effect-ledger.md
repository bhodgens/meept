# internal/effects Ledger Package - Implementation Leaf

## DISPATCH INSTRUCTION

> You are the implementer for this leaf. Work ONLY from this document.
> Implement ALL tasks below using TDD (test first, watch it fail, implement,
> watch it pass). Do NOT commit. Do NOT run git add. The orchestrator handles
> all git operations after review.
> Do NOT use read_file on existing source files — explore with search_files
> or terminal cat. Never pipe read_file output into write_file
> (line-number corruption rule: `NN|` prefixes must never reach a source
> file). After writing a file, do NOT read it back to verify — write once
> and stop. After completing, report what you built, what files you touched,
> test results, and any deviations from the spec.

## Scope

The new package `internal/effects`: the effect ledger interface, the
deterministic key helper, the SQLite-backed store over
`effects_ledger`, an in-memory implementation for tests, and table-driven
tests including the double-claim race test. NOTHING else: no tool wiring, no
daemon construction, no CLI, no RPC (those are leaves 02/03).

**Files (all new; exactly 3 non-test files + 3 test files):**
- Create: `internal/effects/effects.go` (interface, types, errors)
- Create: `internal/effects/key.go` (EffectKey)
- Create: `internal/effects/ledger_sqlite.go` (SQLite store)
- Create: `internal/effects/ledger_memory.go` (in-memory store)
- Create: `internal/effects/key_test.go`
- Create: `internal/effects/effects_test.go` (interface-conformance table)
- Create: `internal/effects/ledger_sqlite_test.go` (double-claim race,
  persistence, state machine)

**Test command:** `go test -p 2 ./internal/effects/...`
(macOS ephemeral-port rule: always `-p 2`, in every invocation including
`-race` runs.)

## Dependencies

None. This is the root leaf. Everything downstream (02, 03) compiles against
this package.

## Estimated context

~70K

## Interface Contract (From Parent)

Implement EXACTLY these pinned signatures (from master.md Contracts 1-4).
Do not rename, re-order, or add parameters. `Abandon` and `Get` are part of
the pinned surface.

```go
package effects

type EffectState string

const (
    StateClaimed   EffectState = "claimed"
    StateReceipted EffectState = "receipted"
    StateCompleted EffectState = "completed"
    StateAbandoned EffectState = "abandoned"
)

type EffectMeta struct {
    TaskID             string          `json:"task_id,omitempty"`
    StepID             string          `json:"step_id,omitempty"`
    SessionID          string          `json:"session_id,omitempty"`
    Tool               string          `json:"tool"`
    ProviderIdempotent bool            `json:"provider_idempotent"`
    Payload            json.RawMessage `json:"payload,omitempty"`
}

type EffectRecord struct {
    Key                string          `json:"key"`
    State              EffectState     `json:"state"`
    TaskID             string          `json:"task_id"`
    StepID             string          `json:"step_id"`
    SessionID          string          `json:"session_id"`
    Tool               string          `json:"tool"`
    ProviderIdempotent bool            `json:"provider_idempotent"`
    ClaimedAt          time.Time       `json:"claimed_at"`
    ExecutedAt         *time.Time      `json:"executed_at,omitempty"`
    CompletedAt        *time.Time      `json:"completed_at,omitempty"`
    Payload            json.RawMessage `json:"payload,omitempty"`
    Receipt            json.RawMessage `json:"receipt,omitempty"`
}

type Ledger interface {
    Claim(ctx context.Context, key string, meta EffectMeta) (granted bool, prior *EffectRecord, err error)
    RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error
    Complete(ctx context.Context, key string) error
    Abandon(ctx context.Context, key string, reason string) error
    ReconcilePending(ctx context.Context) ([]EffectRecord, error)
    Get(ctx context.Context, key string) (*EffectRecord, error)
    Close() error
}

var ErrUnknownKey = errors.New("effects: unknown effect key")
var ErrInvalidTransition = errors.New("effects: invalid state transition")

// EffectKey derives a stable, deterministic key: sha256 hex over the UTF-8
// bytes of strings.Join(normalized(parts), "\x1f") where normalized(p) =
// strings.TrimSpace(p). 64 hex chars. No randomness, no clock reads.
func EffectKey(parts ...string) string

// Run wraps one external effect in the pinned protocol:
// Claim -> execute -> RecordReceipt. Returns (receipt, reused, err);
// reused=true means a completed prior existed, execute was NOT invoked,
// and receipt is the prior completed receipt (idempotent no-op path).
// Callers then invoke Complete themselves (leaf 02's tools do).
// On execute error the record stays claimed (reconcile finds it); Run does
// NOT abandon automatically.
func Run(ctx context.Context, l Ledger, key string, meta EffectMeta,
    execute func(ctx context.Context) (json.RawMessage, error)) (receipt json.RawMessage, reused bool, err error)
```

**State machine (enforced by RecordReceipt/Complete/Abandon):**

```
claimed --RecordReceipt--> receipted --Complete--> completed
claimed --Complete-------> completed          (claimed completion allowed:
                                               effect ran but crashed before
                                               receipt could be recorded)
claimed --Abandon---------> abandoned
receipted --Abandon--------> abandoned
completed --any------------> ErrInvalidTransition (Complete is idempotent:
                                               re-Complete of completed is nil)
abandoned --any------------> ErrInvalidTransition
unknown key ---------------> ErrUnknownKey
```

`ReconcilePending` returns records in state `claimed` or `receipted` only,
ordered oldest `claimed_at` first.

**SQLite schema** (master.md Contract 3, verbatim — DSN
`dbPath+"?_journal_mode=WAL&_busy_timeout=5000"`, driver
`modernc.org/sqlite`; verify the import name actually used in
`internal/agent/park_store_sqlite.go` before coding).

### What This Leaf Consumes

```
// stdlib only + the sqlite driver. No internal package dependencies.
// This package MUST NOT import internal/agent, internal/services,
// internal/backup, internal/daemon, or internal/rpc (dependency direction:
// they import internal/effects).
```

## Context

This package is the mechanism the 'Graph and Loop Engineering using Grok Bot'
article calls the "atomic claim of a stable effect key." Everything about it
is boring on purpose: one table, one PK, four transitions. The subtleties the
tests must pin:

1. **Atomicity comes from the PRIMARY KEY, not from check-then-insert.**
   `Claim` is a plain `INSERT`; on constraint violation it reads the prior
   row and returns `(false, prior, nil)`. Under `-race` with N goroutines
   claiming the same key, exactly one gets `granted=true`.

2. **Receipts/payloads are preserved byte-for-byte.** Store
   `json.RawMessage` as TEXT verbatim; return it verbatim. Do not
   unmarshal/re-marshal (downstream consumers may compare bytes).

3. **Keys are content-derived.** `EffectKey` must be a pure function — same
   parts, same key, across processes and restarts. Never call `pkg/id`
   or read the clock inside it.

4. **In-memory implementation exists for tool tests** (leaf 02) so
   `PushService`/backup tests can run without a database. It must satisfy
   the same `Ledger` interface and pass the SAME conformance tests — write
   the conformance suite once, table it over both constructors.

Conventions to follow (verified in this repo):
- DSN + schema + constructor style: `internal/agent/park_store_sqlite.go`
  lines 90-150 (`parkedTurnsSchema` const, `sql.Open("sqlite", ...)`,
  `logger.Info("... initialized", "path", ...)`).
- RFC3339Nano TEXT timestamps (same as `chatPersistenceKeyTimeFormat` there).
- Table-driven tests with `t.Run` subtests:
  `internal/agent/quota_resume_test.go` style.
- slog structured logging; `fmt.Errorf("...: %w", err)` wrapping; no ignored
  errors; two-value type assertions.

## Tasks

### Task 1: Types, errors, interface (TDD: conformance test first)

**Objective:** `internal/effects/effects.go` compiles with the pinned
interface; both stores satisfy it.

**Files:**
- Test: `internal/effects/effects_test.go`
- Create: `internal/effects/effects.go`

**Steps:**

1. Write `effects_test.go` first: a conformance table driving BOTH
   constructors through the full state machine:

```go
func newTestLedgers(t *testing.T) map[string]func() effects.Ledger {
    t.Helper()
    dir := t.TempDir()
    return map[string]func() effects.Ledger{
        "memory": func() effects.Ledger { return effects.NewMemoryLedger() },
        "sqlite": func() effects.Ledger {
            l, err := effects.NewSQLiteLedger(filepath.Join(dir, "effects.db"), testLogger())
            if err != nil { t.Fatalf("open sqlite ledger: %v", err) }
            return l
        },
    }
}

func TestLedgerConformance(t *testing.T) {
    for name, newLedger := range newTestLedgers(t) {
        t.Run(name, func(t *testing.T) {
            l := newLedger()
            defer l.Close()
            // subtests: claim grants once; double-claim returns prior;
            // receipt transitions; complete; abandon; unknown key;
            // invalid transitions; reconcile ordering + filtering.
        })
    }
}
```

2. Declare the pinned types/errors/interface verbatim from the contract in
   `effects.go`. `Run` also lives here.

3. Run `go test -p 2 ./internal/effects/...` — expect compile failure
   (stores missing).

### Task 2: EffectKey (TDD)

**Objective:** deterministic canonical hashing.

**Files:**
- Test: `internal/effects/key_test.go`
- Create: `internal/effects/key.go`

**Table cases (minimum):**

| parts | property |
|---|---|
| `("backup.git_push", "sess-1", "step-2", "abc123")` | stable: same call twice = same key |
| `("ab", "c")` vs `("a", "bc")` | DIFFERENT keys (`\x1f` join) |
| `(" tool ", "sess")` vs `("tool", "sess")` | SAME key (trim normalization) |
| `()` | valid: sha256 of empty string, 64 hex chars |
| any | output matches `hex.EncodeToString(sha256.Sum256(joined))` exactly |
| any | 64 chars, lowercase hex |

Implementation is ~10 lines: trim each part, `strings.Join(parts, "\x1f")`,
`sha256.Sum256`, `hex.EncodeToString`. No randomness, no clock, no `pkg/id`.

### Task 3: In-memory ledger (TDD)

**Objective:** `NewMemoryLedger()` passing the conformance suite.

**Files:**
- Create: `internal/effects/ledger_memory.go`

**Requirements:**

- `type MemoryLedger struct { mu sync.Mutex; records map[string]EffectRecord }`
  — lock ONLY around map access; never across any call that could do I/O
  (mutexio).
- `Claim` under lock: if key exists return `(false, copyOfPrior, nil)`;
  else insert claimed record with `ClaimedAt: time.Now().UTC()` and return
  `(true, nil, nil)`.
- Return DEFENSIVE COPIES of records (callers mutating a returned record
  must not corrupt the store). For `json.RawMessage` fields, copy the byte
  slice.
- Transitions enforced identically to the SQLite store (one shared
  transition-validation helper in `effects.go` used by both stores keeps
  them honest).

### Task 4: SQLite ledger (TDD)

**Objective:** `NewSQLiteLedger(dbPath string, logger *slog.Logger)` passing
the conformance suite + persistence tests.

**Files:**
- Test: `internal/effects/ledger_sqlite_test.go`
- Create: `internal/effects/ledger_sqlite.go`

**Requirements:**

- Schema const `effectsLedgerSchema` exactly as master.md Contract 3
  (CREATE TABLE IF NOT EXISTS + state CHECK constraint + state index).
- `sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")`,
  migrate in the constructor, `logger.Info("effects ledger initialized",
  "path", dbPath)` — mirror `park_store_sqlite.go:126-139`.
- `Claim` = single `INSERT`; on unique-constraint error
  (`errors.Is`-safe detection: check the error string for "UNIQUE constraint
  failed" OR pre-check with a `SELECT` only AFTER insert failure — never a
  bare check-then-insert), `SELECT` the prior row, return
  `(false, prior, nil)`.
- Times stored as RFC3339Nano TEXT; nullable `executed_at`/`completed_at`
  scan via `sql.NullString`.
- `RecordReceipt`: `UPDATE ... SET state='receipted', receipt=?, executed_at=?
   WHERE key=? AND state='claimed'`; `RowsAffected()==0` → re-read the row
   and return `ErrUnknownKey` / `ErrInvalidTransition` as appropriate.
- `Complete`: `UPDATE ... SET state='completed', completed_at=? WHERE key=?
   AND state IN ('claimed','receipted')`; `RowsAffected()==0` → re-read:
   already-completed returns nil (idempotent), otherwise transition error.
- `Abandon`: `UPDATE ... SET state='abandoned' WHERE key=? AND state IN
   ('claimed','receipted')`; same error mapping.
- `ReconcilePending`: `SELECT ... WHERE state IN ('claimed','receipted')
   ORDER BY claimed_at ASC`.
- Row scanning goes through one `scanEffectRecord` helper used by Get,
  Claim-prior, and ReconcilePending.

**Persistence test (tempdir, not `:memory:`):**

```go
func TestSQLiteLedgerPersistenceAcrossReopen(t *testing.T) {
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "effects.db")
    l1, _ := effects.NewSQLiteLedger(dbPath, testLogger())
    // claim + receipt one effect, leave a second in claimed
    l1.Close()
    l2, err := effects.NewSQLiteLedger(dbPath, testLogger())
    // ReconcilePending on l2 returns BOTH records with identical payloads.
}
```

### Task 5: Double-claim race test (TDD)

**Objective:** prove PK-conflict atomicity under concurrency.

**Files:** `internal/effects/ledger_sqlite_test.go` (append)

```go
func TestSQLiteLedgerDoubleClaimRace(t *testing.T) {
    l, _ := effects.NewSQLiteLedger(filepath.Join(t.TempDir(), "effects.db"), testLogger())
    defer l.Close()
    const goroutines = 16
    key := effects.EffectKey("race", "sess", "step")
    var wg sync.WaitGroup
    granted := make(chan struct{}, goroutines)
    for i := 0; i < goroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ok, _, err := l.Claim(context.Background(), key,
                effects.EffectMeta{Tool: "race"})
            if err != nil { t.Errorf("claim: %v", err); return }
            if ok { granted <- struct{}{} }
        }()
    }
    wg.Wait()
    close(granted)
    if n := len(granted); n != 1 {
        t.Fatalf("granted count = %d, want exactly 1", n)
    }
}
```

Run with `go test -p 2 -race ./internal/effects/...`. Same race test for the
memory ledger inside the conformance table.

### Task 6: Run guard (TDD)

**Objective:** `effects.Run` implements Claim → execute → RecordReceipt and
the reuse path.

**Files:** `internal/effects/effects_test.go` (append table)

**Table cases:**

| scenario | expect |
|---|---|
| fresh key, execute returns receipt | receipt stored, `reused=false`, record `receipted` |
| prior completed exists | execute NOT called (sentinel panic/assert), `reused=true`, prior receipt returned |
| prior claimed (stale) exists | claim returns granted=false, prior NOT completed → `Run` returns `ErrInvalidTransition`-wrapped error naming the state; execute NOT called |
| execute returns error | error propagated (`%w`-wrapped), record stays `claimed` (visible to ReconcilePending) |
| execute returns nil receipt, nil error | error: "execute returned nil receipt" — a receipt is mandatory |

Note the third row: `Run` on a `granted=false` claim completes the idempotent
no-op ONLY for completed priors; a claimed/receipted prior means someone else
owns the in-flight effect and `Run` must NOT execute. This is the exact
behavior leaf 02's tools depend on.

### Task 7: gofmt + full package verification

- `gofmt -l internal/effects/` prints nothing.
- `go test -p 2 ./internal/effects/...` green.
- `go test -p 2 -race ./internal/effects/...` green.
- `go vet ./internal/effects/...` clean.

## Self-Verification Checklist

- [ ] `internal/effects` imports NO internal packages (only stdlib + sqlite driver)
- [ ] Pinned signatures match the contract byte-for-byte (method names, param order, return tuples)
- [ ] `Claim` uses INSERT-first, never check-then-insert
- [ ] Receipt/payload JSON round-trips byte-for-byte (test asserts `bytes.Equal`)
- [ ] `EffectKey` pure: no clock, no randomness, no pkg/id
- [ ] Both stores pass the SAME conformance table
- [ ] Double-claim race: exactly one grant across 16 goroutines, `-race` clean
- [ ] Persistence-across-reopen test passes on a tempdir file
- [ ] `go test -p 2 ./internal/effects/...` green (always `-p 2`)
- [ ] `gofmt` clean; no debug prints, no TODOs, no placeholder values
- [ ] No line-number corruption (`NN|` prefixes) in any file

## Review Checklist

- [ ] All tasks implemented; nothing beyond scope (no tool wiring, no CLI)
- [ ] Interface contracts from master.md satisfied exactly
- [ ] Tests written first and passing (TDD followed)
- [ ] Conventions followed: `%w` wrapping, no ignored errors, mutexio-safe,
      slog structured keys, table-driven tests
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder
      values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source
- [ ] Do NOT commit. Do NOT run git add. — confirmed

Report: what you built, files touched, test output summary, deviations.
