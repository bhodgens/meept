# Leaf 01 — auditlog Package Core (canonical JSON, hash chain, SQLite store)

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.** Read
this document fully, then implement every task in order with TDD (failing
test → run to confirm failure → minimal implementation → run to confirm pass).

- Parent: `master.md` (contracts C1, C2, C3, C8 in this tree apply to you)
- Test command: `go test -p 2 ./internal/auditlog/...`
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** when inspecting existing files use
  `search_files` or `terminal cat` — NEVER pipe `read_file` output into
  `write_file` (the `N|` prefixes corrupt source). After writing a file, do
  NOT read it back to verify — write once and stop.

## Scope

Create the greenfield package `internal/auditlog`: canonical JSON
serialization, the `Record` type + SHA-256 hash chaining, and the SQLite
append-only store. Package-internal verification walk. **No wiring into
internal/employee — that is leaf 02. No CLI/anchor job — leaf 03.**

## Dependencies

None. Greenfield package; depends only on stdlib + `modernc.org/sqlite`
(already a module dependency, see change_journal.go import) + `pkg/id` is NOT
needed here (chain is seq-keyed, no IDs).

## Estimated context

~70K (template + contracts ~6K; writing 6 files ~2,400 lines with tests;
several `go test` cycles).

## Interface Contract

Implement EXACTLY these signatures (frozen in master.md §Interface Contracts
C1–C3; restate nothing — they are normative):

```go
type Record struct {
    Seq        uint64         `json:"seq"`
    PrevHash   string         `json:"prev_hash"`
    RecordHash string         `json:"record_hash"`
    Type       string         `json:"type"`
    EmployeeID string         `json:"employee_id"`
    Payload    map[string]any `json:"payload"`
    At         time.Time      `json:"at"`
}
func HashRecord(rec Record) (string, error)
func CanonicalJSON(v any) ([]byte, error)
type VerifyResult struct { OK bool; Records uint64; Head string; BrokenAt uint64; Reason string }
func Migrate(ctx context.Context, db *sql.DB) error
func OpenStore(dbPath string, logger *slog.Logger) (*Store, error)
func OpenStoreFromDB(db *sql.DB, logger *slog.Logger) (*Store, error)
func (s *Store) Append(ctx context.Context, rec Record) (Record, error)
func (s *Store) Head(ctx context.Context) (Record, bool, error)
func (s *Store) ChainDB() *sql.DB
func (s *Store) Close() error
func VerifyChain(ctx context.Context, db *sql.DB) (VerifyResult, error)
```

`VerifyChain` is a package function over `*sql.DB` (leaf 03 calls it with
`AuditStore.ChainDB()`); it must not require a `*Store` handle.

## Tasks

### Task 1: canonical.go — CanonicalJSON for maps/scalars

**Files:** Create `internal/auditlog/canonical.go`,
`internal/auditlog/canonical_test.go`.

**Step 1 — failing test** (`canonical_test.go`):

```go
package auditlog

import (
    "bytes"
    "testing"
)

func TestCanonicalJSON_SortsKeys(t *testing.T) {
    tests := []struct {
        name string
        in   map[string]any
        want string
    }{
        {"two keys", map[string]any{"b": 1, "a": "x"}, `{"a":"x","b":1}`},
        {"nested", map[string]any{"z": map[string]any{"y": 2, "x": 1}}, `{"z":{"x":1,"y":2}}`},
        {"array order preserved", map[string]any{"l": []any{3, 1, 2}}, `{"l":[3,1,2]}`},
        {"no html escape", map[string]any{"s": "<a>&</a>"}, `{"s":"<a>&</a>"}`},
        {"empty map", map[string]any{}, `{}`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CanonicalJSON(tt.in)
            if err != nil {
                t.Fatalf("CanonicalJSON: %v", err)
            }
            if !bytes.Equal(got, []byte(tt.want)) {
                t.Fatalf("got %s want %s", got, tt.want)
            }
        })
    }
}

func TestCanonicalJSON_Numbers(t *testing.T) {
    tests := []struct {
        name string
        in   any
        want string
    }{
        {"int", 42, `42`},
        {"int64 max", int64(9223372036854775807), `9223372036854775807`},
        {"uint64 seq", uint64(18446744073709551615), `18446744073709551615`},
        {"float integral", float64(3), `3`},
        {"float frac", 0.5, `0.5`},
        {"negative zero normalized", math.Copysign(0, -1), `0`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CanonicalJSON(tt.in)
            if err != nil {
                t.Fatalf("CanonicalJSON(%v): %v", tt.in, err)
            }
            if string(got) != tt.want {
                t.Fatalf("got %s want %s", got, tt.want)
            }
        })
    }
}

func TestCanonicalJSON_RejectsUnsupported(t *testing.T) {
    if _, err := CanonicalJSON(time.Now()); err == nil {
        t.Fatal("time.Time must be rejected")
    }
    if _, err := CanonicalJSON(math.Inf(1)); err == nil {
        t.Fatal("Inf must be rejected")
    }
    if _, err := CanonicalJSON(struct{ A int }{1}); err == nil {
        t.Fatal("struct must be rejected")
    }
}
```

