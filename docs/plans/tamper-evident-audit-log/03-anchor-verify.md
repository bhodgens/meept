# Leaf 03 — Anchoring, Verification Surface, Docs

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.** Read
this document fully, then implement every task in order with TDD (failing
test → run to confirm failure → minimal implementation → run to confirm pass).

- Parent: `master.md` (contracts C6, C7, C8 apply to you)
- Prerequisites: leaf 01 COMPLETE (`internal/auditlog` with `Store`,
  `Head`, `VerifyChain`, `ChainDB`) and leaf 02 COMPLETE (`AuditStore`
  exposes `ChainDB()`; chain wired in wiring.go).
- Test command: `go test -p 2 ./internal/auditlog/...` plus filtered
  employee/cmd commands per task.
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** when inspecting existing files use
  `search_files` or `terminal cat` — NEVER pipe `read_file` output into
  `write_file`. Use the `patch` tool for surgical edits; after writing, do
  NOT read the file back to verify.

## Scope

Make the chain observable and verifiable:

1. `AnchorJob` — periodic digest snapshot (chain head + seq) appended as
   JSONL to `<employeesDir>/anchors/audit-anchors.jsonl`.
2. RPC `agents.audit.verify` (internal/employee/handler.go) returning the
   full-chain VerifyResult.
3. CLI `meept agents audit --verify` (cmd/meept/agents_cmd.go).
4. Docs: a "tamper-evident audit chain" section in
   `docs/workflows/employees.md`, and an `internal/auditlog` row in the
   AGENTS.md Key Components table.

No new emitter paths — leaf 02 owns all emission.

## Dependencies

