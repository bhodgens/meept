# 03 — Query, Verify, Repair, Tail

## DISPATCH INSTRUCTION

Implement this leaf end-to-end in the isolation worktree
`.hermes/worktrees/employee-ledger` (absolute path given in your
dispatch context). Work TDD: tests from the branch test-gate leaf
already exist; write the implementation until they pass GREEN.
**Do NOT run `git commit` or `git add`** — the orchestrator commits.
If a pinned contract is impossible as written, implement the closest
working shape, leave the tests RED, and REPORT the conflict — do not
silently change a frozen name, column, or signature.

- Parent: 01-store/orchestrator.md
- Scope: ops.go: QueryLog/QueryOpen/GetState/VerifyState/RepairState/Tail + FTS insert.
- Dependencies: 02-append (§2-§5 done), 00T §6-§8
- Estimated context: 50K

## Interface Contracts (FROZEN)
## Interface Contracts (FROZEN — verbatim)
 (FROZEN — every leaf quotes these verbatim)

Package `internal/ledger` (import path `github.com/caimlas/meept/internal/ledger`).
Single physical store `<DataDir>/ledger/ledger.db`, modernc.org/sqlite, WAL,
`db.SetMaxOpenConns(1)`, DSN pragmas copied from internal/voice/callstore.go:108
(`?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)`).
Times stored RFC3339 TEXT (house convention: effects ledger / callstore).

```go
package ledger

// Kind classifies one ledger record. Closed set.
type Kind string
const (
    KindCommittal  Kind = "committal"   // what was said/promised on an interaction
    KindAction     Kind = "action"      // something was done
    KindObservation Kind = "observation" // noted fact, no state implication
    KindCommitment Kind = "commitment"  // an obligation owed (opens work)
    KindResolution Kind = "resolution"  // closes/answers a commitment
)

// Source names the writer system. Closed set.
type Source string
const (
    SourceVoice Source = "voice"
    SourceDesk  Source = "desk"
    SourceTool  Source = "tool"
    SourceCLI   Source = "cli"
    SourceTask  Source = "task"
)

// Status is the current-state column value. Closed set with the
// transition map below. Enforced by Append, never by readers.
type Status string
const (
    StatusOpen        Status = "open"
    StatusDone        Status = "done"
    StatusCancelled   Status = "cancelled"
    StatusWaitingMe   Status = "waiting_on_me"
    StatusWaitingThem Status = "waiting_on_them"
)

// AllowedTransitions: the legal state machine for subject status.
//   open            -> done | cancelled | waiting_on_me | waiting_on_them
//   waiting_on_me   -> open | done | cancelled
//   waiting_on_them -> open | done | cancelled
//   done            -> open          (reopen; store logs INFO "ledger reopen")
//   cancelled       -> open          (reopen; same log)
//   "" (new subject) -> any
var AllowedTransitions = map[Status][]Status{...}   // implemented in transitions.go

// Record is one appended, immutable log row. ID is SQLite rowid = the
// monotonic per-store offset used by Tail.
type Record struct {
    ID         int64
    EmployeeID string        // required; never client-supplied at tool boundary
    At         time.Time
    Kind       Kind
    Subject    string        // namespace key, e.g. "acme-roof"
    Payload    []byte        // opaque JSON (turnjournal.result_payload convention)
    Source     Source
    SourceRef  string        // call ccid / turn id / task id ('' allowed)
    Resolves   int64         // ledger.id closed by this row; 0 = none;
                             // MUST reference a row of the SAME EmployeeID
}

// DraftRecord is the Append input (no ID).
type DraftRecord struct {
    EmployeeID string
    Kind       Kind
    Subject    string
    Payload    []byte
    Source     Source
    SourceRef  string
    Resolves   int64
    // SetStatus (optional): desired new subject status. Empty = log-only
    // append, state untouched. Non-empty triggers the transition check.
    SetStatus  Status
    // Summary (optional, requires SetStatus): one-line current truth.
    Summary string
}

// State is the current-truth row (one per employee+subject).
type State struct {
    EmployeeID string
    Subject    string
    Status     Status
    Summary    string
    UpdatedAt  time.Time
    AsOf       int64   // ledger.id of the row that last set it
}

// Errors (frozen names).
var ErrIllegalTransition = errors.New("ledger: illegal status transition")
var ErrUnknownEmployee   = ... // not needed in v1; appends auto-create state rows
var ErrResolveNotFound   = errors.New("ledger: resolves references unknown or foreign record")

// LogQuery filters for QueryLog.
type LogQuery struct {
    EmployeeID string   // required
    Subject    string   // '' = any
    Kinds      []Kind   // nil = any
    SinceID    int64    // 0 = from start
    Limit      int      // 0 = default 200; max 1000
}

type Store struct{ ... }
func Open(dbPath string, logger *slog.Logger) (*Store, error)  // migrates, idempotent
func (s *Store) Close() error
// Append validates kinds/sources/transitions/Resolves, then in ONE
// transaction: INSERT ledger; if SetStatus != "": read state row,
// check AllowedTransitions[current]->SetStatus ("" current = new
// subject = always legal), UPSERT state. Returns the stored Record.
// Publishes the S4 announcement AFTER commit (see Events).
func (s *Store) Append(ctx context.Context, d DraftRecord) (*Record, error)
func (s *Store) QueryLog(ctx context.Context, q LogQuery) ([]Record, error)
// QueryOpen returns state rows with Status not in {done, cancelled}
// for the employee, newest-updated first.
func (s *Store) QueryOpen(ctx context.Context, employeeID string) ([]State, error)
func (s *Store) GetState(ctx context.Context, employeeID, subject string) (State, bool, error)
// Tail yields records with ID > sinceID: catch-up from SQLite, then
// live from an in-process fan-out registered at Append commit. One
// channel per Tail call; closed on ctx cancel or Store.Close.
// Guarantees no gap: registration happens before the catch-up read.
func (s *Store) Tail(ctx context.Context, sinceID int64) (<-chan Record, error)
// VerifyState folds ledger and diffs state; report lists divergences.
type VerifyReport struct{ Checked, Divergent int; Diffs []StateDiff }
type StateDiff struct{ EmployeeID, Subject string; Have, Want State /* Want = recomputed fold */ }
func (s *Store) VerifyState(ctx context.Context) (*VerifyReport, error)
// RepairState rebuilds state from the fold (uses VerifyState; guarded
// writes only on rows that diverge). Returns repaired count.
func (s *Store) RepairState(ctx context.Context) (int, error)
```

