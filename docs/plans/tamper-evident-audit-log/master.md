# Tamper-Evident Hash-Chained Audit Log — Master Plan

> **For Hermes:** Use the `hierarchical-plan-execution` skill. This document
> orchestrates three leaf documents in `docs/plans/tamper-evident-audit-log/`.
> The orchestrator (main model) dispatches leaf implementation agents, reviews
> in-session, and commits. Implementation agents never commit.

**Goal:** Give meept a first-class, tamper-evident, append-only audit trail: a
hash-chained log (`internal/auditlog`) of agent-system transitions — one
record per line, canonical JSON, SHA-256 per record, `prev_hash` linking,
periodic off-host digest anchoring, no secrets in records — verified via
`meept agents audit --verify`.

**Gap context:** "Graph and Loop Engineering using Grok Bot" requires an
append log of agent-system transitions. meept today has plain SQLite audit
findings with no chain and no anchoring: `internal/employee/enforcement.go:1685`
(`type AuditStore`, `Create` at :1743, `auditSchemaSQL` at :~1650).
`internal/tools/builtin/change_journal.go` has per-entry `PostSHA` but not a
chain.

**Tech stack:** Go (single language, all leaves), `crypto/sha256`,
`modernc.org/sqlite` (driver already in use), `slog`, cobra CLI.

---

## Architecture Overview

Three layers, one new package, minimal touch points on existing code:

```
[emitters]                      [chain]                     [verification]
AuditStore.Create ──┐
  (PostTurnAuditor, │  hook ──► internal/auditlog.Store.Append
   PeriodicAuditor, │            Record{Seq,PrevHash,RecordHash,
   scheduler,        │                   Type,EmployeeID,Payload,At}
   manager, goal_loop│)           RecordHash = sha256(canonicalJSON(rec − RecordHash))
                     │            PrevHash = previous RecordHash; Seq monotonic
goal gate result ────┘                 │
  (goal_loop.go:1014, direct emit)     ▼
                          SQLite audit_log_chain (INSERT-only, WAL,
                          single-conn BEGIN IMMEDIATE serialization)
                                       │
              ┌────────────────────────┴─────────────────────┐
              ▼                                              ▼
  AnchorJob (hourly digest snapshot               meept agents audit --verify
  {exported_at,seq,chain_head} → JSONL            RPC agents.audit.verify →
  in <employeesDir>/anchors/)                     auditlog.Verify full-chain walk
```

Why one hook point: `PostTurnAuditor`, `PeriodicAuditor`, `scheduler_jobs.go`,
and `manager.go` all persist findings through `AuditStore.Create`
(enforcement.go:1743) — a single post-insert hook chains all of them for free.
Only goal gate results bypass `Create` (they are not findings), so
`goal_loop.go` emits directly.

Why SQLite-level serialization, not a Go mutex: hash-chaining needs
read-previous-head → compute → insert to be atomic. The Store pins one
connection (`SetMaxOpenConns(1)`, mirroring `change_journal.go`) and wraps each
append in `BEGIN IMMEDIATE`, so concurrent appends serialize inside SQLite.
No Go mutex is held across I/O — mutexio-analyzer-safe by construction.

Files touched per leaf (honest count): leaf 01 creates 6 files in a greenfield
package (3 source + 3 test, one language, one package). Leaves 02/03 touch
4–7 files but each change is small and mostly mechanical; the leaf documents
state exact insertion points. This deviates slightly from the ≤3-file guideline
because the pinned design groups these emitters/surfaces into single leaves.

---

## Interface Contracts

These contracts are FROZEN. All three leaves implement against exactly these
signatures. Any deviation goes to OPEN-QUESTIONS.md before code is written.

### C1. Record and hashing (leaf 01 → consumed by 02, 03)