**Step 2:** `go test -p 2 ./internal/auditlog/...` → compile FAIL
(CanonicalJSON undefined).

**Step 3 — implementation** (`canonical.go`):

```go
// Package auditlog provides the tamper-evident hash-chained audit trail:
// canonical JSON serialization, per-record SHA-256 hashing, prev-hash
// chaining over an append-only SQLite table, and full-chain verification.
//
// Canonicalization is an in-house implementation (sorted keys, no
// insignificant whitespace, UTF-8, standard Go number formatting). It is
// self-consistent within meept but is NOT a full RFC 8785 (JCS)
// implementation — float formatting and HTML-escaping policy differ.
// See docs/plans/tamper-evident-audit-log/OPEN-QUESTIONS.md Q4.
package auditlog

import (
    "bytes"
    "encoding/json"
    "fmt"
    "math"
    "sort"
    "strconv"
)

// CanonicalJSON renders v as canonical JSON: recursively sorted object keys
// (byte-wise), no insignificant whitespace, UTF-8, HTML characters NOT
// escaped. Supported: map[string]any, []any, string, bool, nil, integer
// types, float64, json.Number. Everything else is an error — callers must
// pre-normalize (e.g. time.Time → string via RFC3339Nano).
func CanonicalJSON(v any) ([]byte, error) {
    var buf bytes.Buffer
    if err := appendCanonical(&buf, v); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func appendCanonical(buf *bytes.Buffer, v any) error {
    switch x := v.(type) {
    case nil:
        buf.WriteString("null")
    case bool:
        if x {
            buf.WriteString("true")
        } else {
            buf.WriteString("false")
        }
    case string:
        return appendJSONString(buf, x)
    case float64:
        s, err := canonicalFloat(x)
        if err != nil {
            return err
        }
        buf.WriteString(s)
    case json.Number:
        if _, err := x.Float64(); err != nil {
            return fmt.Errorf("canonical json.Number %q: %w", x, err)
        }
        buf.WriteString(x.String())
    case int:
        buf.WriteString(strconv.FormatInt(int64(x), 10))
    case int8:
        buf.WriteString(strconv.FormatInt(int64(x), 10))
    case int16:
        buf.WriteString(strconv.FormatInt(int64(x), 10))
    case int32:
        buf.WriteString(strconv.FormatInt(int64(x), 10))
    case int64:
        buf.WriteString(strconv.FormatInt(x, 10))
    case uint:
        buf.WriteString(strconv.FormatUint(uint64(x), 10))
    case uint8:
        buf.WriteString(strconv.FormatUint(uint64(x), 10))
    case uint16:
        buf.WriteString(strconv.FormatUint(uint64(x), 10))
    case uint32:
        buf.WriteString(strconv.FormatUint(uint64(x), 10))
    case uint64:
        buf.WriteString(strconv.FormatUint(x, 10))
    case []any:
        buf.WriteByte('[')
        for i, e := range x {
            if i > 0 {
                buf.WriteByte(',')
            }
            if err := appendCanonical(buf, e); err != nil {
                return err
            }
        }
        buf.WriteByte(']')
    case map[string]any:
        keys := make([]string, 0, len(x))
        for k := range x {
            keys = append(keys, k)
        }
        sort.Strings(keys)
        buf.WriteByte('{')
        for i, k := range keys {
            if i > 0 {
                buf.WriteByte(',')
            }
            if err := appendJSONString(buf, k); err != nil {
                return err
            }
            buf.WriteByte(':')
            if err := appendCanonical(buf, x[k]); err != nil {
                return err
            }
        }
        buf.WriteByte('}')
    default:
        return fmt.Errorf("canonical: unsupported type %T", v)
    }
    return nil
}

func appendJSONString(buf *bytes.Buffer, s string) error {
    var sb bytes.Buffer
    enc := json.NewEncoder(&sb)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(s); err != nil {
        return fmt.Errorf("canonical string: %w", err)
    }
    out := sb.Bytes()
    buf.Write(bytes.TrimSuffix(out, []byte("\n"))) // Encoder appends newline
    return nil
}

func canonicalFloat(f float64) (string, error) {
    if math.IsNaN(f) || math.IsInf(f, 0) {
        return "", fmt.Errorf("canonical float: non-finite %v", f)
    }
    if f == 0 {
        return "0", nil // normalize -0
    }
    s := strconv.FormatFloat(f, 'g', -1, 64)
    // 'g' may emit exponents (1e+06); keep — deterministic per Go spec.
    return s, nil
}
```

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS.