Schema (ledger/store.go, `ledgerSchema` const; guarded-ALTER idiom
from callstore.go:118-130 reserved for future columns):

```sql
CREATE TABLE IF NOT EXISTS ledger (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    employee_id TEXT NOT NULL,
    at          TEXT NOT NULL,               -- RFC3339 UTC
    kind        TEXT NOT NULL CHECK (kind IN ('committal','action','observation','commitment','resolution')),
    subject     TEXT NOT NULL DEFAULT '',
    payload     BLOB,                        -- opaque JSON, NULL allowed
    source      TEXT NOT NULL CHECK (source IN ('voice','desk','tool','cli','task')),
    source_ref  TEXT NOT NULL DEFAULT '',
    resolves    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ledger_emp_subject ON ledger(employee_id, subject, id);
CREATE INDEX IF NOT EXISTS idx_ledger_resolves    ON ledger(resolves) WHERE resolves <> 0;

CREATE TABLE IF NOT EXISTS ledger_state (
    employee_id TEXT NOT NULL,
    subject     TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('open','done','cancelled','waiting_on_me','waiting_on_them')),
    summary     TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL,
    as_of       INTEGER NOT NULL,
    PRIMARY KEY (employee_id, subject)
);

CREATE VIRTUAL TABLE IF NOT EXISTS ledger_fts USING fts5(
    subject, payload_snippet, content='');   -- external-content-free: we
                                             -- insert (subject, first 512B
                                             -- of payload) at Append time
```

Fold rule for VerifyState (frozen semantics — the ONLY sanctioned
derivation): replay ledger rows in id order; apply SetStatus
equivalents recorded implicitly: a row "set status" iff a later row
with the same (employee,subject) has a larger id and appears as
`as_of`-writable — concretely, the fold derives state by: for each
(employee,subject), the desired status = the status implied by the
LATEST log-only truth = replay of status-carrying rows. Status-carrying
rows are identified by a frozen payload envelope: Append with
SetStatus != "" ALSO writes payload augmented (see PayloadEnvelope).

```go
// PayloadEnvelope: when SetStatus is used, Append wraps the user payload:
// {"v":1,"status":"done","summary":"...","data":<user payload or null>}
// Fold decodes envelopes; a row with no envelope is log-only.
```

Events (leaf 02-03): after commit, publish the frozen S4 topic, NOT a
new topic:
```go
storage.StorageRecordChangedTopic // "storage.record.changed"
storage.RecordChanged{Store: "ledger", Key: employeeID + "/" + subject,
                      Op: storage.ChangeOpPut, Value: <State JSON>}
```
internal/ledger imports internal/storage (one-way, already gated by S4).

Tool surface (leaf 02-01): internal/tools/builtin/ledger.go, two Tools
implementing tools.Tool (interface.go:24): `ledger_append`,
`ledger_query`. Category "platform". Employee identity is STAMPED
SERVER-SIDE from ctx, never an argument (precedent: employeeIDContextKey
stamp, internal/daemon/voice_wiring.go:1207-1211). The builtin package
defines its own unexported ctx key ONLY if none is exported today; the
leaves MUST locate the existing exported helper first (search
`EmployeeIDFromContext` / `employeeIDContextKey` under internal/) and
reuse it. ledger_append args: {kind, subject, payload(object), source_ref?, resolves_id?, set_status?, summary?}. source is fixed
to "tool" for tool appends. ledger_query args: {mode: "log"|"open",
subject?, since_id?, limit?}.