```go
package auditlog // internal/auditlog/chain.go

// Record is one entry in the hash-chained audit log.
type Record struct {
    Seq        uint64         `json:"seq"`         // starts at 1, monotonic
    PrevHash   string         `json:"prev_hash"`   // "" for Seq 1 (genesis)
    RecordHash string         `json:"record_hash"` // NOT part of hashed input
    Type       string         `json:"type"`        // "audit_finding" | "gate_result"
    EmployeeID string         `json:"employee_id"`
    Payload    map[string]any `json:"payload"`     // pre-sanitized by caller
    At         time.Time      `json:"at"`          // UTC; Append stamps now when zero
}

// HashRecord returns lowercase-hex SHA-256 of CanonicalJSON of the record
// with RecordHash omitted and At normalized to RFC3339Nano UTC.
// Seq is encoded as a JSON number via json.Number (never float64).
func HashRecord(rec Record) (string, error)

// VerifyResult is the outcome of a full-chain walk.
type VerifyResult struct {
    OK       bool   // true iff every record verifies and links
    Records  uint64 // number of records walked
    Head     string // last RecordHash ("" when chain empty)
    BrokenAt uint64 // Seq of first bad record (0 when OK)
    Reason   string // human-readable failure cause ("" when OK)
}
```

Hash-input key set (canonical, sorted): `at, employee_id, payload, prev_hash,
seq, type`. Genesis: `Seq == 1`, `PrevHash == ""`.

### C2. Canonical JSON (leaf 01; deviation risk documented)

```go
// internal/auditlog/canonical.go
// CanonicalJSON renders v as canonical JSON bytes: recursively sorted object
// keys (byte-wise), no insignificant whitespace, UTF-8, HTML characters NOT
// escaped (encoding/json with SetEscapeHTML(false)).
func CanonicalJSON(v any) ([]byte, error)
```

Accepted types (anything else → error): `map[string]any`, `[]any`, `string`,
`bool`, `nil`, every Go integer type, `float64`, `json.Number`.
**Rejected:** `time.Time`, structs, typed slices, `NaN`/`Inf` floats — callers
must pre-normalize. Number rules: integers as plain decimal; `float64` via
`strconv.FormatFloat(f,'g',-1,64)` with `-0` normalized to `0`;
`json.Number` passed through verbatim after a parse-validity check.
**RFC 8785 (JCS) deviation risk (pinned decision):** this is an in-house
canonicalization, not full JCS — float serialization and HTML-escaping policy
differ from RFC 8785's ES6 number formatting. Hashes are self-consistent
within meept only; a third-party verifier would need this exact algorithm.
See OPEN-QUESTIONS.md Q4.

### C3. Store (leaf 01 → 02 wires it, 03 verifies through it)

```go
// internal/auditlog/store.go
func Migrate(ctx context.Context, db *sql.DB) error // CREATE TABLE IF NOT EXISTS, idempotent
func OpenStore(dbPath string, logger *slog.Logger) (*Store, error)
func OpenStoreFromDB(db *sql.DB, logger *slog.Logger) (*Store, error)
func (s *Store) Append(ctx context.Context, rec Record) (Record, error)
    // fills Seq, PrevHash, RecordHash; stamps At=now().UTC() when zero.
    // Serialized via single conn + BEGIN IMMEDIATE; no Go mutex.
func (s *Store) Head(ctx context.Context) (Record, bool, error) // bool=false when empty
func (s *Store) ChainDB() *sql.DB
func (s *Store) Close() error
```

Schema (frozen; leaf 03 verification reads it):

```sql
CREATE TABLE IF NOT EXISTS audit_log_chain (
    seq         INTEGER PRIMARY KEY,
    prev_hash   TEXT NOT NULL,
    record_hash TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL,
    employee_id TEXT NOT NULL,
    payload     TEXT NOT NULL, -- canonical JSON of Payload
    at          TEXT NOT NULL  -- RFC3339Nano UTC
);
-- INSERT-only BY CONVENTION in this plan; UPDATE/DELETE-blocking trigger is
-- OPEN-QUESTIONS.md Q1 (recommended fast-follow, deliberately not in scope).
```

