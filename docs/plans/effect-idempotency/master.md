# External-Effect Idempotency Ledger - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 leaf documents under this node
- **Scope:** First-class external-effect idempotency for meept agents: a
  durable ledger that atomically claims a stable effect key before any
  irreversible external effect, records a receipt after execution, writes a
  completion marker, and reconciles claimed-but-incomplete effects on resume.

## Goal

The 'Graph and Loop Engineering using Grok Bot' article mandates an effect
protocol for irreversible external actions (publish, delete, send, push):
before the effect, atomically claim a stable effect key; execute once; verify;
store a receipt; only then write a completion marker. On resume, reconcile
every claimed-but-incomplete effect before retrying; auto-retry only when the
provider accepts the same idempotency key, otherwise stop for human
reconciliation.

Meept today has NO effect ledger. Its existing mechanisms solve different
problems:

- **Park/resume** (`agent.TurnParker`, `employee.EpisodeParker`,
  `agent.SQLiteParkStore` over `<data_dir>/parks.db`) — WAIT dedup: a turn
  paused on a provider quota/throttle wait, resumed later. Nothing about
  external side effects.
- **Atomic job claims** (scheduler/queue) — QUEUE dedup: one worker takes a
  job. Says nothing about whether the job's external effects already ran.
- **`internal/tools/builtin/change_journal.go`** — file pre-image journal for
  revert. A different concern (undo local edits, not dedup external effects).

A daemon crash or parked-turn resume between "agent decided to push/send" and
"effect confirmed" can silently drop the effect or double-execute it. This
tree closes that gap.

Deliverables:

1. **Ledger** — new package `internal/effects`: atomic claim via SQLite
   primary-key conflict, receipts, completion markers, pending reconciliation,
   deterministic key helper, in-memory implementation for tests.
2. **Wiring** — the first two external-effect tools wrapped:
   push notification (`internal/services/push_service.go`) and backup git
   push (`internal/backup/git_ops.go`); plus `ReconcilePending` hooks on
   daemon startup and the parked-turn resume path.
3. **Visibility** — CLI `meept effects list` / `meept effects reconcile <key>`
   over RPC, plus docs (`docs/workflows/effects.md`) and an AGENTS.md Key
   Components row. NO new bus topics (see Architecture Overview).

## Architecture Overview

### The ledger

`internal/effects` is a small, storage-first package. The durable store is
SQLite at `<daemon data_dir>/effects.db` (WAL + 5s busy timeout — same DSN
convention as `agent.SQLiteParkStore` over parks.db, `internal/agent/park_store_sqlite.go:126`).
The table `effects_ledger` has `key TEXT PRIMARY KEY`; atomicity comes from
the INSERT PK conflict — a second `Claim` of the same key gets
`granted=false` plus the prior record, never a second row. No mutex is needed
on the SQLite implementation (SQLite + busy_timeout serializes writers); the
in-memory test implementation guards its map with a mutex that is never held
across I/O (mutexio rule).

State machine: `claimed → receipted → completed`, with `claimed`/`receipted`
also able to move to `abandoned` (tool error path or explicit human
reconciliation via CLI). `claimed` means the effect is believed not-yet-run;
`receipted` means it ran and the provider's outcome is stored but the
completion marker is not yet written; `completed` means done — subsequent
claims of that key are idempotent no-ops returning the prior receipt.

### Key canonicalization

`EffectKey(parts ...string)` = sha256 hex over the canonical joining of tool
name + normalized arguments + session/step IDs + round key (exact rule in
Interface Contracts, Contract 2). Keys are content-derived and deterministic
across restarts — they are NOT generated IDs, so the `pkg/id` rule does not
apply here (no `time.Now`/`math/rand` involved anywhere in the package).

### Tool integration

Wired tools wrap their external call in the `RunOnce` guard
(Claim → execute → verify → RecordReceipt → Complete). On
`granted=false` with a completed prior, the tool returns the prior receipt as
an idempotent no-op result. The first two wired tools:

- **(a) Push notification** — `(*PushService).Push` in
  `internal/services/push_service.go` (verified: ~line 145; publishes
  `push.notify` + `push.<session>` bus events; there is no separate
  `internal/services/push` package). `ProviderIdempotent=false` —
  re-publishing would double-render in TUI/Telegram.
- **(b) Backup git push** — `gitPushWithRetry` in `internal/backup/git_ops.go`
  (verified: ~line 162, go-git `repo.Push` with `Op: "git_push"`).
  `ProviderIdempotent=true` — pushing the same commit again is
  `AlreadyUpToDate`, safe to auto-retry. Survey note: the task's original
  target `internal/workspace/git_ops.go` contains NO push —
  `internal/workspace/manager.go:341` states workspace Close never commits or
  pushes. The second push site, `(*GitSync).push` in
  `internal/cluster/git_sync.go:460`, is heartbeat-driven (frequent) and is
  deferred — see OPEN-QUESTIONS.md Q4.

### Resume semantics

On daemon startup AND on parked-turn resume, the daemon runs
`ReconcilePending`. Records stuck in `claimed`/`receipted` are handled by a
small reconciler in `internal/daemon/effects_wiring.go`: auto-retry ONLY for
tools whose `EffectMeta.ProviderIdempotent` is true AND that registered a
re-executor; everything else is surfaced — slog Error + CLI visibility —
NEVER a prompt to the agent and NEVER a new bus topic. Park/resume events
continue to ride the existing `agent.quota_wait` topic family per the
AGENTS.md invariant; effect state is observed via `meept effects list`.

### Surfaces

CLI commands `meept effects list` and `meept effects reconcile <key>` in
`cmd/meept`, backed by RPC methods `effects.list` / `effects.reconcile`
registered on the existing `rpc.Server` (direct `RegisterHandler` closures —
the plan-handler pattern, NOT a bus proxy; see the AGENTS.md live-responder
rule). Docs land in `docs/workflows/effects.md` (the `internal/<pkg>` →
`docs/workflows/<pkg>.md` mapping) and one AGENTS.md Key Components row.

## Interface Contracts

### Contract 1: Ledger API

```
// File: internal/effects/ledger.go
package effects

import (
    "context"
    "encoding/json"
    "time"
)

type EffectState string

const (
    StateClaimed   EffectState = "claimed"
    StateReceipted EffectState = "receipted"
    StateCompleted EffectState = "completed"
    StateAbandoned EffectState = "abandoned"
)

// EffectMeta is caller-supplied context captured at claim time.
type EffectMeta struct {
    TaskID    string          `json:"task_id,omitempty"`
    StepID    string          `json:"step_id,omitempty"`
    SessionID string          `json:"session_id,omitempty"`
    Tool      string          `json:"tool"`
    // ProviderIdempotent: the tool declares the provider accepts the same
    // idempotency key, so reconcile may auto-retry this effect. Tools that
    // cannot safely re-execute (e.g. push notification) leave it false;
    // their stuck records are surfaced for human reconciliation instead.
    ProviderIdempotent bool   `json:"provider_idempotent"`
    // Payload is the effect input (e.g. the marshaled PushRequest). Stored
    // at claim time and returned in EffectRecord so reconcile re-executors
    // can reconstruct the effect after a restart. Contract decision: this
    // column was ADDED to the pinned schema — without it auto-retry cannot
    // rebuild the request. See OPEN-QUESTIONS.md Q6.
    Payload   json.RawMessage `json:"payload,omitempty"`
}

// EffectRecord is a durable ledger row.
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
    // Claim atomically records the intent to execute the effect identified
    // by key. granted=true means this caller owns the effect and must
    // execute it. granted=false means the key already exists: prior carries
    // the existing record (completed prior => return its receipt as an
    // idempotent no-op). INSERT PK conflict is the atomicity mechanism —
    // no separate check-then-insert.
    Claim(ctx context.Context, key string, meta EffectMeta) (granted bool, prior *EffectRecord, err error)
    // RecordReceipt stores the provider outcome and moves claimed ->
    // receipted (executed_at stamped). Unknown key or non-claimed state is
    // an error.
    RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error
    // Complete writes the completion marker: receipted (or claimed) ->
    // completed (completed_at stamped). Idempotent when already completed.
    Complete(ctx context.Context, key string) error
    // Abandon marks the effect dead (claimed/receipted -> abandoned) with a
    // reason folded into the record log. Serves the pinned 'abandoned'
    // state: tool error paths and CLI --abandon. Completed keys cannot be
    // abandoned.
    Abandon(ctx context.Context, key string, reason string) error
    // ReconcilePending returns every record in state claimed or receipted,
    // oldest claimed_at first. Startup and parked-turn resume call this.
    ReconcilePending(ctx context.Context) ([]EffectRecord, error)
    // Get returns one record or (nil, ErrUnknownKey).
    Get(ctx context.Context, key string) (*EffectRecord, error)
    Close() error
}

var ErrUnknownKey = errors.New("effects: unknown effect key")
var ErrInvalidTransition = errors.New("effects: invalid state transition")
```

