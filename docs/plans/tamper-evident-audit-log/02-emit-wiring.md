# Leaf 02 — Emitter Wiring (AuditStore hook, gate emits, payload redactor)

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.** Read
this document fully, then implement every task in order with TDD (failing
test → run to confirm failure → minimal implementation → run to confirm pass).

- Parent: `master.md` (contracts C1, C4, C5, C8 apply to you)
- Prerequisite: leaf 01 is COMPLETE — `internal/auditlog` exists with
  `Record`, `OpenStoreFromDB`, `Migrate`, `Append`, `Head` (C1/C3 signatures).
- Test command: `go test -p 2 ./internal/auditlog/...` plus the filtered
  employee-package commands given per task.
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** when inspecting existing files use
  `search_files` or `terminal cat` — NEVER pipe `read_file` output into
  `write_file` (the `N|` prefixes corrupt source). For surgical edits use
  the `patch` tool with unique old/new strings; after writing, do NOT read
  the file back to verify.

## Scope

Wire the hash chain into the existing employee audit surface:

1. `AuditStore` gains a `ChainEmitter` hook fired after every successful
   finding INSERT — this chains PostTurnAuditor, PeriodicAuditor
   (`scheduler_jobs.go:396`), and `manager.go:1317/1544` emissions for free,
   because they all call `AuditStore.Create`.
2. Goal quality-gate results (NOT findings) are emitted directly from
   `internal/employee/goal_loop.go` where `RunGate` returns
   (gate.go alias; call site at goal_loop.go:1014).