Voice wiring (branch 03): daemon-owned glue in internal/daemon
(ledger_wiring.go) constructs the Store once (components.go pattern),
passes it to tools via a constructor param, to ReleaseHook, and to the
day-brief seam. ReleaseHook attach point: immediately after
Binding.OnCallEnd succeeds inside the `if seen` block
(internal/daemon/voice_employee.go:447) — exactly-once via the existing
heldCalls guard. The brief LLM call reuses the employee's pinned model
via the same llm client the loop uses (search loop.SetPersistentModelOverride
consumer for the client handle; fall back to daemon default LLM client
if none reachable — the leaf pins: no new HTTP client).

Brief JSON contract (frozen; mirrors uptractor turn.ts:848-901):
```json
{"summary":"", "action_items":[{"text":"","owner":"me|them","due":""}],
 "commitments":[""], "follow_up_message":"", "risk_flags":[""]}
```
Refusal rule (frozen): usable speech = >=3 non-filler words in the
transcript (port uptractor heardEnough rule; filler list: um, uh, okay,
yeah, hmm, like). If unusable or transcript empty → no LLM call, no
brief, log INFO.

Day brief consumer (leaf 03-03): internal/voice/daybrief.go gains an
optional seam (interface, default nil) the wiring sets:
```go
type OpenCommitmentsSource interface { OpenCommitments(ctx context.Context, employeeID string) ([]CommitmentBrief, error) }
type CommitmentBrief struct{ Subject, Summary, Status, LastAt string }
```
When the seam is non-nil AND returns rows, PromptBlock lists
"- [open] subject — summary (status)" lines INSTEAD of the raw
prior-call transcript section that embeds phone numbers
(daybrief.go:203). Raw transcripts stay in calls.db; only the prompt
section is replaced. Never render a phone number in the block.

Ops (branch 04): scheduler job `ledger-integrity` runs VerifyState; on
divergence logs ERROR (WARN if none) — and RepairState only when
config `ledger.auto_repair` (default false; add under [ledger] config
table). Audit: appends of kind action|commitment|resolution get one
auditlog chain row {type:"ledger.append", employee_id, payload} via the
shared chain handle (components.go:5277-5286 precedent, voice dial
WriteAudit voice_wiring.go:596). Log-only rows (observation/committal)
skip the chain to keep it a security trail (master C decision).



## Tasks

1. QueryLog: build SQL from LogQuery (parameterized only); LIMIT clamp.
2. QueryOpen: WHERE status IN (open, waiting_on_me, waiting_on_them)
   ORDER BY updated_at DESC, employee filter required.
3. GetState: single-row fetch, bool ok.
4. FTS: Append already inserts subject + 512B payload snippet — move that
   INSERT into the same tx via a small helper in ops.go called from
   Append (edit the tx block; keep tests §5 GREEN). Add `SearchLog(q,
   term)` helper used by RPC leaf for text search.
5. VerifyState: fold per (employee,subject): iterate ledger rows in id
   order for employees present in state; decode envelopes; latest
   envelope = Want (as_of = that id); compare Have vs Want (status,
   summary, as_of); assemble VerifyReport.
6. RepairState: for each diff, UPSERT Want over Have inside one tx;
   return count.
7. Tail: struct tailer{{ch chan Record; since int64}}; Store keeps
   []*tailer under a mutex; On Append commit (after the
   WithAfterAppend hook) push record to every tailer whose last-delivered
   id < rec.id; Tail registers the tailer, THEN does the catch-up SELECT
   id>sinceID, then dedupes live records by id (monotonic so keep
   lastSeen). Close on ctx.Done or Store.Close (close all tailers).
   Guarantee doc-comment: register-before-read, dedupe-by-id = no gaps,
   possible duplicate at handoff (consumers treat idempotently).

## What This Leaf Exposes to Siblings

`QueryLog/SearchLog/QueryOpen/GetState/VerifyState/RepairState/Tail`.
## Self-Verification Checklist

- [ ] 00T §6-§8 GREEN; -race on the tail tests.
- [ ] Fold uses ONLY envelope rows; log-only rows never change Want.
- [ ] Tail cancel closes without goroutine leak (goleak if repo uses it;
      else runtime.NumGoroutine delta test).

## Review Checklist

- [ ] All pinned signatures/columns present exactly as in contracts.
- [ ] Tests GREEN; `go build ./...` clean; gofmt applied.
- [ ] No artifacts (debug prints, TODOs), no extra files touched.
- [ ] Do NOT commit rule respected (worktree left unstaged).