Owner: 01-effect-ledger.md. Consumers: 02, 03. The four pinned signatures
(`Claim`, `RecordReceipt`, `Complete`, `ReconcilePending`) are exact;
`Abandon` and `Get` are additions required by the pinned `abandoned` state
and the CLI reconcile surface (contract decision, see OPEN-QUESTIONS.md Q1
for the timeout policy that also touches `abandoned`).

### Contract 2: EffectKey canonicalization

```
// File: internal/effects/key.go
package effects

// EffectKey derives a stable, deterministic key: sha256 over the UTF-8
// bytes of strings.Join(normalized(parts), "\x1f"), hex-encoded (64 chars).
// normalized(p) = strings.TrimSpace(p) for every part. The \x1f (unit
// separator) join makes ("ab","c") != ("a","bc"). Order matters: callers
// pass parts in a fixed documented order per tool:
//   EffectKey(toolName, sessionID, stepOrRoundKey, argsHashOrIDs...)
// Empty parts are allowed and simply contribute an empty field. The
// function never allocates randomness and never reads the clock.
func EffectKey(parts ...string) string
```

Owner: 01-effect-ledger.md. Consumers: 02, 03. Canonicalization is sha256 of
the canonical `\x1f`-joined, trimmed parts — this is THE pinned rule; do not
substitute JSON serialization or different separators.

### Contract 3: SQLite schema

```
// File: internal/effects/ledger_sqlite.go
// DSN convention (same as internal/agent/park_store_sqlite.go:130):
//   sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
// Driver: modernc.org/sqlite (already in go.mod — verify with search_files).

CREATE TABLE IF NOT EXISTS effects_ledger (
    key                 TEXT PRIMARY KEY,
    state               TEXT NOT NULL CHECK (state IN
                          ('claimed','receipted','completed','abandoned')),
    task_id             TEXT NOT NULL DEFAULT '',
    step_id             TEXT NOT NULL DEFAULT '',
    session_id          TEXT NOT NULL DEFAULT '',
    tool                TEXT NOT NULL DEFAULT '',
    provider_idempotent INTEGER NOT NULL DEFAULT 0,
    claimed_at          TEXT NOT NULL,   -- RFC3339Nano, like parks.db
    executed_at         TEXT,
    completed_at        TEXT,
    payload             TEXT,            -- raw JSON preserved byte-for-byte
    receipt             TEXT             -- raw JSON preserved byte-for-byte
);

CREATE INDEX IF NOT EXISTS idx_effects_ledger_state
    ON effects_ledger(state);
```

