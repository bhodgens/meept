# Employee Transactional Ledger — Master Plan

Date: 2026-11-09. Repo: meept. Tree: docs/plans/20261109-employee-ledger/

## Goal

Give every meept employee a durable, queryable record of "what was
communicated and what was done": an append-only ledger with per-subject
current state, one write path, one sanctioned reader surface, voice
call-end briefs as its richest writer, and self-healing integrity.
Design provenance: uptractor jobfile (shared store + provenance tags +
redaction), refined through sessions on 2026-11-09 (see decisions below).

## Architecture Overview

Four branches, executed 01 -> {02, 03 parallel} -> 04:

```
01-store     internal/ledger package: schema, Append+transitions,
             Query/Verify/Tail. Foundation; no other branch compiles
             against reality until it is COMPLETE.
02-surfaces  ledger_append / ledger_query tools; RPC read surface;
             S4 record-changed publish.
03-voice     per-call structured brief at ReleaseHook (voice_employee.go:447);
             brief action items -> commitment records; daybrief.go consumes
             open commitments (also removes phone numbers from LLM prompts).
04-ops       scheduled VerifyState integrity pass; audit-chain entries
             for state-changing appends.
```

Decisions frozen by the requesting owner (do NOT re-litigate in leaves):
- D1 Single physical file, employee attribution via `employee_id` column;
  API scoping mandatory; subject-wide cross-employee reads deferred.
- D2 Bus is NOT the record. Refactoring internal/bus for durability was
  considered and rejected (drop-on-full semantics, tree-wide blast
  radius, queries/validation still unsolved). The ledger gets
  rowid-as-offset Tail; the bus stays best-effort announcement.
- D3 Reuse frozen S4 topic `storage.record.changed` with Store="ledger".
  No new topic name.
- D4 Status/Kind vocabularies and the AllowedTransitions map as pinned
  in Interface Contracts below.
- D5 Voice brief branch stays inside this tree (the ledger without its
  richest writer is a schema waiting for a purpose).
- D6 Out of scope: inbound caller-authorization gate; TUI/Flutter/push
  display-boundary number redaction. Separate trees.

## Coding Conventions

- Go, gofmt; package doc comment on every new package file (house style,
  see internal/voice/callstore.go header).
- Errors wrapped with package prefix: `fmt.Errorf("ledger: ...: %w", err)`.
- SQLite: modernc driver only; SetMaxOpenConns(1); RFC3339 TEXT times;
  guarded-ALTER idiom for future migrations (callstore.go:118-130).
- Tests: table-driven, stdlib + testify per repo convention (check
  existing _test.go imports first; callstore tests use stdlib).
- NO git commit / git add by implementation agents (orchestrator commits).
- Do NOT read files through read_file then re-write with line-number
  risk; use patch tool or full-file write from source text given in
  the leaf.

## Interface Contracts (FROZEN — every leaf quotes these verbatim)

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

## Child Index

| Child | Type | Deps | Est. ctx | Notes |
|---|---|---|---|---|
| 01-store/00T-store-tests.md | test-gate | — | 45K | full unit suite for branch 01 |
| 01-store/01-schema.md | leaf | 00T | 30K | store.go: types+schema+Open/Close |
| 01-store/02-append-transitions.md | leaf | 01 | 40K | transitions.go + Append |
| 01-store/03-query-verify-tail.md | leaf | 02 | 50K | query/verify/repair/tail |
| 02-surfaces/00T-surfaces-tests.md | test-gate | 01 | 35K | tool arg validation + envelope tests |
| 02-surfaces/01-tools.md | leaf | 00T | 35K | builtin/ledger.go |
| 02-surfaces/02-rpc.md | leaf | 01 | 30K | ledger.query RPC (seam pattern) |
| 02-surfaces/03-publish.md | leaf | 02 | 25K | S4 publish on commit |
| 03-voice/00T-voice-tests.md | test-gate | 01 | 45K | brief builder + refusal + seam tests |
| 03-voice/01-brief.md | leaf | 00T,02-03 | 50K | voice_brief.go + ReleaseHook attach |
| 03-voice/02-commitments.md | leaf | 01 | 35K | brief -> commitment/observation appends |
| 03-voice/03-daybrief-consumer.md | leaf | 02-03 | 40K | seam + PromptBlock swap, number removal |
| 04-ops/01-integrity-job.md | leaf | 01 | 30K | scheduler job + config key |
| 04-ops/02-audit-entries.md | leaf | 02 | 30K | chain rows for state-changing appends |

Dependency graph is acyclic. Parallelism: after 01-store COMPLETE,
branch 02 and 03 leaves dispatch concurrently (shared-file check: 02-03
and 03-01/03-03 all touch internal/ledger or internal/daemon wiring but
each in DIFFERENT files; the orchestrator serializes anything touching
ledger_wiring.go — 03-01, 03-03, 04-02 land it in order of return, one
rebase re-check each).

## Dispatch Protocol