DSN/pool pattern (match `internal/tools/builtin/change_journal.go:100-105`):
`dbPath + "?_journal_mode=WAL&_busy_timeout=5000"`, `db.SetMaxOpenConns(1)`,
`os.MkdirAll(dir, 0o700)`, `~` expansion.

### C4. Sanitization (leaf 02 → applied by all emitters)

```go
// internal/auditlog/redact.go
// SanitizePayload deep-copies p and redacts in place:
//  1. key blocklist (case-insensitive substring): token, secret, password,
//     api_key, apikey, authorization, credential, private_key, bearer, cookie
//     → value replaced with "[redacted]"
//  2. every remaining string value scrubbed via security.OutputMonitor.
//     DetectAndRedact (credential redaction; internal/security imports none of
//     internal/{employee,bot,agent,tools} — no import cycle, verified)
//  3. any string value > 4096 bytes truncated with suffix "...[truncated]"
func SanitizePayload(p map[string]any) map[string]any
```

Secrets policy (pinned): **no tokens, secrets, raw command output, or raw LLM
output in records.** Gate records carry `output_sha256` (hex of SHA-256 of
gate output), never `Output` text. Finding records carry sanitized evidence.

### C5. Emitter wiring (leaf 02, internal/employee)

```go
// internal/employee/enforcement.go — additions
// ChainEmitter is implemented by *auditlog.Store. AuditStore calls Emit after
// a successful finding INSERT. Nil = chaining disabled (back-compat).
type ChainEmitter interface {
    Emit(rec auditlog.Record) error
}

// SetChainEmitter wires the chain emitter. Nil is ignored (setter nil-guard
// convention, AGENTS.md).
func (s *AuditStore) SetChainEmitter(e ChainEmitter)

// ChainDB exposes the underlying handle for auditlog.Migrate/OpenStoreFromDB
// and auditlog.Verify. Read-mostly; no new ownership.
func (s *AuditStore) ChainDB() *sql.DB
```

Emit-failure policy (frozen): a failed `Emit` is logged `logger.Warn` and does
**not** fail the finding INSERT — findings remain the primary record; the
chain is the tamper-evidence layer.

Record `Type` values (frozen enum): `"audit_finding"` (everything flowing
through `AuditStore.Create`), `"gate_result"` (goal quality gate).

`audit_finding` payload keys: `finding_id`, `severity`, `checkpoint`,
`violated_rule`, `goal_id`, `plan_id`, `turn_id`, `evidence` (sanitized,
truncated). `gate_result` payload keys: `goal_id`, `command`, `passed`,
`skipped`, `duration_ms`, `output_sha256`.

### C6. Anchoring (leaf 03)

```go
// internal/auditlog/anchor.go
const DefaultAnchorInterval = time.Hour

type AnchorJob struct { /* store, dir, interval, logger */ }
func NewAnchorJob(store *Store, dir string, interval time.Duration, logger *slog.Logger) *AnchorJob
func (j *AnchorJob) ExportOnce(ctx context.Context) (string, error) // returns file path
func (j *AnchorJob) Run(ctx context.Context)                        // ticker loop; returns on ctx.Done

// Store method (leaf 03 appends to store.go or anchor.go — anchor.go owns it):
func (s *Store) ExportDigest(ctx context.Context, dir string) (string, error)
```

Anchor line format (JSONL, one object per line, appended):
`{"exported_at":"<RFC3339Nano>","seq":<N>,"chain_head":"<hex>"}`.
Empty chain (no records) → no line written. Directory
`<employeesDir>/anchors/audit-anchors.jsonl`, `MkdirAll 0o700`.

### C7. Verification surface (leaf 03)

RPC (registered in `internal/employee/handler.go` next to
`agents.audit.list` at :123):

```
method: "agents.audit.verify"   params: {}
result: {"ok":bool,"records":uint64,"head":string,"broken_at":uint64,"reason":string}
```