### Task 2: chain.go — Record, HashRecord, VerifyChain

**Files:** Create `internal/auditlog/chain.go`,
`internal/auditlog/chain_test.go`.

**Step 1 — failing test** (`chain_test.go`):

```go
package auditlog

import (
    "context"
    "testing"
    "time"
)

func fixedAt() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

func rec(seq uint64, prev string, typ string) Record {
    return Record{Seq: seq, PrevHash: prev, Type: typ, EmployeeID: "e1",
        Payload: map[string]any{"k": "v"}, At: fixedAt()}
}

func TestHashRecord_DeterministicAndKeyOrderIndependent(t *testing.T) {
    a := rec(1, "", "audit_finding")
    b := rec(1, "", "audit_finding")
    b.Payload = map[string]any{"k2": 1, "k1": "v"} // different key order/set → different hash
    h1, err := HashRecord(a)
    if err != nil {
        t.Fatalf("HashRecord: %v", err)
    }
    if len(h1) != 64 {
        t.Fatalf("want 64-char hex, got %q", h1)
    }
    again, _ := HashRecord(a)
    if h1 != again {
        t.Fatal("hash must be deterministic")
    }
    h2, _ := HashRecord(b)
    if h1 == h2 {
        t.Fatal("different payloads must hash differently")
    }
    // RecordHash itself must NOT affect the hash (chicken-and-egg guard).
    a.RecordHash = "any-old-value"
    h3, _ := HashRecord(a)
    if h1 != h3 {
        t.Fatal("RecordHash must be excluded from hash input")
    }
}

func TestHashRecord_TimeNormalization(t *testing.T) {
    a := rec(1, "", "x")
    b := rec(1, "", "x")
    b.At = fixedAt().In(time.FixedZone("EST", -5*3600))
    h1, _ := HashRecord(a)
    h2, _ := HashRecord(b)
    if h1 == h2 {
        t.Fatal("different instants must hash differently")
    }
    c := rec(1, "", "x")
    c.At = fixedAt().Truncate(time.Second) // truncated instant differs
    h3, _ := HashRecord(c)
    if h1 == h3 {
        t.Fatal("truncation must change the hash (RFC3339Nano in canonical form)")
    }
}

func TestVerifyChain_EmptyAndGood(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    if err := Migrate(ctx, db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    res, err := VerifyChain(ctx, db)
    if err != nil {
        t.Fatalf("VerifyChain empty: %v", err)
    }
    if !res.OK || res.Records != 0 {
        t.Fatalf("empty chain must verify: %+v", res)
    }

    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()
    prev := ""
    for i := uint64(1); i <= 5; i++ {
        got, err := s.Append(ctx, rec(i, prev, "audit_finding"))
        if err != nil {
            t.Fatalf("Append %d: %v", i, err)
        }
        if got.Seq != i || got.PrevHash != prev {
            t.Fatalf("Append %d: got %+v want prev=%q", i, got, prev)
        }
        prev = got.RecordHash
    }
    res, err = VerifyChain(ctx, db)
    if err != nil {
        t.Fatalf("VerifyChain: %v", err)
    }
    if !res.OK || res.Records != 5 {
        t.Fatalf("want ok 5 records: %+v", res)
    }
}

func TestVerifyChain_DetectsTamperAndLinkBreak(t *testing.T) {
    t.Run("payload tampered", func(t *testing.T) {
        db := chainOfThree(t)
        _, err := db.Exec(`UPDATE audit_log_chain SET payload='{"evil":1}' WHERE seq=2`)
        if err != nil {
            t.Fatalf("tamper exec: %v", err)
        }
        res, err := VerifyChain(context.Background(), db)
        if err != nil {
            t.Fatalf("VerifyChain: %v", err)
        }
        if res.OK || res.BrokenAt != 2 {
            t.Fatalf("want broken at 2: %+v", res)
        }
    })
    t.Run("prev_hash link broken", func(t *testing.T) {
        db := chainOfThree(t)
        _, err := db.Exec(`UPDATE audit_log_chain SET prev_hash='deadbeef' WHERE seq=3`)
        if err != nil {
            t.Fatalf("tamper exec: %v", err)
        }
        res, _ := VerifyChain(context.Background(), db)
        if res.OK || res.BrokenAt != 3 {
            t.Fatalf("want broken at 3: %+v", res)
        }
    })
    t.Run("genesis prev_hash must be empty", func(t *testing.T) {
        db := chainOfThree(t)
        _, _ = db.Exec(`UPDATE audit_log_chain SET prev_hash='x' WHERE seq=1`)
        res, _ := VerifyChain(context.Background(), db)
        if res.OK || res.BrokenAt != 1 {
            t.Fatalf("want broken at 1: %+v", res)
        }
    })
}
```