Owner: 01-effect-ledger.md. Consumers: 02 (reconciler), 03 (CLI display).
Times are RFC3339 TEXT (same convention as `parked_turns.resume_at` /
`sessions.created_at`). `payload` was added to the pinned schema — decision
recorded in OPEN-QUESTIONS.md Q6.

### Contract 4: RunOnce guard + tool-integration pattern

```
// File: internal/effects/ledger.go
package effects

// RunOnce wraps one external effect in the pinned protocol. execute runs
// the real effect and returns its receipt (it MUST have verified the
// provider accepted the effect before returning a non-nil receipt).
// Returns:
//   receipt — the stored receipt (freshly executed OR prior completed one)
//   reused  — true when a completed prior existed and execute was NOT run
//             (the idempotent no-op path)
//   err     — claim/execute/record errors
// On execute error the effect is NOT abandoned automatically: the tool
// decides (transient errors stay claimed so reconcile finds them;
// permanently-dead effects call Abandon explicitly).
func Run(ctx context.Context, l Ledger, key string, meta EffectMeta,
    execute func(ctx context.Context) (json.RawMessage, error)) (receipt json.RawMessage, reused bool, err error)

// Tool integration pattern (pseudocode — every wired tool follows this):
//
//   key := effects.EffectKey("backup.git_push", repoPath, headSHA)
//   receipt, reused, err := effects.Run(ctx, ledger, key, effects.EffectMeta{
//       Tool: "backup.git_push", ProviderIdempotent: true, Payload: reqJSON,
//   }, func(ctx context.Context) (json.RawMessage, error) {
//       out, err := doPush(ctx)          // the irreversible call
//       if err != nil { return nil, err } // stays claimed -> reconcile sees it
//       return out, nil                   // out = verified provider receipt
//   })
//   if err != nil { return fmt.Errorf("backup push: %w", err) }
//   if reused {
//       // granted=false with completed prior: return prior receipt as an
//       // idempotent no-op result — never re-execute.
//   }
//   return effects.Complete(ctx, key)
```

Owner: contract defined by 01, consumed by 02. Never let a tool invent its
own claim/complete sequence around the guard.

### Contract 5: Reconcile semantics + daemon wiring

```
// File: internal/daemon/effects_wiring.go (new; owner leaf 02)
//
// Components gains: EffectsLedger *effects.Ledger-ish (opened over
// <data_dir>/effects.db in NewComponents, closed in stopComponents —
// mirror the ParkStore field/lifecycle at internal/daemon/components.go:139).
//
// type effectReExecutor func(ctx context.Context, rec effects.EffectRecord)
//     (json.RawMessage, error)
// Registered per tool name, ONLY for tools with ProviderIdempotent=true
// (initially: backup git push).
//
// Reconcile(ctx) runs:
//   pending := ledger.ReconcilePending(ctx)
//   for each rec:
//     - executor registered AND rec.ProviderIdempotent
//         -> re-execute via Run(ctx, ledger, rec.Key, meta, executor)
//            success => receipted+completed; failure => stays, logged.
//     - else: slog.Error("effect pending reconciliation", key, tool,
//            age, ...) and count it as SURFACED for CLI visibility.
//   returns counts (retried, surfaced, failed).
//
// Invocation points:
//   1. Daemon startup: synchronous-with-30s-timeout call from
//      internal/daemon/daemon.go New() after RPC handler registration
//      (~line 796). Errors are logged, never fatal.
//   2. Parked-turn resume: best-effort call at the top of
//      (*ChatHandler).resumeQuotaParkedTurn (internal/agent/handler.go:1648)
//      and resumeParkedTurn (internal/agent/handler.go:1885), via a nil-
//      guarded SetEffectsReconciler setter. Errors logged; resume proceeds.
//
// INVARIANT: no new bus topics. Park/resume events ride the existing
// agent.quota_wait topic family; effect state is observed via
// `meept effects list` (RPC), never via prompts and never via a new
// topic prefix.
```

Owner: 02-tool-wiring.md. Consumers: 03 (CLI reads the same ledger).

