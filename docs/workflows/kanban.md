# Kanban board (internal/kanban + workflow publishers)

Feature docs for the kanban-board plan tree (`plans/kanban-board/`).
The sidecar gateway has its own doc: `docs/workflows/kanbansvc.md`.

## What it is

The board is a **rebuildable projection** over the work-item machines
(tasks, queue jobs, plans, steps, goals). The machines stay the single
source of truth; the board never owns state. A projector (branch 03,
landing next) subscribes to state-change events and maintains
board/column/card rows in `internal/kanban` (SQLite WAL, FK cascades,
schema in `internal/kanban/store.go`, migration-owned by leaf 01/01).

## Event contract (master.md C1/C2)

State changes ride typed `internal/bus` topics with payload
`kanban.StateEvent{Kind, ID, From, To, Reason string; TS time.Time}`.
The event carries identity only. Provenance (`goal_id`, `employee_id`,
`plan_id`, `session_id`, `trigger_ref`) is NOT on the wire: the
projector reads the source work-item row keyed by `StateEvent.ID` at
projection time.

Topics live in `internal/kanban/events.go`:

| Topic | Kind | Publisher (leaf) |
|---|---|---|
| `kanban.event.task.state` | task | task Registry: UpdateState, CancelTask, handleCancel (02/03) |
| `kanban.event.job.state` | job | PersistentQueue: Claim, MarkProcessing, CompleteAttempt, FailAttempt, Retry, Requeue (02/03) |
| `kanban.event.plan.state` | plan | 02/04 |
| `kanban.event.step.state` | step | 02/04 |
| `kanban.event.goal.state` | goal | 02/04 |

Reason labels are pinned constants: `claim`, `processing`, `complete`,
`fail`, `retry`, `requeue`, `cancel`, `update-state`.

Helpers `PublishTaskState` / `PublishJobState` fill `Kind` and `TS`
defaults and treat a nil bus as a silent no-op, so publishers are
one-liners and test harnesses without a bus never panic.

## Publisher rules (02/03)

- Publish ONLY after the store confirms the transition (the machines'
  CAS predicates are the arbiter — a rejected write publishes nothing).
- Idempotent/superseded dispositions (e.g. `CompleteAttempt` retry with
  the same token) publish nothing; a second card event would
  double-count downstream.
- Same-state writes publish nothing.
- A pre-state that cannot be read skips the event rather than
  publishing a lying `From`. The periodic rebuild/reconcile (leaf
  03/02) is the backstop that re-derives cards from the rows.

## Opt-in gate

Both publishers are gated per machine instance:
`PersistentQueue.SetKanbanEventPublishing(bool)` and
`Registry.SetKanbanEventPublishing(bool)`. Default is false: zero
events are constructed or published and behavior is byte-identical to
pre-kanban builds. The daemon turns the gate on only when the kanban
feature is enabled; that wiring is leaf 07/01. Unit tests drive both
sides through the setters.

## Grants (C5, leaf 04/01)

Three string grants: `board.read`, `board.mutate`, `board.takeover`
(`internal/kanban/authz.go`). With `multiuser.enabled=false` the owner
holds all three. Consumers: the sidecar gateway
(`internal/kanbansvc`, see kanbansvc.md) and the `kanban.*` RPC
handlers (leaf 05/01).

## Config (leaf 01/02)

`[kanban]` section, `KanbanConfig` in `internal/config/schema.go`:
opt-in, `sidecar_enabled` defaults false; `sidecar_bind` defaults
loopback (C7 bind guard refuses a non-loopback bind without an
explicit opt-out).

## Sidecar spawn supervision (leaf 04/04)

`[kanban].sidecar_enabled` (requires `[kanban].enabled` and
`[transport].rpc.enabled`; either missing logs a Warn and skips the
spawn) makes `Daemon.Run` boot a supervised `meept-kanban` child via
`internal/daemon/kanban_boot.go` (`StartKanbanSidecar`), mirroring the
voice-child pattern by REUSE: the child is an `llm.RuntimeProcess`
(endpoint key `kanban-sidecar`, pid file + durable spawn record under
`$MEEPT_HOME/run`, AutoStop=true), so the startup orphan sweep reaps a
leaked sidecar before spawn, the ownership token keeps a LIVE other
instance's sidecar un-killable, and a hard-killed daemon takes its own
child down through the parent-death supervisor. Binary resolution
follows the voice order: explicit path, daemon-adjacent,
`$MEEPT_HOME/bin` — never PATH. The child is passed
`-daemon-socket <rpc socket> -bind <sidecar_bind>` (+`-allow-lan` only
when the bind is a deliberate non-loopback). Unexpected exits respawn
with exponential backoff, capped at 5 attempts in 60s, then one ERROR
give-up (the gateway stays down until the next daemon start).
`Daemon.shutdown` stops it gracefully (SIGTERM group, the sidecar's own
5s HTTP drain, then SIGKILL) BEFORE the RPC socket closes. Disabled
(default): no resolve, no process, no pid file — byte-identical boot.
A spawn failure is non-fatal: the board gateway degrades, never the
daemon.

## Verification

- Coverage pins: `internal/kanban/events_test.go` walks both transition
  tables and asserts every legal edge has a publisher path;
  `internal/queue/kanban_events_test.go` and
  `internal/task/kanban_events_test.go` pin gate-on emission, gate-off
  silence, and the publish-nothing dispositions.
- e2e: suite `kanban-board` (`e2e/suites/kanban-board/`) covers the
  real sidecar binary; manifest maps `internal/kanban/`,
  `internal/kanbansvc/`, `cmd/meept-kanban/` to it.