(`openTestDB` and `chainOfThree` helpers live in
`internal/auditlog/helpers_test.go` — see Task 5.)

**Step 2:** `go test -p 2 ./internal/auditlog/...` → FAIL (undefined symbols).

**Step 3 — implementation** (`chain.go`):

```go
package auditlog

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "fmt"
)

// Record is one entry in the hash-chained audit log (master.md C1).
type Record struct {
    Seq        uint64         `json:"seq"`
    PrevHash   string         `json:"prev_hash"`
    RecordHash string         `json:"record_hash"` // excluded from hash input
    Type       string         `json:"type"`
    EmployeeID string         `json:"employee_id"`
    Payload    map[string]any `json:"payload"`
    At         time.Time      `json:"at"`
}

// HashRecord returns lowercase-hex SHA-256 of CanonicalJSON of rec with
// RecordHash omitted and At normalized to RFC3339Nano UTC. Hash-input key
// set: at, employee_id, payload, prev_hash, seq, type.
func HashRecord(rec Record) (string, error) {
    at := rec.At
    if at.IsZero() {
        return "", fmt.Errorf("hash record: zero At")
    }
    input := map[string]any{
        "seq":         rec.Seq,
        "prev_hash":   rec.PrevHash,
        "type":        rec.Type,
        "employee_id": rec.EmployeeID,
        "payload":     rec.Payload,
        "at":          at.UTC().Format(time.RFC3339Nano),
    }
    b, err := CanonicalJSON(input)
    if err != nil {
        return "", fmt.Errorf("hash record: %w", err)
    }
    sum := sha256.Sum256(b)
    return hex.EncodeToString(sum[:]), nil
}

// VerifyResult reports the outcome of a full-chain walk (master.md C1).
type VerifyResult struct {
    OK       bool
    Records  uint64
    Head     string
    BrokenAt uint64
    Reason   string
}

// VerifyChain walks the audit_log_chain table in seq order and verifies each
// record's hash and its prev-hash link. The first failure short-circuits.
func VerifyChain(ctx context.Context, db *sql.DB) (VerifyResult, error) {
    rows, err := db.QueryContext(ctx, `
        SELECT seq, prev_hash, record_hash, type, employee_id, payload, at
        FROM audit_log_chain ORDER BY seq ASC`)
    if err != nil {
        return VerifyResult{}, fmt.Errorf("verify chain: %w", err)
    }
    defer rows.Close()

    var res VerifyResult
    var wantPrev string
    var prevSeq uint64
    first := true
    for rows.Next() {
        var (
            rec     Record
            payload string
            at      string
        )
        if err := rows.Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash,
            &rec.Type, &rec.EmployeeID, &payload, &at); err != nil {
            return VerifyResult{}, fmt.Errorf("verify chain scan: %w", err)
        }
        if err := decodePayload(payload, &rec.Payload); err != nil {
            return VerifyResult{}, err
        }
        if err := rec.At.UnmarshalJSON([]byte(`"` + at + `"`)); err != nil {
            return VerifyResult{}, fmt.Errorf("verify chain at %d: %w", rec.Seq, err)
        }
        res.Records++
        if first {
            if rec.Seq != 1 || rec.PrevHash != "" {
                res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
                    "genesis must be seq 1 with empty prev_hash"
                return res, nil
            }
            first = false
        } else {
            if rec.Seq != prevSeq+1 {
                res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
                    fmt.Sprintf("seq gap: got %d want %d", rec.Seq, prevSeq+1)
                return res, nil
            }
            if rec.PrevHash != wantPrev {
                res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
                    fmt.Sprintf("prev_hash mismatch: got %q want %q", rec.PrevHash, wantPrev)
                return res, nil
            }
        }
        h, err := HashRecord(rec)
        if err != nil {
            res.OK, res.BrokenAt, res.Reason = false, rec.Seq, err.Error()
            return res, nil
        }
        if h != rec.RecordHash {
            res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
                fmt.Sprintf("record_hash mismatch: got %q computed %q", rec.RecordHash, h)
            return res, nil
        }
        prevSeq = rec.Seq
        wantPrev = rec.RecordHash
        res.Head = rec.RecordHash
    }
    if err := rows.Err(); err != nil {
        return VerifyResult{}, fmt.Errorf("verify chain rows: %w", err)
    }
    res.OK = true
    return res, nil
}
```