CLI (`cmd/meept/agents_cmd.go`, `newAgentsAuditCmd` at :1235): flag `--verify`.
With `--verify`, zero positional args allowed (employee id not required);
otherwise `Args: cobra.MaximumNArgs(1)` + explicit error when an id is
missing. Exit non-zero (RunE error) when the chain is broken. Output:
`audit chain ok: N records, head <first 12 hex>` or
`audit chain BROKEN at seq N: <reason>`.

### C8. Test command (pinned, every leaf)

```bash
go test -p 2 ./internal/auditlog/...
```

macOS rule (AGENTS.md): always `-p 2` — unbounded localhost sockets exhaust
the ephemeral port range. Leaves touching `internal/employee` additionally run
filtered employee-package tests (exact commands in each leaf).

---

## Dispatch Protocol

Execute leaves strictly in order (shared files: `wiring.go` in 02 and 03;
`enforcement.go`/`goal_loop.go` only in 02):

1. **Dispatch implementation agent** — one `delegate_task` per leaf, passing
   the leaf document verbatim plus the relevant C1–C8 contracts from this
   file. Every dispatch context MUST include: *"Do NOT commit. Do NOT run
   git add. Write code, run tests, report results only. The orchestrator
   handles all git operations."*
2. **Review in-session** — the main model reviews changed files against the
   leaf's Review Checklist and C1–C8. Do NOT delegate review (delegated
   reviewers inherit the subagent model). Run the leaf's test command
   yourself.
3. **Re-dispatch on gaps** — with specific feedback; loop until review passes
   (max 3 iterations). Reset tracking status to IN_PROGRESS on re-dispatch.
4. **Commit** (orchestrator only, after review passes) — stage exactly the
   files the leaf lists; message `feat(auditlog): <leaf name>`. Before the
   first commit run: `gofmt -l <touched dirs>`, `go vet`, `make mutexio`,
   `make predid` (U1000 unused-code: remove any helper the leaf leaves
   unreferenced — pre-commit hooks reject it).
5. **Record status** in the Completion Tracking Table. The parent determines
   completion — never the implementer.

Concurrency: none available (sequential chain of dependencies). Dispatch one
leaf at a time.

---

## Child Index

| Leaf | File | Scope | Depends on | Est. context | Status |
|------|------|-------|------------|--------------|--------|
| 01 | `01-chain-core.md` | `internal/auditlog` package: canonical.go, chain.go, store.go, redact-free tests. No wiring. | — | ~70K | PENDING |
| 02 | `02-emit-wiring.md` | Wire emitters: AuditStore.Create hook, PostTurnAuditor + PeriodicAuditor (free via Create), goal gate results (goal_loop.go), SanitizePayload redactor, wiring.go construction. | 01 COMPLETE | ~90K | PENDING |
| 03 | `03-anchor-verify.md` | AnchorJob digest export, RPC `agents.audit.verify`, CLI `--verify`, docs/workflows/employees.md section, AGENTS.md Key Components row. | 01+02 COMPLETE | ~80K | PENDING |

Plus `OPEN-QUESTIONS.md` (no dispatch — user decisions).

---

## Review Checklist

For each leaf, the orchestrator verifies in-session:

- [ ] All C-contracts the leaf claims are implemented with EXACT signatures
      (diff the declarations against master C1–C8).
- [ ] Leaf test command passes: `go test -p 2 ./internal/auditlog/...`
      (plus leaf-specific extras), and `go build ./...` is clean.
- [ ] No debug artifacts, no TODOs, no placeholder values, no commented-out
      code, no stray `fmt.Println`.
