# internal/kanban/AGENTS.md

Guidance for AI agents working in `internal/kanban/`. Referenced from the
root AGENTS.md; the full board-projection contract lives here. Update both
in the same commit per the root maintenance rule. User-facing docs:
`docs/workflows/kanban.md` (feature + workflows), `docs/workflows/kanbansvc.md`
(sidecar), `docs/workflows/meept-kanban.md` (sidecar binary). Plan tree:
`plans/kanban-board/` (contract ids C1–C10 referenced below are verbatim
from `plans/kanban-board/master.md`).

## The board is a projection; the machines are the truth

The board tables (`kanban_boards` / `kanban_columns` / `kanban_cards` /
`kanban_projections` / `kanban_card_events`, `baseSchema` in
`store.go:60-112`)
are **rebuildable projection state, never a source of truth** (master.md
decision 2: "after rebuild, board == source"). Concretely:

- `kanban_projections` is the mapping **authority** (C3): the reconciler
  reads it and never rewrites it; the rebuild target is `kanban_cards`
  (`reconcile.go:28-33`). A rule naming a missing column is inert — skipped.
- Reconcile (`reconcile.go`, leaf 03/02) is the AUTHORITATIVE lane and
  idempotent: a second pass over an unchanged source changes zero rows
  (`ReconcileStats` all-zero, `reconcile.go:85-96`). The projector is the
  LIVE lane; reconcile is its recovery for events lost under adversarial
  timing (`reconcile.go:22-27`, pinned by `TestReconcile_KillProjectorRestart`).
- Deletion classes are only true orphans and duplicates past the
  first-by-id; a parked card (current state matches no rule) stays
  UNTOUCHED; NULL-ref cards are manual and excluded from the scan
  (`reconcile.go:34-38`). Enumeration errors skip a kind WHOLESALE —
  a board that cannot see a machine must not delete that machine's cards
  (`TestReconcile_EnumerationErrorNeverDeletes`).
- Reconcile publishes NOTHING on the bus; `kanban.board.changed` stays
  with the projector + human lane (C2) (`reconcile.go:47-48`).
- Writes are linearised, not locked: the Store is single-connection
  (`store.go:41` `SetMaxOpenConns(1)`), the projector's worker goroutine
  is the sole card writer and `mu` guards counters only
  (`projector.go:137-139`), and the reconcile
  batch lands in one transaction (`reconcile.go:43-46`).

The SQLite DSN uses ONLY the modernc `_pragma=name(value)` form (WAL,
busy_timeout, synchronous(NORMAL), foreign_keys(1), `_txlock=immediate`)
— `store.go:29-35`; mattn-style keys are silently dropped by
modernc (`scripts/audit-sqlite-dsn-pragmas.py`). `foreign_keys(1)` is
required: the card→timeline cascade deletes (`store.go:103-111`) are
load-bearing for reconcile orphan removal.

## C1: events carry identity only; provenance is resolved at projection

`StateEvent{Kind, ID, From, To, Reason, TS}` (`types.go:41-48`) is the
whole wire shape. Provenance (`goal_id`, `employee_id`, `plan_id`,
`session_id`, `trigger_ref`) is NEVER on the bus: the projector reads the
source work-item row keyed by `StateEvent.ID` at projection time through
the injected `RowFetcher` seam (`projector.go:48-63`); reconcile carries
the same rule via `ListFetcher`/`WorkItemRef.Row` (`reconcile.go:50-78`).
A `noopFetcher` (empty provenance, generated title) is the nil-safe
default — projection never dies on an unwired source
(`projector.go:66-73`, `TestReconcile_StoreWithoutWiredFetcherIsInert`).

**Import ban (enforce-by-convention; verify with `go list -deps`):**
`internal/kanban` may NEVER import `internal/task`, `internal/queue`,
`internal/plan`, `internal/employee` or `internal/tui`
(`projector.go:49-50`). The committed package imports exactly
`internal/bus`, `internal/config` + `internal/auth` (authz.go only), and
`pkg/id` — every
machine-side question is asked through an injected interface
(`RowFetcher`, `ListFetcher`, `LeaseAuthority`), never through an import.
The real implementations of the fetchers and the authority close over the
machine stores and live in daemon wiring (leaf 07/01, not yet committed).

## The rule key is FROM-state