### Contract 6: RPC + CLI surface

```
// RPC (internal/rpc/effects.go, registered from daemon.go next to the plan
// handler registration ~lines 785-796, direct RegisterHandler pattern):
//   effects.list     params {"state"?: string, "key"?: string}
//                    -> {"effects": [EffectRecord...]}   (oldest first)
//   effects.reconcile params {"key": string,
//                             "action": "complete"|"abandon",
//                             "receipt"?: json, "reason"?: string}
//                    -> {"record": EffectRecord}
//                    complete = optional receipt overwrite + Complete();
//                    abandon = Abandon(reason). Unknown key => error.
//
// CLI (cmd/meept/effects.go, newEffectsCmd() in the cobra style of
// cmd/meept/plans.go; registered in cmd/meept/main.go's AddCommand block
// ~lines 145-194):
//   meept effects list [--state=claimed|receipted] [--json]
//   meept effects reconcile <key> --complete [--receipt '<json>']
//   meept effects reconcile <key> --abandon --reason '<text>'
//   meept effects reconcile <key>            # no flag: show record + hint
// CLI output text is lowercase (AGENTS.md UI convention).
```

Owner: 03-cli-reconcile.md.

### Contract 7: Docs + AGENTS.md

- `docs/workflows/effects.md` (new) — the `internal/effects` →
  `docs/workflows/<pkg>.md` mapping; covers the protocol, key rule, wired
  tools, reconcile behavior, CLI usage.
- `AGENTS.md` Key Components table gains one row after **Session**:
  `| **Effects** | `internal/effects` (external-effect idempotency ledger) |`.
- Owner: 03-cli-reconcile.md.

## Dispatch Protocol

Three sequential phases (each child depends on the previous). One child per
batch — no concurrency here.

### Phase 1: Dispatch leaf 01

1. **Read** 01-effect-ledger.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-effect-ledger.md"
   - Context: full leaf text + Contracts 1-4 + Coding Conventions below +
     INLINED: `internal/agent/park_store_sqlite.go` lines 90-150 (DSN +
     schema + constructor conventions) and the current `go.mod` sqlite
     driver line.
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include the no-read_file clause from the leaf's DISPATCH INSTRUCTION.

### Phase 2: Review leaf 01, then dispatch leaf 02