- Leaves 01+02 COMPLETE.
- Files read (search_files / cat): `internal/employee/handler.go`
  (registration map :120-126, handleAuditList :585), `internal/employee/
  wiring.go` (where leaf 02 constructed the chain store), 
  `cmd/meept/agents_cmd.go` (`newAgentsAuditCmd` :1235), 
  `docs/workflows/employees.md` (## Concepts structure), `AGENTS.md`
  (Key Components table :90-107).

## Estimated context

~80K (contracts ~6K; reading 5 files ~12K; writing ~700 lines incl. docs;
TDD cycles across three packages — auditlog, employee handler, cmd build).

## Interface Contract

Implement EXACTLY (frozen in master.md C6/C7):

```go
// internal/auditlog/anchor.go
const DefaultAnchorInterval = time.Hour

func NewAnchorJob(store *Store, dir string, interval time.Duration, logger *slog.Logger) *AnchorJob
func (j *AnchorJob) ExportOnce(ctx context.Context) (string, error) // returns file path
func (j *AnchorJob) Run(ctx context.Context)                        // ticker loop, exits on ctx.Done
// Store method (lives in anchor.go; appends to the package surface):
func (s *Store) ExportDigest(ctx context.Context, dir string) (string, error)
```

Anchor JSONL line format (appended, one object per line):
`{"exported_at":"<RFC3339Nano>","seq":<N>,"chain_head":"<hex>"}`.
Empty chain → no line.

RPC (registered next to `agents.audit.list`, handler.go :123):

```
"agents.audit.verify": h.handleAuditVerify
params: {}  (none)
result: {"ok":bool,"records":uint64,"head":string,"broken_at":uint64,"reason":string}
```

CLI: `meept agents audit [--verify] <id>?` — with `--verify`, zero
positional args allowed; exit non-zero (RunE error) when the chain is
broken. Output lines:
`audit chain ok: N records, head <12-hex-prefix>` /
`audit chain BROKEN at seq N: <reason>`.

## Tasks

### Task 1: anchor.go — Store.ExportDigest

**Files:** Create `internal/auditlog/anchor.go`,
`internal/auditlog/anchor_test.go`.

**Step 1 — failing test** (`anchor_test.go`):

```go
package auditlog

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestExportDigest_EmptyChainWritesNothing(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()
    dir := t.TempDir()
    path, err := s.ExportDigest(ctx, dir)
    if err != nil {
        t.Fatalf("ExportDigest: %v", err)
    }
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    if len(strings.TrimSpace(string(data))) != 0 {
        t.Fatalf("empty chain must write no line, got %q", data)
    }
}

func TestExportDigest_AppendsVerifiableLines(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()
    _, err = s.Append(ctx, Record{Type: "audit_finding", EmployeeID: "e",
        Payload: map[string]any{"i": 1}})
    if err != nil {
        t.Fatalf("Append: %v", err)
    }
    dir := t.TempDir()
    p1, err := s.ExportDigest(ctx, dir)
    if err != nil {
        t.Fatalf("ExportDigest 1: %v", err)
    }
    p2, err := s.ExportDigest(ctx, dir)
    if err != nil {
        t.Fatalf("ExportDigest 2: %v", err)
    }
    if p1 != p2 {
        t.Fatalf("exports must append to the same file: %s vs %s", p1, p2)
    }
    data, err := os.ReadFile(p2)
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    if len(lines) != 2 {
        t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
    }
    var line2 struct {
        ExportedAt string `json:"exported_at"`
        Seq        uint64 `json:"seq"`
        ChainHead  string `json:"chain_head"`
    }
    if err := json.Unmarshal([]byte(lines[1]), &line2); err != nil {
        t.Fatalf("line 2 not JSON: %v", err)
    }
    if line2.Seq != 1 {
        t.Fatalf("seq: %+v", line2)
    }
    head, ok, err := s.Head(ctx)
    if err != nil || !ok || line2.ChainHead != head.RecordHash {
        t.Fatalf("chain_head mismatch: %+v err=%v head=%+v", line2, err, head)
    }
    // Line format sanity: no whitespace between tokens (canonical-ish JSON).
    if strings.Contains(lines[1], ": ") || strings.Contains(lines[1], ", \"") {
        t.Fatalf("line has insignificant whitespace: %q", lines[1])
    }
}
```

**Step 2:** `go test -p 2 ./internal/auditlog/... -run TestExportDigest -count=1`
→ FAIL (ExportDigest undefined).

**Step 3 — implementation** (`anchor.go`):

```go
package auditlog

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "os"
    "path/filepath"
    "time"
)

// anchorDirName is the subdirectory (under the employees data dir) holding
// periodic digest snapshots.
const anchorDirName = "anchors"

// anchorFileName is the JSONL file digests append to.
const anchorFileName = "audit-anchors.jsonl"

// ExportDigest appends the current chain head to <dir>/anchors/
// audit-anchors.jsonl as one canonical JSON line:
// {"exported_at":"<RFC3339Nano>","seq":<N>,"chain_head":"<hex>"}
// An empty chain writes no line. Returns the file path.
func (s *Store) ExportDigest(ctx context.Context, dir string) (string, error) {
    head, ok, err := s.Head(ctx)
    if err != nil {
        return "", fmt.Errorf("export digest: %w", err)
    }
    anchorPath := filepath.Join(dir, anchorDirName, anchorFileName)
    if !ok {
        return anchorPath, nil // nothing to anchor
    }
    if err := os.MkdirAll(filepath.Dir(anchorPath), 0o700); err != nil {
        return "", fmt.Errorf("export digest: %w", err)
    }
    line, err := json.Marshal(map[string]any{
        "exported_at": time.Now().UTC().Format(time.RFC3339Nano),
        "seq":         head.Seq,
        "chain_head":  head.RecordHash,
    })
    if err != nil {
        return "", fmt.Errorf("export digest: %w", err)
    }
    f, err := os.OpenFile(anchorPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
    if err != nil {
        return "", fmt.Errorf("export digest: %w", err)
    }
    defer f.Close() //nolint:errcheck // write error below is the reported one
    if _, err := f.Write(append(line, '\n')); err != nil {
        return "", fmt.Errorf("export digest: %w", err)
    }
    return anchorPath, nil
}
```

> **Implementer note:** `json.Marshal(map[string]any{...})` emits
> alphabetically-ordered keys for map[string]any (Go sorts map keys), which
> satisfies the no-insignificant-whitespace assertion; a line has exactly
> three top-level keys in sorted order: chain_head, exported_at, seq.

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS.

### Task 2: AnchorJob — periodic ticker loop

**Files:** Modify `internal/auditlog/anchor.go` (append), modify
`anchor_test.go`.

**Step 1 — failing test** (append to `anchor_test.go`):

```go
func TestAnchorJob_RunExportsAndStopsOnCancel(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()
    _, err = s.Append(ctx, Record{Type: "t", Payload: map[string]any{}})
    if err != nil {
        t.Fatalf("Append: %v", err)
    }

    dir := t.TempDir()
    job := NewAnchorJob(s, dir, 25*time.Millisecond, nil)
    runCtx, cancel := context.WithCancel(context.Background())
    done := make(chan struct{})
    go func() { job.Run(runCtx); close(done) }()

    // Wait for at least one export.
    deadline := time.Now().Add(2 * time.Second)
    anchorPath := filepath.Join(dir, "anchors", "audit-anchors.jsonl")
    for time.Now().Before(deadline) {
        if data, err := os.ReadFile(anchorPath); err == nil && len(data) > 0 {
            break
        }
        time.Sleep(5 * time.Millisecond)
    }
    cancel()
    select {
    case <-done:
    case <-time.After(2 * time.Second):
        t.Fatal("Run did not exit on cancel")
    }
    data, err := os.ReadFile(anchorPath)
    if err != nil || len(data) == 0 {
        t.Fatalf("no anchor written: err=%v data=%q", err, data)
    }
}

func TestAnchorJob_NilIntervalUsesDefault(t *testing.T) {
    // ExportOnce must work regardless of interval; constructor validation:
    job := NewAnchorJob(nil, t.TempDir(), 0, nil)
    if job == nil {
        t.Fatal("constructor must not return nil for zero interval (uses default)")
    }
}
```

**Step 2:** FAIL. **Step 3 — implementation** (append to anchor.go):

```go
// DefaultAnchorInterval is the export cadence when a job is constructed
// with a non-positive interval.
const DefaultAnchorInterval = time.Hour

// AnchorJob periodically appends the chain digest to the anchors JSONL file.
// It holds no mutex: Run is the only mutation path and it is single-goroutine
// by construction (mutexio-safe).
type AnchorJob struct {
    store    *Store
    dir      string
    interval time.Duration
    logger   *slog.Logger
}

// NewAnchorJob constructs the periodic digest exporter. A non-positive
// interval falls back to DefaultAnchorInterval. A nil logger falls back to
// slog.Default().
func NewAnchorJob(store *Store, dir string, interval time.Duration, logger *slog.Logger) *AnchorJob {
    if interval <= 0 {
        interval = DefaultAnchorInterval
    }
    if logger == nil {
        logger = slog.Default()
    }
    return &AnchorJob{store: store, dir: dir, interval: interval, logger: logger}
}

// ExportOnce exports the digest immediately and returns the file path.
func (j *AnchorJob) ExportOnce(ctx context.Context) (string, error) {
    return j.store.ExportDigest(ctx, j.dir)
}

// Run blocks, exporting every interval, until ctx is done.
func (j *AnchorJob) Run(ctx context.Context) {
    ticker := time.NewTicker(j.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            path, err := j.ExportOnce(ctx)
            if err != nil {
                j.logger.Warn("audit anchor export failed", "err", err)
                continue
            }
            j.logger.Debug("audit anchor exported", "path", path)
        }
    }
}
```

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS.

### Task 3: RPC agents.audit.verify

**Files:** Modify `internal/employee/handler.go` (registration map :120-126 +
new handler method near :585), Create `internal/employee/handler_verify_test.go`.

**Step 1 — failing test** (`handler_verify_test.go`):

```go
package employee

import (
    "context"
    "database/sql"
    "encoding/json"
    "path/filepath"
    "testing"

    "github.com/caimlas/meept/internal/auditlog"
)

func TestHandleAuditVerify_OKAndBroken(t *testing.T) {
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
    for i := 0; i < 3; i++ {
        if _, err := chainStore.Append(ctx, auditlog.Record{
            Type: "audit_finding", EmployeeID: "e",
            Payload: map[string]any{"i": i},
        }); err != nil {
            t.Fatalf("Append %d: %v", i, err)
        }
    }

    h := &RPCHandler{auditChain: chainStore} // ADJUST: real RPCHandler field/construction
    raw, err := json.Marshal(map[string]any{})
    if err != nil {
        t.Fatalf("marshal: %v", err)
    }
    out, err := h.handleAuditVerify(ctx, raw)
    if err != nil {
        t.Fatalf("handleAuditVerify: %v", err)
    }
    res, ok := out.(auditlog.VerifyResult)
    if !ok {
        t.Fatalf("wrong result type %T", out)
    }
    if !res.OK || res.Records != 3 {
        t.Fatalf("verify: %+v", res)
    }

    // Tamper → broken.
    if _, err := chainDB.Exec(`UPDATE audit_log_chain SET payload='{}' WHERE seq=2`); err != nil {
        t.Fatalf("tamper: %v", err)
    }
    out, err = h.handleAuditVerify(ctx, raw)
    if err != nil {
        t.Fatalf("handleAuditVerify tampered: %v", err)
    }
    res = out.(auditlog.VerifyResult)
    if res.OK || res.BrokenAt != 2 {
        t.Fatalf("tampered: %+v", res)
    }
}
```

> **Implementer note:** read handler.go:80-130 (`terminal cat`) to see the
> real `RPCHandler` struct and how existing handlers reach the stores
> (`h.manager`, direct fields...). The `auditChain` field is the
> contract-pinned name; if the handler reaches stores through `Manager`,
> add the field during wiring (Task 4) instead of inventing an accessor.

**Step 2:** `go test -p 2 ./internal/employee/ -run TestHandleAuditVerify -count=1`
→ FAIL.

**Step 3 — implementation** (handler.go additions):

```go
// In the handler registration map (next to "agents.audit.list" at :123):
        "agents.audit.verify": h.handleAuditVerify,

// Near the other audit handlers (~:585):
// handleAuditVerify verifies the full hash chain. The result mirrors
// auditlog.VerifyResult; verification failure is DATA (broken chain), not an
// RPC error — only infrastructure errors return err.
func (h *RPCHandler) handleAuditVerify(ctx context.Context, raw json.RawMessage) (any, error) {
    if h.auditChain == nil {
        return nil, errNotConfigured
    }
    return auditlog.VerifyChain(ctx, h.auditChain.ChainDB())
}
```

Add the `auditChain *sql.DB` field to `RPCHandler` (or wire the store — see
note). Import `auditlog`.

**Step 4:** filtered test PASS;
`go test -p 2 ./internal/employee/ -run TestHandle -count=1` → PASS.

### Task 4: wiring.go — hand the chain DB to the RPC handler + start AnchorJob

**Files:** Modify `internal/employee/wiring.go` (where leaf 02 constructed
`chainStore`).

**Step 1 — failing test:** none new (Task 3's test covers handler shape;
wiring is compile-checked). Extend `wiring_chain_test.go` from leaf 02 with:

```go
func TestWiringChainStore_ExposesVerifyHandle(t *testing.T) {
    // After wiring, the RPCHandler must hold a non-nil auditChain handle.
    // Construct through the real wiring path if it is exported; otherwise
    // verify via the existing wiring function used in leaf 02's test — the
    // assertion is that NewRPCHandler (or the wiring function) received the
    // chain DB. ADJUST to the real construction call after reading wiring.go.
    t.Skip("replaced by direct construction assertions in Task 3/4 review — see orchestrator review note")
}
```

**Step 2/3:** Wire `as.ChainDB()` (or `chainStore.ChainDB()`) into the
RPCHandler construction inside the same wiring function, and start the
AnchorJob:

```go
    // Hand the chain handle to the RPC layer (agents.audit.verify).
    rpcHandler.auditChain = chainStore.ChainDB() // ADJUST names to real wiring

    // Periodic off-host anchoring (master.md C6). Hourly by default.
    anchorJob := auditlog.NewAnchorJob(chainStore,
        employeesDir, auditlog.DefaultAnchorInterval, logger)
    go anchorJob.Run(ctx)
```

> **Implementer notes:** (1) read the wiring function end-to-end first and
> adapt names; if the RPCHandler is constructed elsewhere (search
> `NewRPCHandler`), thread the handle through that constructor's params
> instead of a direct field poke — match existing style. (2) If the wiring
> function has no long-lived `ctx`, use
> `context.Background()` and note it (daemon lifetime). (3) The `ctx` used
> for `Migrate` in leaf 02's sketch may need plumbing here too.

**Step 4:** `go build ./...` clean;
`go test -p 2 ./internal/employee/ -count=1` → PASS.

### Task 5: CLI --verify

**Files:** Modify `cmd/meept/agents_cmd.go` (`newAgentsAuditCmd` :1235).

**Step 1 — failing test:** cmd/meept has no unit-test harness for cobra
commands (verified: package compiles via `go build`); verification is the
build + the integration smoke in master.md's Integration Test Plan. Add the
flag now:

**Step 2/3 — implementation** (inside `newAgentsAuditCmd`):

```go
    var verify bool
    // ... existing flag defs ...
    cmd.Flags().BoolVar(&verify, "verify", false,
        "verify the full audit hash chain and exit")

    // Args: relax to allow zero args when --verify is set.
    cmd.Args = func(cmd *cobra.Command, args []string) error {
        if verify {
            if len(args) > 1 {
                return fmt.Errorf("accepts at most 1 arg when --verify is set, received %d", len(args))
            }
            return nil
        }
        if len(args) != 1 {
            return fmt.Errorf("accepts 1 arg (employee id), received %d (or use --verify)", len(args))
        }
        return nil
    }
```

And in `RunE`, FIRST branch on verify (before the agentID path):

```go
        if verify {
            rawResult, err := client.Call("agents.audit.verify", map[string]any{})
            if err != nil {
                return fmt.Errorf("failed to verify audit chain: %w", err)
            }
            var resultMap map[string]any
            if err := json.Unmarshal(rawResult, &resultMap); err != nil {
                return fmt.Errorf("failed to parse response: %w", err)
            }
            if errMsg := rpcError(resultMap); errMsg != "" {
                return fmt.Errorf("%s", errMsg)
            }
            okv, _ := resultMap["ok"].(bool) // two-value assertion (AGENTS.md)
            records, _ := resultMap["records"].(float64)
            head, _ := resultMap["head"].(string)
            brokenAt, _ := resultMap["broken_at"].(float64)
            reason, _ := resultMap["reason"].(string)
            if !okv {
                return fmt.Errorf("audit chain BROKEN at seq %d: %s",
                    int64(brokenAt), reason)
            }
            headPrefix := head
            if len(headPrefix) > 12 {
                headPrefix = headPrefix[:12]
            }
            fmt.Printf("audit chain ok: %d records, head %s\n",
                int64(records), headPrefix)
            return nil
        }
```

Also update the command's `Long` help text: add the line
`meept agents audit --verify                       # verify hash chain`.

**Step 4:** `go build -o bin/meept ./cmd/meept` → clean;
`go vet ./cmd/meept/...` → clean.

### Task 6: Docs — employees.md section + AGENTS.md row

**Files:** Modify `docs/workflows/employees.md` (insert after the
"### Quality gate (completion gating)" section ends, before
"### Roster gate vs employee gate" at :165 — read first to confirm the exact
neighborhood), Modify `AGENTS.md` (Key Components table :90-107).

**employees.md — new section** (insert; keep house tone, lowercase UI text):

```markdown
### Tamper-evident audit chain

Every audit finding and every goal quality-gate result is appended to a
hash-chained, append-only log (`internal/auditlog`, table
`audit_log_chain` in the employees SQLite database). Each record carries:

- `seq` — monotonic sequence number starting at 1
- `prev_hash` — the previous record's hash (empty for the first record)
- `record_hash` — SHA-256 over the record's canonical JSON (sorted keys, no
  insignificant whitespace; `record_hash` itself excluded)

The chain is INSERT-only by convention. Payloads are sanitized before
storage: keys or values that look like credentials are redacted, and gate
command output is reduced to a SHA-256 digest (never stored raw).

**Anchoring:** every hour the daemon appends the current
`{exported_at, seq, chain_head}` digest to
`~/.meept/employees/anchors/audit-anchors.jsonl`. Copy that file off-host;
a stored digest proves the chain state at export time even if the host is
later compromised.

**Verification:**

    meept agents audit --verify

Walks the full chain and re-computes every hash. Exits non-zero and prints
the first broken sequence number when the log was tampered with. Anchor
lines can be cross-checked against the reported `chain_head`.
```

**AGENTS.md — Key Components row** (insert into the table :90-107, after
the **Employee** row):

```
| **Audit Chain** | `internal/auditlog` (canonical JSON, hash chain, store, anchoring, verification) |
```

**AGENTS.md consistency rule:** per the AGENTS.md Maintenance Rule, this
row addition ships in the same commit as the leaf's code changes.

**Step 4 (docs verification):**
- Both files render: every heading/link resolves
  (`grep -n 'audit' docs/workflows/employees.md | head` spot-check).
- `meept agents audit --verify` appears in employees.md exactly once.

## Self-Verification Checklist

- [ ] `go test -p 2 ./internal/auditlog/...` and
      `go test -p 2 ./internal/employee/` pass with `-count=1`.
- [ ] `go build ./...` clean; `gofmt -l` empty; `go vet` clean;
      `make mutexio` + `make predid` clean.
- [ ] `meept agents audit --verify` (built binary) exits 0 on a good chain,
      non-zero + `BROKEN at seq N` on a tampered one (tamper via sqlite3).
- [ ] Anchor file exists at `<employeesDir>/anchors/audit-anchors.jsonl`
      after one interval; every line parses as JSON with the three keys.
- [ ] RPC result field names exactly: ok, records, head, broken_at, reason.
- [ ] employees.md section present; AGENTS.md row present.
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] No line-number prefixes
      (`grep -rcE '^\s+[0-9]+\|' internal/auditlog internal/employee cmd/meept docs/workflows AGENTS.md` → 0).
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator)