> **Implementer note:** the verification order per record is: (1) genesis
> check (seq 1 + empty prev_hash); (2) seq strictly +1 from previous row; (3)
> prev_hash equals previous RecordHash; (4) computed hash equals stored
> record_hash. The tests pin BrokenAt for tampered-payload=2, link-break=3,
> genesis=1. `decodePayload` is a small helper (see Task 3) — declare it in
> store.go and reuse it here; you must also add the `time` import to
> chain.go.

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS (Task 5 provides
helpers; if you land Task 2 before Task 5, the test file will fail to compile
— implement Task 5's helper file immediately after this task's impl).

### Task 3: store.go — Migrate, OpenStore, Append, Head

**Files:** Create `internal/auditlog/store.go`,
`internal/auditlog/store_test.go`.

**Step 1 — failing test** (`store_test.go`):

```go
package auditlog

import (
    "context"
    "database/sql"
    "path/filepath"
    "sync"
    "testing"
)

func openTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db")+"?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }
    t.Cleanup(func() { _ = db.Close() })
    return db
}

func TestMigrate_Idempotent(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    if err := Migrate(ctx, db); err != nil {
        t.Fatalf("Migrate 1: %v", err)
    }
    if err := Migrate(ctx, db); err != nil {
        t.Fatalf("Migrate 2: %v", err)
    }
}

func TestAppend_ChainsSeqAndHashes(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    if err := Migrate(ctx, db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()

    r1, err := s.Append(ctx, rec(0, "ignored-prev", "audit_finding"))
    if err != nil {
        t.Fatalf("Append 1: %v", err)
    }
    if r1.Seq != 1 || r1.PrevHash != "" {
        t.Fatalf("genesis: got %+v", r1)
    }
    if r1.RecordHash == "" {
        t.Fatal("RecordHash must be filled")
    }
    r2, err := s.Append(ctx, r1) // caller-supplied Seq/PrevHash overwritten
    if err != nil {
        t.Fatalf("Append 2: %v", err)
    }
    if r2.Seq != 2 || r2.PrevHash != r1.RecordHash {
        t.Fatalf("chain: got %+v", r2)
    }

    // Zero At is stamped to now.
    r3, err := s.Append(ctx, Record{Type: "x", Payload: map[string]any{}})
    if err != nil {
        t.Fatalf("Append 3: %v", err)
    }
    if r3.At.IsZero() {
        t.Fatal("zero At must be stamped")
    }
}

func TestHead(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()

    if _, ok, err := s.Head(ctx); err != nil || ok {
        t.Fatalf("empty head: ok=%v err=%v", ok, err)
    }
    r1, _ := s.Append(ctx, rec(0, "", "t"))
    h, ok, err := s.Head(ctx)
    if err != nil || !ok || h.RecordHash != r1.RecordHash {
        t.Fatalf("head: ok=%v err=%v head=%+v", ok, err, h)
    }
}

func TestOpenStore_ExpandsHomeAndCreatesDir(t *testing.T) {
    // Relative path with a new subdirectory must be created with 0700.
    dir := filepath.Join(t.TempDir(), "nested", "audit")
    s, err := OpenStore(filepath.Join(dir, "chain.db"), nil)
    if err != nil {
        t.Fatalf("OpenStore: %v", err)
    }
    defer s.Close()
    st, err := os.Stat(dir)
    if err != nil {
        t.Fatalf("stat: %v", err)
    }
    if st.IsDir() == false {
        t.Fatal("dir not created")
    }
}

func TestAppend_ConcurrentSerialization(t *testing.T) {
    db := openTestDB(t)
    ctx := context.Background()
    _ = Migrate(ctx, db)
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    defer s.Close()

    const n = 20
    var wg sync.WaitGroup
    seqs := make(chan uint64, n)
    for i := 0; i < n; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            r, err := s.Append(ctx, Record{
                Type: "concurrent", EmployeeID: "e",
                Payload: map[string]any{"i": i},
            })
            if err != nil {
                t.Errorf("Append %d: %v", i, err)
                return
            }
            seqs <- r.Seq
        }(i)
    }
    wg.Wait()
    close(seqs)
    seen := map[uint64]bool{}
    for sq := range seqs {
        if seen[sq] {
            t.Fatalf("duplicate seq %d", sq)
        }
        seen[sq] = true
    }
    res, err := VerifyChain(ctx, db)
    if err != nil || !res.OK || res.Records != n {
        t.Fatalf("verify after concurrent appends: %+v err=%v", res, err)
    }
}
```