Projection rules map `(kind, from_state) -> column` — the state the
transition LEAVES (`projection_rules.go:14-31`: "(task, pending) ->
doing" means "when a task leaves pending, its card goes to doing").
Consequences:

- `WorkItemRef.State` for reconcile is the from_state of the transition
  that LAST placed the item, derived from the machine's state graph —
  both lanes then resolve the same column for the same item, which IS
  "board == source" (`reconcile.go:56-62`).
- An unmapped state creates no card ("no card until the first mapped
  transition") and never moves or deletes an existing card — the card
  parks in its last mapped column (`projector.go:120-124`).

## Publisher gates live in the MACHINE packages, not here

This package only DECLARES the five typed topics and the nil-safe publish
helpers (`events.go:74-166`). The opt-in gate `SetKanbanEventPublishing`
lives with each publisher machine — `internal/queue/queue.go:184`,
`internal/task/registry.go:91`, `internal/plan/manager.go:1826`,
`internal/employee/goal.go:720` — default false = zero events constructed,
byte-identical behavior. Publishers must only emit AFTER the store
confirms the transition (the machines' CAS predicates are the arbiter),
never for idempotent/superseded or same-state dispositions, and skip the
event rather than publish a lying `From` when the pre-state is unreadable
(`docs/workflows/kanban.md` "Publisher rules"). Do not move gate logic
into `internal/kanban`.

## Lease rule A (C10): human-only, fail-closed, audit-before-effect

- **The projector performs NO lease check** — it mirrors transitions the
  machines already committed, including legitimate claim acquisitions
  (`projector.go:107-113`). Every refusal is the HUMAN lane
  (`HumanCommands`, `human.go`) enforcing decision 1: a human
  `card.move` into a column that a projection rule maps into the card's
  kind's active lane is REFUSED with `ErrLeaseHeld` while the work item
  holds a live claim (`human.go:425-450`). A column no rule maps into is
  purely manual and does not engage the rule (`columnIsProjectedTarget`,
  `human.go:499`).
- **Fail-closed lookup**: a `LookupLease` ERROR refuses the move — a
  board that cannot answer "is this claimed?" must not move claimed
  work (`human.go:431-437`; pinned by `TestHumanMove_LeaseLookupErrorFailsClosed`,
  `human_test.go:234`).
- **Takeover order is frozen**: audit row FIRST (`kanban_card_events`
  kind=takeover), then cancel the episode, then release the token, then
  the effect — `CancelEpisode` before `ReleaseToken` so a worker cannot
  renew a dropped token (`human.go:238-252`, `LeaseAuthority` doc
  `human.go:84-90`; pinned by `TestHumanTakeover_AuditBeforeEffectOrdering`,
  `human_test.go:456`). An authority failure aborts before any card
  mutation and the audit row STAYS (it records the attempt,
  `TestHumanTakeoverMove_AuthorityFailureLeavesCardUnmoved`). Takeover is
  the ONLY lease bypass (`TakeoverMove` stamps `LeaseBlocked` so surfaces
  can tell a forced move from an ordinary one, `human.go:281-295`).
- The nil-default is `noopLeaseAuthority` (no lease ever held, board fully
  usable pre-07/01) and setter injection must keep the typed-nil guard
  `isNilish` (`human.go:93-118`).

## WIP posture is split by lane (C9)

- **Projection writes always land.** An over-WIP projection move is NOT
  dropped or blocked: the write lands, a Warn logs, `WIPOverflow`
  increments — silently parking a card would make board != source
  (`projector.go:114-119`, `ProjectorStats` `projector.go:88-93`).
- **Human moves obey `wip_mode`**: `block` refuses with `ErrWIPFull`
  BEFORE the write; `warn` moves, appends a `wip_warning` timeline row,
  and carries the text in the `kanban.board.changed` `Note` field
  (`human.go:450-491`; enum mirrors config without importing it,
  `human.go:33-38`, validated identically to
  `internal/config/schema.go:269-275`). `Column.WIP == 0` is unlimited.

## Error taxonomy: the C1 strings are a wire contract

`ErrLeaseHeld` ("lease held by <holder> since <RFC3339>"), `ErrWIPFull`
("WIP limit reached for column <id>: <limit>"), `ErrUnknownCard`
("unknown card: <id>") are FROZEN text (`types.go:58-82`). THREE
surfaces regex-parse exactly those strings off the transport-error path:
CLI `kanbanError` (`cmd/meept/kanban.go:477-478`), TUI
`kanbanRefusalMessage` (`internal/tui/kanban_view.go:547-569`), and Dart
`_parseLeaseHeld`/`_parseWipFull` behind the gateway's 502
(`ui/flutter_ui/lib/features/kanban/kanban_service.dart:610-626`). The
mapping note is `internal/rpc/kanban.go:43-53`: refusals ride the ERROR
path, never a success-body envelope — changing one string breaks three
clients. Refusals publish nothing; timeline rows + the two C2 publishes
land only on a successful move (`human.go:472-491`).

## Grants (C5)

Three string grants `board.read` / `board.mutate` / `board.takeover`
(`authz.go:9-13`). `multiuser.enabled=false` (or a nil auth store)
grants ALL THREE to any principal — owner-only mode (`authz.go:22-37`,
`authz.go:56-58`). The takeover lane is conjunctive downstream (mutate
AND takeover) — enforced in the gateway (`internal/kanbansvc`) and the
RPC table (`internal/rpc/kanban.go:161-173`), never assumed here.

## Deadcode-stock status (leaf 07/01 pending)

The package is fully committed (leaves 01–03, 04/01) but NOT yet
daemon-wired: `internal/kanban` entries in `.githooks/deadcode-baseline.txt`
(e.g. `Store.SetProjectionRule`, `RegisterDefaultReconciler`, the
`Manager`/`HumanCommands` methods) are the honest census of functions
waiting for the 07/01 wiring (fetchers, LeaseAuthority binding,
`RegisterDefaultReconciler` call site, `RunPeriodicReconcile` ticker,
gate flips). Do not delete them as dead; do not widen the ratchet
baseline for anything else.

## Verification

```
go test -p 2 -race -count=1 ./internal/kanban/...
go list -deps ./internal/kanban | grep -E 'internal/(task|queue|plan|employee|tui)'   # must be empty
```
e2e: suite `kanban-board` (`e2e/manifest.json` maps `internal/kanban/` to it).