- [ ] No line-number corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go'
      internal/auditlog internal/employee cmd/meept` returns zero.
- [ ] Conventions held: `fmt.Errorf("...: %w", err)` wrapping; slog logging;
      nil-guarded setters; two-value type assertions on `map[string]any`;
      no ignored errors (`_ =` sites); no `time.Now`-based IDs; no I/O under
      Go mutexes (`make mutexio` clean); IDs via `pkg/id` only where IDs are
      needed (chain rows are seq-keyed — no IDs).
- [ ] `make predid` clean (no predictable ID generation).
- [ ] Commit staged file list matches the leaf's declared file list exactly.

---

## Coding Conventions

Match `internal/employee` and AGENTS.md exactly:

- **Errors:** wrap with `fmt.Errorf("context: %w", err)`. Never ignore errors;
  pre-commit hooks reject new `_ = f()` sites.
- **Logging:** `log/slog`; components tag themselves
  (`logger.With("component", "audit-chain")`, pattern from
  change_journal.go:109).
- **Tests:** table-driven (`tests := []struct{ name string; ... }{...}` with
  `t.Run(tt.name, ...)`); `t.TempDir()` for DB paths; explicit `At` values for
  determinism except tests that target zero-time stamping.
- **Setters:** every `Set*` method nil-guards (`if fn == nil { return }`).
- **Map payloads:** two-value type assertions only
  (`if v, ok := m["k"].(string); ok { ... }`).
- **Mutex scope:** no I/O under a Go mutex. The Store uses SQLite-level
  serialization (single conn + BEGIN IMMEDIATE), no app mutex — keep it that
  way.
- **IDs:** `pkg/id.Generate` for any ID; NEVER `time.Now`/`math/rand`-based
  IDs (`make predid`). The chain itself is `seq`-keyed and needs no IDs.
- **SQLite:** WAL + `_busy_timeout=5000` DSN, `SetMaxOpenConns(1)`, `0o700`
  dirs, `~` expansion — mirror change_journal.go.
- **Formatting:** `gofmt`; analyzers `make mutexio && make predid` must pass.
- **Test parallelism:** always `-p 2` (macOS ephemeral-port rule).
- **Docs:** feature mapping `internal/<pkg>/` → `docs/workflows/<pkg>.md`;
  AGENTS.md Key Components row updated in the same change (leaf 03).

---

## Completion Tracking Table

| Leaf | Scope | Status | Notes |
|------|-------|--------|-------|
| 01-chain-core | internal/auditlog package (canonical, chain, store + tests) | PENDING | |
| 02-emit-wiring | emitter hook + gate emit + redactor + wiring.go | PENDING | |
| 03-anchor-verify | AnchorJob + RPC verify + CLI --verify + docs + AGENTS.md | PENDING | |
| OPEN-QUESTIONS | 6 decisions awaiting user | PENDING | review before/with execution |

Status values: PENDING → IN_PROGRESS → IMPLEMENTED → REVIEWED → COMPLETE
(orchestrator updates after every transition).

---

## Integration Test Plan

After all leaves reach REVIEWED, the orchestrator runs (in-session, before
final commit):

1. **Full package tests:** `go test -p 2 ./internal/auditlog/... -count=1`
   and `go test -p 2 ./internal/employee/ -run 'TestAudit|TestChain|TestGate|TestSanitize' -count=1`.
2. **Cross-package chain test:** employee finding INSERT produces a chained
   row (leaf 02's test); gate run produces `gate_result` row; every row's
   `Verify` passes.
3. **Full suite (no regressions):** `make test` (short mode, `-p 2`).
4. **Analyzers:** `make analyzers` (mutexio + predid) and `go vet ./...`.
5. **CLI e2e smoke:** build `go build -o bin/meept ./cmd/meept` and
   `go build -o bin/meept-daemon ./cmd/meept-daemon`; start the daemon against
   a scratch HOME; trigger one finding; run `./bin/meept agents audit --verify`
   → expect `audit chain ok: N records`. Then tamper:
   `sqlite3 <db> "UPDATE audit_log_chain SET payload='{}' WHERE seq=1;"`
   → `--verify` must fail with `BROKEN at seq 1` and non-zero exit.
6. **Anchor smoke:** confirm `<employeesDir>/anchors/audit-anchors.jsonl`
   exists with a line per export; `jq -c . <file>` parses every line.
7. **Docs:** employees.md section renders (headings/links valid); AGENTS.md
   Key Components table row present.