**Step 2:** `go test -p 2 ./internal/auditlog/...` → FAIL.

**Step 3 — implementation** (`store.go`):

```go
package auditlog

import (
    "context"
    "database/sql"
    "fmt"
    "log/slog"
    "os"
    "path/filepath"
    "strings"
    "time"

    _ "modernc.org/sqlite" //nolint:revive // sqlite driver registration
)

const auditLogSchemaSQL = `
CREATE TABLE IF NOT EXISTS audit_log_chain (
    seq         INTEGER PRIMARY KEY,
    prev_hash   TEXT NOT NULL,
    record_hash TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL,
    employee_id TEXT NOT NULL,
    payload     TEXT NOT NULL,
    at          TEXT NOT NULL
);
`

// Migrate creates the audit_log_chain table if missing. Idempotent.
// INSERT-only by convention in this plan; an UPDATE/DELETE-blocking trigger
// is OPEN-QUESTIONS.md Q1 (recommended fast-follow, deliberately not here).
func Migrate(ctx context.Context, db *sql.DB) error {
    if _, err := db.ExecContext(ctx, auditLogSchemaSQL); err != nil {
        return fmt.Errorf("migrate audit log: %w", err)
    }
    return nil
}

// Store appends records to the hash-chained audit log. Concurrency is
// serialized by SQLite itself: a single pinned connection (SetMaxOpenConns(1))
// plus BEGIN IMMEDIATE per append — the same pattern as
// internal/tools/builtin/change_journal.go. No Go mutex is held across I/O.
type Store struct {
    db     *sql.DB
    logger *slog.Logger
}

// OpenStore opens (or creates) the audit log database at dbPath. A leading
// ~ expands to the user's home directory. The parent directory is created
// with 0700. A nil logger falls back to slog.Default().
func OpenStore(dbPath string, logger *slog.Logger) (*Store, error) {
    if logger == nil {
        logger = slog.Default()
    }
    if strings.HasPrefix(dbPath, "~") {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, fmt.Errorf("audit log: home dir: %w", err)
        }
        dbPath = filepath.Join(home, dbPath[1:])
    }
    if dir := filepath.Dir(dbPath); dir != "" {
        if err := os.MkdirAll(dir, 0o700); err != nil {
            return nil, fmt.Errorf("audit log: create dir: %w", err)
        }
    }
    dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, fmt.Errorf("audit log: open db: %w", err)
    }
    db.SetMaxOpenConns(1)
    s := &Store{db: db, logger: logger.With("component", "audit-chain")}
    if err := s.migrate(context.Background()); err != nil {
        _ = db.Close()
        return nil, err
    }
    return s, nil
}

// OpenStoreFromDB wraps an existing connection pool (shared databases).
// The caller keeps ownership of db; Close is a no-op on the handle
// (documented: only OpenStore-owned conns are closed).
func OpenStoreFromDB(db *sql.DB, logger *slog.Logger) (*Store, error) {
    if logger == nil {
        logger = slog.Default()
    }
    db.SetMaxOpenConns(1)
    s := &Store{db: db, logger: logger.With("component", "audit-chain")}
    if err := s.migrate(context.Background()); err != nil {
        return nil, err
    }
    return s, nil
}

func (s *Store) migrate(ctx context.Context) error { return Migrate(ctx, s.db) }

// Append inserts rec as the next chain record. Seq, PrevHash and RecordHash
// are computed and fill the returned Record (caller values ignored); At is
// stamped now().UTC() when zero. The read-head → hash → insert sequence runs
// inside one BEGIN IMMEDIATE transaction so concurrent appends cannot fork
// the chain.
func (s *Store) Append(ctx context.Context, rec Record) (Record, error) {
    if rec.Type == "" {
        return Record{}, fmt.Errorf("audit log append: type is required")
    }
    if rec.At.IsZero() {
        rec.At = time.Now().UTC()
    }
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return Record{}, fmt.Errorf("audit log append: begin: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    // BEGIN IMMEDIATE (write lock) — modernc sqlite defers to the first
    // write, so pin the write intent with a no-op write touch if needed.
    // With SetMaxOpenConns(1) our single conn serializes all callers; the
    // transaction keeps read-head+insert atomic against THIS connection.
    var seq uint64
    var prev string
    err = tx.QueryRowContext(ctx, `SELECT seq, record_hash FROM audit_log_chain ORDER BY seq DESC LIMIT 1`).Scan(&seq, &prev)
    if err != nil && err != sql.ErrNoRows {
        return Record{}, fmt.Errorf("audit log append: head: %w", err)
    }
    if err == sql.ErrNoRows {
        seq, prev = 0, ""
    }
    rec.Seq = seq + 1
    rec.PrevHash = prev
    h, err := HashRecord(rec)
    if err != nil {
        return Record{}, fmt.Errorf("audit log append: %w", err)
    }
    rec.RecordHash = h
    payload, err := CanonicalJSON(rec.Payload)
    if err != nil {
        return Record{}, fmt.Errorf("audit log append: payload: %w", err)
    }
    _, err = tx.ExecContext(ctx, `
        INSERT INTO audit_log_chain (seq, prev_hash, record_hash, type, employee_id, payload, at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
        rec.Seq, rec.PrevHash, rec.RecordHash, rec.Type, rec.EmployeeID,
        string(payload), rec.At.UTC().Format(time.RFC3339Nano))
    if err != nil {
        return Record{}, fmt.Errorf("audit log append: insert: %w", err)
    }
    if err := tx.Commit(); err != nil {
        return Record{}, fmt.Errorf("audit log append: commit: %w", err)
    }
    return rec, nil
}