3. `SanitizePayload` redactor in `internal/auditlog/redact.go` (lives with
   the package that owns records; uses `internal/security`'s output monitor).
4. Construction wiring in `internal/employee/wiring.go` (shared-DB and
   standalone paths).

No CLI, no docs, no anchoring — leaf 03.

## Dependencies

- Leaf 01 COMPLETE.
- Files read (search_files / cat, never read_file→write_file):
  `internal/employee/enforcement.go` (AuditStore ~:1685, Create :1743),
  `internal/employee/wiring.go:150-185`, `internal/employee/goal_loop.go`
  (RunGate call ~:1014, auditStore.Create ~:1414), `internal/gate/gate.go`
  (GateResult :62), `internal/security/sanitizer.go:445`
  (OutputMonitor.DetectAndRedact).

## Estimated context

~90K (contracts ~6K; reading 5 existing files ~15K; writing ~800 lines across
4 source + 3 test files; TDD cycles in two packages).

## Interface Contract

Implement EXACTLY (frozen in master.md C4/C5):

```go
// internal/auditlog/redact.go
func SanitizePayload(p map[string]any) map[string]any

// internal/employee/enforcement.go (additions)
type ChainEmitter interface {
    Emit(rec auditlog.Record) error
}
func (s *AuditStore) SetChainEmitter(e ChainEmitter) // nil-guarded setter
func (s *AuditStore) ChainDB() *sql.DB

// internal/employee/goal_loop.go — emit helper on GoalLoop
func (l *GoalLoop) emitGateResult(ctx context.Context, goalID string, res gate.GateResult, duration time.Duration)
```

Emit-failure policy (frozen): log Warn, never fail the finding INSERT or the
gate flow. Record `Type` enum: `audit_finding` | `gate_result`. Payload keys
per master.md C5. Gate records carry `output_sha256`, never `Output` text.

`*auditlog.Store` satisfies `ChainEmitter` by adding this thin wrapper in
leaf 02 (file `internal/auditlog/emit.go`):

```go
// Emit implements employee.ChainEmitter. It maps the record through the
// store's Append and returns the error. Kept in the auditlog package so
// employee never depends on concrete Store construction details.
func (s *Store) Emit(rec Record) error {
    _, err := s.Append(context.Background(), rec)
    return err
}
```

## Tasks

### Task 1: redact.go — SanitizePayload

**Files:** Create `internal/auditlog/redact.go`,
`internal/auditlog/redact_test.go`.

**Step 1 — failing test** (`redact_test.go`):

```go
package auditlog

import "testing"

func TestSanitizePayload_BlocksSecretKeys(t *testing.T) {
    tests := []struct {
        name string
        key  string
    }{
        {"token", "api_token"},
        {"secret", "SECRET_KEY"},
        {"password", "user_password"},
        {"api_key", "api_key"},
        {"apikey", "apikey"},
        {"authorization", "Authorization"},
        {"credential", "aws_credential"},
        {"private_key", "client_private_key"},
        {"bearer", "bearer_token"},
        {"cookie", "session_cookie"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            in := map[string]any{tt.key: "supersecret", "keep": "visible"}
            out := SanitizePayload(in)
            if out[tt.key] != "[redacted]" {
                t.Fatalf("key %q: got %v", tt.key, out[tt.key])
            }
            if out["keep"] != "visible" {
                t.Fatalf("sibling key altered: %v", out["keep"])
            }
            if in[tt.key] != "supersecret" {
                t.Fatal("input map must not be mutated (deep copy)")
            }
        })
    }
}

func TestSanitizePayload_ScrubsCredentialLookingStrings(t *testing.T) {
    in := map[string]any{
        "evidence": "connected with sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ123456 ok",
        "n":        42,
    }
    out := SanitizePayload(in)
    s, ok := out["evidence"].(string)
    if !ok {
        t.Fatalf("evidence not a string: %T", out["evidence"])
    }
    if s == in["evidence"] {
        t.Fatal("credential-looking string must be scrubbed")
    }
    if out["n"] != 42 {
        t.Fatalf("non-string values preserved: %v", out["n"])
    }
}

func TestSanitizePayload_TruncatesLongStrings(t *testing.T) {
    long := make([]byte, 5000)
    for i := range long {
        long[i] = 'a'
    }
    out := SanitizePayload(map[string]any{"evidence": string(long)})
    s := out["evidence"].(string)
    if len(s) > 4200 {
        t.Fatalf("string not truncated: %d bytes", len(s))
    }
    if !hasSuffix(s, "...[truncated]") {
        t.Fatalf("missing truncation marker: %q", s[len(s)-20:])
    }
}

func TestSanitizePayload_RecursesAndHandlesNil(t *testing.T) {
    in := map[string]any{
        "nested": map[string]any{"password": "x", "deep": []any{"tok", 1}},
        "nilly":  nil,
    }
    out := SanitizePayload(in)
    nested := out["nested"].(map[string]any)
    if nested["password"] != "[redacted]" {
        t.Fatalf("nested secret leaked: %v", nested["password"])
    }
    if out["nilly"] != nil {
        t.Fatalf("nil must stay nil: %v", out["nilly"])
    }
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }
```

**Step 2:** `go test -p 2 ./internal/auditlog/... -run TestSanitizePayload`
→ compile FAIL.

**Step 3 — implementation** (`redact.go`):

```go
package auditlog

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"

    "github.com/caimlas/meept/internal/security"
)

// secretKeyFragments is a case-insensitive substring blocklist applied to
// payload KEYS (master.md C4).
var secretKeyFragments = []string{
    "token", "secret", "password", "api_key", "apikey",
    "authorization", "credential", "private_key", "bearer", "cookie",
}

// maxPayloadStringBytes caps any single string VALUE in a record payload.
const maxPayloadStringBytes = 4096

// redactMonitor is the shared output monitor used for value-level
// credential redaction (internal/security/sanitizer.go DetectAndRedact).
// The monitor is stateless for our purposes (Scan+redact per call).
var redactMonitor = security.NewOutputMonitor()

// OutputSHA256 is the only channel gate command output may take into a
// record (master.md C4: never raw command output).
func OutputSHA256(s string) string {
    sum := sha256.Sum256([]byte(s))
    return hex.EncodeToString(sum[:])
}

// SanitizePayload deep-copies p and redacts: (1) secret-fragment keys →
// "[redacted]"; (2) remaining string values scrubbed via
// security.OutputMonitor.DetectAndRedact; (3) string values > 4096 bytes
// truncated with a "...[truncated]" suffix. The input map is never mutated.
func SanitizePayload(p map[string]any) map[string]any {
    out := make(map[string]any, len(p))
    for k, v := range p {
        out[k] = sanitizeValue(k, v)
    }
    return out
}

func isSecretKey(k string) bool {
    lk := strings.ToLower(k)
    for _, frag := range secretKeyFragments {
        if strings.Contains(lk, frag) {
            return true
        }
    }
    return false
}

func sanitizeValue(key string, v any) any {
    if isSecretKey(key) {
        return "[redacted]"
    }
    switch x := v.(type) {
    case map[string]any:
        return SanitizePayload(x)
    case []any:
        out := make([]any, len(x))
        for i, e := range x {
            // Array elements carry no key; only value-level rules apply.
            out[i] = sanitizeElement(e)
        }
        return out
    case string:
        s := x
        if redacted, changed := redactMonitor.DetectAndRedact(s); changed {
            s = redacted
        }
        if len(s) > maxPayloadStringBytes {
            s = s[:maxPayloadStringBytes] + "...[truncated]"
        }
        return s
    default:
        return v
    }
}

func sanitizeElement(v any) any {
    switch x := v.(type) {
    case map[string]any:
        return SanitizePayload(x)
    case []any:
        out := make([]any, len(x))
        for i, e := range x {
            out[i] = sanitizeElement(e)
        }
        return out
    case string:
        s := x
        if redacted, changed := redactMonitor.DetectAndRedact(s); changed {
            s = redacted
        }
        if len(s) > maxPayloadStringBytes {
            s = s[:maxPayloadStringBytes] + "...[truncated]"
        }
        return s
    default:
        return v
    }
}
```

> **Implementer note:** verify `security.NewOutputMonitor()` and
> `DetectAndRedact(text) (string, bool)` by reading
> `internal/security/sanitizer.go:445` first (use `terminal cat` /
> `search_files`, never read_file→write_file). If the constructor needs
> arguments, adjust minimally and note the deviation in your report. If the
> `security` import creates any compile cycle (it should not — verified:
> internal/security imports none of internal/{employee,bot,agent,tools}),
> stop and report instead of duplicating scrub logic.

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS (leaf 01 tests
still green).

### Task 2: enforcement.go — ChainEmitter hook on AuditStore

**Files:** Modify `internal/employee/enforcement.go` (AuditStore struct ~:1685,
Create :1743), Create `internal/employee/enforcement_chain_test.go`.

**Step 1 — failing test** (`enforcement_chain_test.go`):

```go
package employee

import (
    "context"
    "database/sql"
    "path/filepath"
    "testing"
    "time"

    "github.com/caimlas/meept/internal/auditlog"
)

// recordingEmitter captures Emit calls without needing a real chain DB.
type recordingEmitter struct {
    mu     sync.Mutex
    called int
    last   auditlog.Record
    err    error
}

func (r *recordingEmitter) Emit(rec auditlog.Record) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.called++
    r.last = rec
    return r.err
}

func TestAuditStore_CreateChainsFinding(t *testing.T) {
    dbPath := filepath.Join(t.TempDir(), "audit.db")
    store, err := NewAuditStore(dbPath)
    if err != nil {
        t.Fatalf("NewAuditStore: %v", err)
    }
    defer store.Close()

    chainDB, err := sql.Open("sqlite",
        filepath.Join(t.TempDir(), "chain.db")+"?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        t.Fatalf("open chain db: %v", err)
    }
    defer chainDB.Close()
    if err := auditlog.Migrate(context.Background(), chainDB); err != nil {
        t.Fatalf("Migrate: %v", err)
    }

    em := &recordingEmitter{}
    store.SetChainEmitter(em)
    store.SetChainEmitter(nil) // nil guard: must not clear em

    f := AuditFinding{
        EmployeeID: "e1",
        Severity:   SeverityCritical, // adjust to the actual enum: search AuditSeverity
        Checkpoint: CheckpointPostTurn,
        Evidence:   "violation",
        DetectedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
    }
    if err := store.Create(context.Background(), f); err != nil {
        t.Fatalf("Create: %v", err)
    }
    em.mu.Lock()
    defer em.mu.Unlock()
    if em.called != 1 {
        t.Fatalf("Emit called %d times, want 1", em.called)
    }
    if em.last.Type != "audit_finding" || em.last.EmployeeID != "e1" {
        t.Fatalf("record fields: %+v", em.last)
    }
    if em.last.Payload["finding_id"] == "" {
        t.Fatalf("finding_id missing: %+v", em.last.Payload)
    }
    if em.last.At.IsZero() {
        t.Fatal("At must be stamped")
    }
}

func TestAuditStore_EmitFailureDoesNotFailCreate(t *testing.T) {
    dbPath := filepath.Join(t.TempDir(), "audit.db")
    store, err := NewAuditStore(dbPath)
    if err != nil {
        t.Fatalf("NewAuditStore: %v", err)
    }
    defer store.Close()

    store.SetChainEmitter(&recordingEmitter{err: errBoom})
    f := AuditFinding{EmployeeID: "e1", Severity: SeverityInfo, Checkpoint: CheckpointPostTurn}
    if err := store.Create(context.Background(), f); err != nil {
        t.Fatalf("Create must succeed despite Emit failure: %v", err)
    }
}

func TestAuditStore_ChainDB(t *testing.T) {
    dbPath := filepath.Join(t.TempDir(), "audit.db")
    store, err := NewAuditStore(dbPath)
    if err != nil {
        t.Fatalf("NewAuditStore: %v", err)
    }
    defer store.Close()
    if store.ChainDB() == nil {
        t.Fatal("ChainDB must return the underlying handle")
    }
}

var errBoom = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }
```

> **Implementer note:** `SeverityCritical`, `SeverityInfo`,
> `CheckpointPostTurn` are placeholders — search enforcement.go for the real
> `AuditSeverity`/`AuditCheckpoint` enum names before writing (search_files
> pattern `AuditSeverity = |Severity.* AuditSeverity|Checkpoint.* string`).
> The test helper types (`recordingEmitter`, `errBoom`) belong at the top of
> the file; adjust to match real enum constants. `sync` import needed.

**Step 2:** `go test -p 2 ./internal/employee/ -run 'TestAuditStore_CreateChains|TestAuditStore_EmitFailure|TestAuditStore_ChainDB' -count=1`
→ FAIL (SetChainEmitter undefined).

**Step 3 — implementation** (enforcement.go additions):

```go
// ChainEmitter receives one auditlog.Record after every successful finding
// INSERT (master.md C5). Implemented by *auditlog.Store.Emit.
type ChainEmitter interface {
    Emit(rec auditlog.Record) error
}

// SetChainEmitter wires the chain emitter. Nil is ignored (setter nil-guard).
func (s *AuditStore) SetChainEmitter(e ChainEmitter) {
    if e == nil {
        return
    }
    s.chainEmitter = e
}

// ChainDB exposes the underlying database handle for auditlog.Migrate /
// auditlog.Verify (leaf 03 anchoring + verification).
func (s *AuditStore) ChainDB() *sql.DB { return s.db }
```

Add field to the `AuditStore` struct (next to `planIDValidator`):

```go
    // chainEmitter, when set, receives one auditlog record per successful
    // Create. Emit failures are logged and swallowed (chain is the
    // tamper-evidence layer; findings remain the primary record).
    chainEmitter ChainEmitter
```

Modify `Create` (:1743): immediately before the final `return nil`, add:

```go
    if s.chainEmitter != nil {
        rec := auditlog.Record{
            Type:       "audit_finding",
            EmployeeID: f.EmployeeID,
            Payload: auditlog.SanitizePayload(map[string]any{
                "finding_id":    f.ID,
                "severity":      string(f.Severity),
                "checkpoint":    string(f.Checkpoint),
                "violated_rule": f.ViolatedRule,
                "goal_id":       nullableString(f.GoalID),
                "plan_id":       nullableString(f.PlanID),
                "turn_id":       nullableString(f.TurnID),
                "evidence":      f.Evidence,
            }),
            At: f.DetectedAt,
        }
        if err := s.chainEmitter.Emit(rec); err != nil {
            // Frozen policy: chain failure never fails the finding insert.
            slog.Warn("audit chain emit failed", "err", err, "finding_id", f.ID)
        }
    }
    return nil
```

(Add `"log/slog"` and `auditlog` imports; `nullableString` already exists in
the package — it is used in Create's INSERT at :1761.)

**Step 4:** filtered employee tests PASS; then
`go test -p 2 ./internal/employee/ -run 'TestAuditStore' -count=1` (all
pre-existing AuditStore tests still green).

### Task 3: emit.go — the Store→ChainEmitter adapter

**Files:** Create `internal/auditlog/emit.go`,
`internal/auditlog/emit_test.go`.

**Step 1 — failing test** (`emit_test.go`):

```go
package auditlog

import (
    "context"
    "testing"
)

func TestStoreEmit_AppendsRecord(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()

    var ce interface{ Emit(Record) error } = s // structural check
    if err := ce.Emit(Record{
        Type: "audit_finding", EmployeeID: "e1",
        Payload: SanitizePayload(map[string]any{"finding_id": "f-1"}),
    }); err != nil {
        t.Fatalf("Emit: %v", err)
    }
    res, err := VerifyChain(ctx, db)
    if err != nil || !res.OK || res.Records != 1 {
        t.Fatalf("verify after Emit: %+v err=%v", res, err)
    }
}
```

**Step 2:** FAIL (Emit undefined). **Step 3** — implementation exactly the
snippet in the Interface Contract section (`Emit` calls `Append` with
`context.Background()`). **Step 4:**
`go test -p 2 ./internal/auditlog/...` → PASS.

### Task 4: goal_loop.go — gate result emission

**Files:** Modify `internal/employee/goal_loop.go` (RunGate call site ~:1014
and the auditStore.Create block ~:1414 for context), Create
`internal/employee/goal_loop_gate_emit_test.go`.

**Step 1 — failing test** (`goal_loop_gate_emit_test.go`):

```go
package employee

import (
    "context"
    "database/sql"
    "path/filepath"
    "testing"

    "github.com/caimlas/meept/internal/auditlog"
    "github.com/caimlas/meept/internal/gate"
)

func TestGateResultRecord_Shape(t *testing.T) {
    // The mapping helper is unit-tested directly; the GoalLoop call-site
    // wiring is verified in the integration test (master.md Integration Test
    // Plan step 2) because constructing a full GoalLoop in a unit test
    // requires the whole loop fixture.
    res := gate.GateResult{Passed: true, Output: "all tests passed\n", Skipped: false}
    rec := gateResultRecord("g-1", res, 250*time.Millisecond)
    if rec.Type != "gate_result" || rec.EmployeeID != "" {
        t.Fatalf("record header: %+v", rec)
    }
    if rec.Payload["passed"] != true || rec.Payload["skipped"] != false {
        t.Fatalf("passed/skipped: %+v", rec.Payload)
    }
    if rec.Payload["output_sha256"] != auditlog.OutputSHA256(res.Output) {
        t.Fatalf("output_sha256: %+v", rec.Payload)
    }
    if _, has := rec.Payload["output"]; has {
        t.Fatal("raw output must never appear in a gate record")
    }
    if rec.Payload["duration_ms"] != int64(250) {
        t.Fatalf("duration_ms: %+v", rec.Payload)
    }
}

func TestEmitGateResult_ChainsAndSanitizes(t *testing.T) {
    chainDB, err := sql.Open("sqlite",
        filepath.Join(t.TempDir(), "chain.db")+"?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        t.Fatalf("open: %v", err)
    }
    defer chainDB.Close()
    ctx := context.Background()
    if err := auditlog.Migrate(ctx, chainDB); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    chainStore, err := auditlog.OpenStoreFromDB(chainDB, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer chainStore.Close()

    l := &GoalLoop{}
    l.SetChainStore(chainStore)
    l.emitGateResult(ctx, "g-9",
        gate.GateResult{Passed: true, Output: "token=supersecret"}, 100*time.Millisecond)

    res, err := auditlog.VerifyChain(ctx, chainDB)
    if err != nil || !res.OK || res.Records != 1 {
        t.Fatalf("verify: %+v err=%v", res, err)
    }
}
```

**Step 2:** `go test -p 2 ./internal/employee/ -run 'TestGateResultRecord|TestEmitGateResult' -count=1`
→ FAIL (gateResultRecord / SetChainStore undefined).

**Step 3 — implementation** (goal_loop.go additions; `l.chainStore` is a new
unexported field `chainStore ChainEmitter` on GoalLoop):

```go
// SetChainStore wires the audit chain emitter for gate-result records.
// Nil is ignored (setter nil-guard).
func (l *GoalLoop) SetChainStore(e ChainEmitter) {
    if e == nil {
        return
    }
    l.chainStore = e
}

// gateResultRecord maps a gate outcome to a chain record. The gate command
// output is reduced to its SHA-256 — never stored raw (master.md C4).
func gateResultRecord(goalID string, res gate.GateResult, dur time.Duration) auditlog.Record {
    payload := map[string]any{
        "goal_id":       goalID,
        "passed":        res.Passed,
        "skipped":       res.Skipped,
        "duration_ms":   dur.Milliseconds(),
        "output_sha256": auditlog.OutputSHA256(res.Output),
    }
    return auditlog.Record{
        Type:    "gate_result",
        Payload: auditlog.SanitizePayload(payload),
        At:      time.Now().UTC(),
    }
}

// emitGateResult chains a gate outcome. Failures are logged, never fatal
// (frozen policy, master.md C5).
func (l *GoalLoop) emitGateResult(ctx context.Context, goalID string, res gate.GateResult, dur time.Duration) {
    if l.chainStore == nil {
        return
    }
    if err := l.chainStore.Emit(gateResultRecord(goalID, res, dur)); err != nil {
        slog.Warn("audit chain gate emit failed", "err", err, "goal_id", goalID)
    }
}
```

At the `RunGate` call site (~goal_loop.go:1014), capture duration and emit:

```go
    gateStart := time.Now()
    res, state, err := RunGate(ctx, cfg, backend, workdir, &prev)
    if err != nil {
        return nil, err
    }
    l.emitGateResult(ctx, l.currentGoalID(), res, time.Since(gateStart))
```

> **Implementer notes:** (1) `l.currentGoalID()` is a stand-in — find how
> the goal ID is available at that call site (the surrounding function's
> receiver/params) and use the real accessor; if no goal ID is in scope,
> pass the value from the loop's active-goal field and say so in your
> report. (2) Confirm the exact context around goal_loop.go:1014 with
> `terminal cat -n` before patching; match surrounding style. (3) If GoalLoop
> construction makes a zero-value `&GoalLoop{}` test impractical, export a
> small `newGoalLoopForTest` helper ONLY in the _test.go file (Go allows
> same-package test helpers; do not add it to production code). (4) `slog`
> import needed.

**Step 4:** filtered tests PASS; full leaf-01 suite still PASS.

### Task 5: wiring.go — construct and attach the chain store

**Files:** Modify `internal/employee/wiring.go` (AuditStore construction
:162-169).

**Step 1 — failing test** (`wiring_chain_test.go`):

```go
package employee

import (
    "context"
    "database/sql"
    "path/filepath"
    "testing"

    "github.com/caimlas/meept/internal/auditlog"
)

func TestWiringChainStore_AttachedToAuditStore(t *testing.T) {
    // Direct construction parity: OpenStoreFromDB over a shared handle must
    // produce a working emitter end-to-end (finding → chain row → verify).
    chainDB, err := sql.Open("sqlite",
        filepath.Join(t.TempDir(), "chain.db")+"?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        t.Fatalf("open: %v", err)
    }
    defer chainDB.Close()
    ctx := context.Background()
    if err := auditlog.Migrate(ctx, chainDB); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    chainStore, err := auditlog.OpenStoreFromDB(chainDB, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer chainStore.Close()

    store, err := NewAuditStore(filepath.Join(t.TempDir(), "audit.db"))
    if err != nil {
        t.Fatalf("NewAuditStore: %v", err)
    }
    defer store.Close()
    store.SetChainEmitter(chainStore)

    f := AuditFinding{
        EmployeeID: "e1",
        Severity:   AuditSeverity("info"),   // ADJUST: real enum value
        Checkpoint: AuditCheckpoint("post_turn"), // ADJUST: real enum value
    }
    if err := store.Create(ctx, f); err != nil {
        t.Fatalf("Create: %v", err)
    }
    res, err := auditlog.VerifyChain(ctx, chainDB)
    if err != nil || !res.OK || res.Records != 1 {
        t.Fatalf("chain after finding: %+v err=%v", res, err)
    }
    rec, ok, err := chainStore.Head(ctx)
    if err != nil || !ok || rec.Type != "audit_finding" {
        t.Fatalf("head: ok=%v err=%v rec=%+v", ok, err, rec)
    }
}
```

**Step 2:** FAIL (chainDB path exists in wiring but nothing constructs the
chain store).

**Step 3 — implementation** (wiring.go, at the AuditStore construction block
:162-169). Mirror the shared/standalone duality of the findings store:

```go
    var as *AuditStore
    var chainStore *auditlog.Store
    if sharedDB != nil {
        as, err = NewAuditStoreFromDB(sharedDB)
    } else {
        asPath := filepath.Join(employeesDir, "audit.db")
        as, err = NewAuditStore(asPath)
    }
    if err != nil {
        // existing error handling
    }
    // Chain store shares the same DB when sharedDB is present (one file);
    // standalone path gets its own file next to audit.db.
    if sharedDB != nil {
        if err := auditlog.Migrate(ctx, sharedDB); err != nil {
            return nil, fmt.Errorf("migrate audit chain: %w", err)
        }
        chainStore, err = auditlog.OpenStoreFromDB(sharedDB, logger)
    } else {
        chainStore, err = auditlog.OpenStore(
            filepath.Join(employeesDir, "audit_chain.db"), logger)
    }
    if err != nil {
        return nil, fmt.Errorf("open audit chain store: %w", err)
    }
    as.SetChainEmitter(chainStore)
```

> **Implementer notes:** (1) read wiring.go:140-200 first (`terminal cat`)
> and match the real function signature/returns — the sketch above assumes
> `logger` and `ctx` are in scope and the function returns
> `(something, error)`; adapt names. (2) Decide ownership: when `sharedDB`
> is used, `chainStore` shares the pool — do NOT close it independently at
> wiring teardown if `sharedDB` is closed there; add a comment. (3) The
> standalone filename `audit_chain.db` is a contract decision recorded in
> master.md C5-adjacent note; keep it consistent with leaf 03's docs task.

**Step 4:** `go test -p 2 ./internal/employee/ -run 'TestWiring|TestAuditStore|TestEmit|TestGateResult' -count=1`
→ PASS.

### Task 6: package-wide verification

1. `gofmt -l internal/employee internal/auditlog` → empty.
2. `go vet ./internal/employee/... ./internal/auditlog/...` → clean.
3. `make mutexio` and `make predid` → clean.
4. `go test -p 2 ./internal/auditlog/... -count=1` → PASS.
5. `go test -p 2 ./internal/employee/ -count=1` → PASS (full package — the
   Create change touches every emitter's path; the existing suite is the
   regression net).
6. `go build ./...` → clean.

## Self-Verification Checklist

- [ ] `go test -p 2 ./internal/auditlog/...` and
      `go test -p 2 ./internal/employee/` pass with `-count=1`.
- [ ] `go build ./...` clean; `gofmt -l` empty; `go vet` clean;
      `make mutexio` + `make predid` clean.
- [ ] Signatures match master.md C4/C5 exactly: `SanitizePayload`,
      `ChainEmitter`, `SetChainEmitter` (nil-guarded), `ChainDB`,
      `SetChainStore` (nil-guarded), `Emit`.
- [ ] Gate records carry `output_sha256`, NEVER raw `Output`.
- [ ] Secret-key blocklist + value scrub + truncation all tested; input maps
      never mutated.
- [ ] Emit failure never fails Create or the gate flow (policy test).
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] No line-number prefixes
      (`grep -rcE '^\s+[0-9]+\|' internal/auditlog internal/employee` → 0).
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator)

- [ ] enforcement.go: hook fires only after successful INSERT; failure logged
      via slog.Warn, swallowed; payload keys exactly per C5; `At` = finding
      DetectedAt.
- [ ] goal_loop.go: emit wrapped around RunGate with duration; helper is
      nil-safe; output reduced to SHA-256; command string sanitized
      (it is user-authored but non-secret — passes through sanitizer).
- [ ] wiring.go: both construction paths (sharedDB / standalone) wire the
      emitter; ownership/close semantics commented; no double-close risk.
- [ ] redact.go: blocklist case-insensitive substring; scrub via
      internal/security OutputMonitor; truncation marker; deep-copy (no
      mutation of caller maps).
- [ ] All setter methods nil-guarded; two-value assertions only; errors
      wrapped with %w; no ignored errors.
- [ ] Pre-existing AuditStore tests untouched and passing.
- [ ] No debug artifacts, no TODOs, no placeholder values, no ignored errors.
- [ ] `git status --short` shows changes ONLY in: internal/auditlog (redact,
      emit + tests), internal/employee (enforcement.go, goal_loop.go,
      wiring.go + 3 test files).