1. Create isolation worktree FIRST (mandatory):
   `git worktree add .hermes/worktrees/employee-ledger -b plan/employee-ledger`
   (verify .hermes/ is gitignored in meept; if not, add it locally).
2. Per branch: dispatch that branch's `00T` test leaf first ("tests
   ONLY, must fail RED for missing implementation; Do NOT commit").
   Control agent = this orchestrator, IN-SESSION: validate returned
   tests against the branch Test Gate matrix; lesser artifact yields to
   greater; commit gate tests; mark TESTS_VALIDATED.
3. Dispatch implementation leaves up to the concurrency cap, each with:
   leaf path, full Interface Contracts block above, worktree absolute
   path as cwd, "Do NOT commit", exact verify command
   (`go build ./... && go test ./internal/<pkg>/... -count=1`).
4. Review in-session (main model): read changed files, verify contracts
   verbatim, run package tests. On gaps: re-dispatch with specific
   feedback (max 3 loops). Reset status to IN_PROGRESS on re-dispatch.
5. Commit per leaf with pathspec: `git commit -- <exact files>` (never
   bare `git commit` — sibling sessions share this repo).
6. Update the Completion Tracking Table after every transition.

## Test Gate

| Feature | Unit tests | e2e/integration | Status |
|---|---|---|---|
| Schema + Open idempotent reopen | 00T-store-tests §1 | — | PENDING |
| Append validation matrix (kinds/sources/transitions/resolves) | 00T §2-4 | — | PENDING |
| State UPSERT + envelope | 00T §5 | — | PENDING |
| QueryLog / QueryOpen / GetState | 00T §6 | — | PENDING |
| VerifyState fold + RepairState | 00T §7 | 04-01 job smoke | PENDING |
| Tail catch-up + live, no-gap | 00T §8 | — | PENDING |
| Tool arg schema + ctx stamp, no employee_id arg | 00T-surfaces §1 | — | PENDING |
| RPC ledger.query | 00T-surfaces §2 | — | PENDING |
| S4 publish after commit (order + content) | 00T-surfaces §3 | — | PENDING |
| Brief JSON contract + refusal rule | 00T-voice §1 | fake-LLM loop | PENDING |
| Brief -> commitments provenance (source=voice, source_ref=ccid) | 00T-voice §2 | — | PENDING |
| daybrief seam swap + NO phone digits in block | 00T-voice §3 | e2e voice-local | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 01-store/00T | PENDING | | |
| 01-store/01 | PENDING | | |
| 01-store/02 | PENDING | | |
| 01-store/03 | PENDING | | |
| 02/00T | PENDING | | |
| 02/01 | PENDING | | |
| 02/02 | PENDING | | |
| 02/03 | PENDING | | |
| 03/00T | PENDING | | |
| 03/01 | PENDING | | |
| 03/02 | PENDING | | |
| 03/03 | PENDING | | |
| 04/01 | PENDING | | |
| 04/02 | PENDING | | |

## Integration Test Plan

After all leaves REVIEWED, in the worktree:
1. `go build ./... && go vet ./...`
2. `go test ./internal/ledger/... ./internal/tools/... -count=1`
3. `go test ./internal/voice/... ./internal/daemon/... -count=1`
4. Live end-to-end: start daemon against a temp DataDir, append via
   `ledger_append` in a CLI turn (or test client), call `ledger_query`
   mode=open, end a fake voice call through the release hook with a
   canned transcript, assert a brief + commitments row exist with
   source=voice, source_ref=ccid, and assert the day-brief block has no
   `\+\d` digit run.
5. Kill daemon mid-append-test (SIGKILL after INSERT of 1 of 2 queued
   appends); reopen; VerifyState == 0 divergent (transaction proof).
6. Repo gates: npm-equivalents here = `make lint` / golangci if present
   in the repo Makefile; run what exists.

## Review Checklist

- [ ] Contract block copied verbatim into every dispatch context.
- [ ] No leaf added fields/columns not in the frozen schema.
- [ ] Employee identity NEVER accepted as tool argument (grep tool args).
- [ ] Append is single-transaction; publish strictly after commit.
- [ ] No new bus topic strings; only S4 reuse.
- [ ] daybrief section swap gated on seam non-nil (byte-identical
      behavior when ledger absent).
- [ ] No phone number reaches PromptBlock in any test.
- [ ] Orchestrator-only commits; per-file pathspec; no `git add -A`.
- [ ] No debug prints / TODO placeholders in returned diffs.

## Open Questions

- Q1 Fold derivation assumes status-carrying rows are exactly envelope
      rows; if a repair sees divergence caused by a crash BETWEEN commit
      of ledger and state (impossible: same tx) — VerifyState remains
      belt-and-braces, keep.
- Q2 Cross-employee shared-subject convergence deferred (D1); revisit
      only if real demand appears.
- Q3 ledger_append tool name may collide with a constitution deny-list
      default; check [employees] tool policy seeding when wiring.