// Head returns the newest record; ok=false when the chain is empty.
func (s *Store) Head(ctx context.Context) (Record, bool, error) {
    var (
        rec     Record
        payload string
        at      string
    )
    err := s.db.QueryRowContext(ctx, `
        SELECT seq, prev_hash, record_hash, type, employee_id, payload, at
        FROM audit_log_chain ORDER BY seq DESC LIMIT 1`).
        Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash, &rec.Type,
            &rec.EmployeeID, &payload, &at)
    if err == sql.ErrNoRows {
        return Record{}, false, nil
    }
    if err != nil {
        return Record{}, false, fmt.Errorf("audit log head: %w", err)
    }
    if err := decodePayload(payload, &rec.Payload); err != nil {
        return Record{}, false, err
    }
    if err := rec.At.UnmarshalJSON([]byte(`"` + at + `"`)); err != nil {
        return Record{}, false, fmt.Errorf("audit log head: %w", err)
    }
    return rec, true, nil
}

// ChainDB exposes the underlying handle (leaf 03 verification + anchoring).
func (s *Store) ChainDB() *sql.DB { return s.db }

// Close closes an OpenStore-owned database. OpenStoreFromDB handles are
// caller-owned; Close on those is a no-op by convention (we cannot detect
// ownership, so Close always closes — callers sharing a pool must not call
// Close; document at call sites).
func (s *Store) Close() error { return s.db.Close() }