Review in-session (main model, NOT a delegated subagent): read the changed
files, check against Contracts 1-4 and the Review Checklist, run
`go test -p 2 ./internal/effects/...` and
`go test -p 2 -race ./internal/effects/...`. If gaps: re-dispatch with
specific feedback (max 3 cycles, then halt and report). If pass: update the
tracking table (the orchestrator commits per this tree's parent process).

Then dispatch 02-tool-wiring.md the same way, with INLINED: the leaf's
verified anchor snippets (PushService.Push, gitPushWithRetry, handler.go
resume methods, components.go ParkStore lifecycle) and Contracts 1-5.

### Phase 3: Review leaf 02, then dispatch leaf 03

Same review protocol; run the leaf's test commands plus
`go build ./cmd/meept ./cmd/meept-daemon ./internal/daemon`. Then dispatch
03-cli-reconcile.md with Contracts 6-7 + the cmd/meept plans.go registration
style INLINED.

### Phase 4: Integration review

After ALL children reach REVIEWED:

1. `go build ./...` and `go test -p 2 ./internal/effects/... ./internal/services/... ./internal/backup/... -count=1`.
2. Verify the end-to-end protocol: double-push dedup (push test), restart
   reconcile (ledger persistence + reconciler), CLI round-trip.
3. `make lint-ci` (golangci-lint + mutexio + predid + audit scripts).
4. gofmt; verify no line-number corruption:
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' .` returns zero.
5. Update the tracking table to COMPLETE.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-effect-ledger.md | leaf | none | 70K | A |
| 02 | 02-tool-wiring.md | leaf | 01 | 90K | B |
| 03 | 03-cli-reconcile.md | leaf | 02 | 60K | C |

**Concurrency groups:** strictly sequential A → B → C (each leaf consumes the
previous leaf's compiled package).

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied (exact pinned
      signatures — `Claim`, `RecordReceipt`, `Complete`, `ReconcilePending`,
      `EffectKey` — byte-for-byte)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed; `go test -p 2` used)
- [ ] Double-claim race test present and passing under `-race`
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues (receipt/payload JSON preserved
      byte-for-byte; no secrets logged)
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values,
      no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files
- [ ] No new bus topics introduced (grep the diff for `Publish(` additions)

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (server). Module `github.com/caimlas/meept`.
- **Error handling:** wrap with `fmt.Errorf("context: %w", err)`; never
  `_ = someFunc()` (pre-commit blocks new ignored-error sites); never bare
  `panic(err)`.
- **Two-value type assertions** on `map[string]any` bus payload values.
- **Mutex scope:** never hold a mutex across I/O; collect-under-lock then
  operate (`mutexio` analyzer enforces). The SQLite ledger needs no mutex;
  the in-memory ledger locks only around map access.
- **Setters:** every `Set*` method gets a nil guard (setters_test.go
  pattern) — applies to `SetEffectsLedger`, `SetEffectsReconciler`.
- **IDs:** never `time.Now().UnixNano()`/`math/rand`; use `pkg/id.Generate()`
  where an ID is needed. Effect KEYS are content-derived (Contract 2), not
  generated IDs.
- **Logging:** `log/slog` with structured keys (`"key"`, `"tool"`, `"state"`),
  matching `internal/employee` / `internal/daemon` style.
- **Tests:** table-driven where natural; `_test.go` alongside; SQLite tests
  on tempdir files (persistence cases) and `:memory:` (pure logic cases);
  always `go test -p 2 ./internal/effects/...` (macOS ephemeral-port rule:
  always `-p 2`).
- **Config:** JSON5 with quoted keys; no new config keys in this tree.
- **CLI/UI text:** all lowercase.
- **Docs:** `internal/<pkg>` changes map to `docs/workflows/<pkg>.md`
  (leaf 03 owns consolidation).
- **Formatting:** `gofmt` before reporting completion.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-effect-ledger | PENDING | 0 | |
| 02-tool-wiring | PENDING | 0 | |
| 03-cli-reconcile | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. **Package:** `go test -p 2 ./internal/effects/... -count=1` — key
   canonicalization table, ledger state-machine table, double-claim,
   reconcile ordering.
2. **Race:** `go test -p 2 -race ./internal/effects/...` — concurrent
   `Claim` of the same key on the SQLite store: exactly one `granted=true`
   (PK-conflict atomicity), plus in-memory ledger concurrent guard use.
3. **Persistence:** close + reopen a tempdir SQLite ledger; a record left
   `claimed` before "restart" is returned by `ReconcilePending` after.
4. **Tool boundary (push):** with the in-memory ledger and a fake bus,
   calling `PushService.Push` twice with identical inputs yields one bus
   publication and one receipt; the second call reports the prior receipt
   (`reused=true`).
5. **Tool boundary (backup push):** with a local bare remote (seed pattern
   from `internal/workspace/manager_test.go`), one push produces a receipt;
   re-running the same key does not push again (remote ref unchanged).
6. **Startup reconcile smoke:** daemon construction runs the reconciler once;
   surfaced pending effects appear in `effects.list` output; log contains
   `effect pending reconciliation`.
7. **CLI round-trip:** `meept effects list --json` parses as JSON;
   `effects.reconcile --abandon` flips a claimed record to abandoned via the
   RPC handler test.
8. **Analyzers:** `make lint-ci` green; line-number-corruption grep zero.

## Open Questions

See OPEN-QUESTIONS.md (abandoned-state timeout policy, additional tools to
wire later, retention, cluster heartbeat push, audit-finding integration;
resolved forks: payload column, workspace-push target replacement).