- [ ] anchor.go: JSONL append semantics (O_APPEND), 0600 file, 0700 dir,
      empty chain → no line, exported_at RFC3339Nano UTC.
- [ ] AnchorJob: ticker loop exits on ctx.Done; export failures Warn+continue;
      non-positive interval → DefaultAnchorInterval; no mutex needed
      (single-goroutine Run) — confirm no I/O under any lock.
- [ ] handler.go: verify handler returns VerifyResult as data; broken chain
      is NOT an RPC error; errNotConfigured when unwired; registration next
      to agents.audit.list.
- [ ] wiring: handle threaded to RPCHandler in existing style (no field pokes
      across packages); AnchorJob started with daemon-lifetime context.
- [ ] agents_cmd.go: --verify path FIRST in RunE; two-value type assertions;
      non-zero exit on broken; help text updated; old behavior (no --verify)
      byte-identical.
- [ ] Docs match implementation (paths, flags, file names — grep-verify each
      string quoted in employees.md exists in code).
- [ ] No debug artifacts, no TODOs, no placeholder values, no ignored errors.
- [ ] `git status --short` matches the declared file list: internal/auditlog
      (anchor.go + tests), internal/employee (handler.go, wiring.go,
      wiring_chain_test.go, handler_verify_test.go), cmd/meept/
      (agents_cmd.go), docs/workflows/employees.md, AGENTS.md.