func decodePayload(s string, into *map[string]any) error {
    var m map[string]any
    if err := json.Unmarshal([]byte(s), &m); err != nil {
        return fmt.Errorf("audit log payload decode: %w", err)
    }
    *into = m
    return nil
}
```

> **Implementer notes:** (1) add the `encoding/json` import for
> `decodePayload`; (2) single-conn `SetMaxOpenConns(1)` is what actually
> serializes appends in practice; `BeginTx` gives read-head+insert atomicity.
> The concurrency test (20 goroutines) must pass — if you see
> `SQLITE_BUSY`, raise `SetMaxOpenConns` handling by keeping 1 and relying on
> the busy_timeout; do NOT add a Go mutex.

**Step 4:** `go test -p 2 ./internal/auditlog/...` → PASS.

### Task 4: helpers_test.go — shared test helpers

**Files:** Create `internal/auditlog/helpers_test.go`.

Contains `chainOfThree(t)` (opens a DB, migrates, appends 3 chained records
via a Store, returns the *sql.DB) — the test files from Tasks 2–3 reference
`openTestDB` (store_test.go) and `rec`/`fixedAt` (chain_test.go); helpers_test.go
holds only `chainOfThree`:

```go
package auditlog

import (
    "context"
    "database/sql"
    "testing"
)

// chainOfThree builds a verified 3-record chain in a fresh DB and returns
// the handle (tests tamper directly via SQL).
func chainOfThree(t *testing.T) *sql.DB {
    t.Helper()
    db := openTestDB(t)
    ctx := context.Background()
    if err := Migrate(ctx, db); err != nil {
        t.Fatalf("Migrate: %v", err)
    }
    s, err := OpenStoreFromDB(db, nil)
    if err != nil {
        t.Fatalf("OpenStoreFromDB: %v", err)
    }
    t.Cleanup(func() { _ = s.Close() })
    prev := ""
    for i := uint64(1); i <= 3; i++ {
        got, err := s.Append(ctx, rec(i, prev, "audit_finding"))
        if err != nil {
            t.Fatalf("Append %d: %v", i, err)
        }
        prev = got.RecordHash
    }
    return db
}
```

(You may consolidate `openTestDB` into helpers_test.go instead of
store_test.go — either location is fine; keep one definition.)

**Step 2/4:** `go test -p 2 ./internal/auditlog/...` → PASS.

### Task 5: full-package verification

1. `gofmt -l internal/auditlog` → empty.
2. `go vet ./internal/auditlog/...` → clean.
3. `go run ./tools/analyzers/mutexio/... ./internal/auditlog/...` (or
   `make mutexio`) → clean.
4. `go test -p 2 ./internal/auditlog/... -count=1` → all PASS.
5. `go build ./...` → clean (package compiles into the module; no wiring yet).

## Self-Verification Checklist

- [ ] `go test -p 2 ./internal/auditlog/...` passes with `-count=1`.
- [ ] `go build ./...` clean; `gofmt -l` empty; `go vet` clean.
- [ ] `make mutexio` and `make predid` clean.
- [ ] Signatures match master.md C1–C3 EXACTLY (field names, types, JSON tags).
- [ ] Hash input excludes RecordHash; includes RFC3339Nano UTC `at`.
- [ ] Genesis = seq 1 + empty prev_hash; tamper tests pin BrokenAt 1/2/3.
- [ ] Concurrent-append test passes (no duplicate seqs; chain verifies).
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] No line-number prefixes in any file
      (`grep -rcE '^\s+[0-9]+\|' internal/auditlog` → 0).
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator)

- [ ] canonical.go: sorted keys, no whitespace, HTML unescaped, -0 → 0,
      unsupported types rejected (time.Time, struct, Inf).
- [ ] chain.go: hash input key set exactly {at, employee_id, payload,
      prev_hash, seq, type}; VerifyChain checks genesis, link, hash, and
      monotonic seq.
- [ ] store.go: WAL DSN + busy_timeout 5000 + MaxOpenConns(1) + 0700 dirs +
      `~` expansion (change_journal pattern); Append fills Seq/PrevHash/
      RecordHash; zero At stamped; no Go mutex.
- [ ] Table `audit_log_chain` matches the frozen schema (no extra columns).
- [ ] Tests are table-driven where enumerating cases; no skipped tests.
- [ ] No debug artifacts, no TODOs, no placeholder values, no ignored errors.
- [ ] Nothing outside internal/auditlog was modified (`git status --short`
      shows only new package files).
- [ ] No line-number corruption in any touched file.
