# Kanban board (internal/kanban + workflow publishers)

Feature docs for the kanban-board plan tree (`plans/kanban-board/`).
Package invariants: `internal/kanban/AGENTS.md` and
`internal/kanbansvc/AGENTS.md`. The sidecar gateway has its own doc:
`docs/workflows/kanbansvc.md` (+ `docs/workflows/meept-kanban.md`).

## What it is

The board is a **rebuildable projection** over the work-item machines
(tasks, queue jobs, plans, steps, goals). The machines stay the single
source of truth; the board never owns state. The projector
(`internal/kanban/projector.go`, leaf 03/01) subscribes to state-change
events and maintains one canonical card per work item; the reconciler
(`internal/kanban/reconcile.go`, leaf 03/02) is the authoritative
full-scan rebuild that repairs drift ("after rebuild, board == source").
Store: SQLite WAL + FK cascades, schema in `internal/kanban/store.go`
(leaf 01/01).

## Status (as-built)

Everything below is committed and tested. The 07-daemon-wiring/01 pass
LIVES the wiring: `internal/daemon/kanban_wiring.go` constructs the full
stack when `[kanban].enabled` (default false = byte-identical boot —
no store file, no goroutine, no subscriber, no RPC registration). On
enable it opens the store at `$MEEPT_HOME/kanban/board.db` (migrates on
open, `MaxOpenConns(1)`), builds Manager + HumanCommands with the real
C10 `LeaseAuthority` over the queue/task machinery (job claim state +
task interrupt + plan synthesis lease + goal activity; takeover cancels
the episode then requeues the claim), ensures the default board
(`"meept board"` with todo/doing/done), seeds the default projection
ruleset once (idempotent upsert; 30 (kind, from_state) -> column rules
documented at the top of kanban_wiring.go), starts the Projector on the
five `kanban.event.*.state` topics with the real daemon `RowFetcher` +
`ListFetcher` (the enumeration lane reports the from_state of the last
MAPPED transition — `kanbanFromState` — so reconcile and the projector
resolve one column per item), arms the five machine publishers
(`SetKanbanEventPublishing(true)` + bus installs), registers the
thirteen C4 methods plus the C7 pairing `kanban.pair.mint`/
`kanban.pair.exchange` owner-socket helpers, binds the kanban_* tool
provider via `builtin.SetKanbanProvider` before `toolprov.RegisterAll`,
and schedules the C9 `reconcile_interval` ticker on the run context.
Shutdown stops the projector (unsubscribe + drain + join, counters
logged) after the sidecar child goes down, unbinds the reconciler, and
closes the store handle (`Store.Close`, drain-then-close per the
MaxOpenConns(1) invariant). `meept kanban` is mounted on
the CLI root (`cmd/meept/main.go`). Boot tests:
`internal/daemon/kanban_wiring_test.go` (disabled-constructs-nothing,
enabled store/rules/projection round-trip, rule bootstrap idempotence,
from_state closure property); the hermetic e2e case (g) in
`e2e/suites/kanban-board` covers the real daemon boot.

Pairing follow-ups, now closed (as-built): (1) `internal/kanban`
exports `Store.Close` — idempotent, nil-safe; `stopKanban` calls it
after the projector drain. (2) `POST /kanban/pair/exchange` is a real
mux route (`internal/comm/http/kanban_pairing.go`), armed only when the
kanban pairing store exists: loopback-only, per-IP rate-limited,
bearer-auth-exempt (the single-use console-printed code IS the
credential), one uniform 401 for wrong/consumed/expired/missing codes,
4KB body cap, code and token never logged — the dev-key pairing
precedent's posture exactly. Codes remain in-memory per process: a
daemon-minted code exchanges at the daemon (this route or the owner RPC),
a sidecar-minted code at the sidecar's own exchange route; the minted
bearer token lands in the shared tokens.json and authenticates at both.

## Event contract (master.md C1/C2)

State changes ride typed `internal/bus` topics with payload
`kanban.StateEvent{Kind, ID, From, To, Reason string; TS time.Time}`.
The event carries identity only. Provenance (`goal_id`, `employee_id`,
`plan_id`, `session_id`, `trigger_ref`) is NOT on the wire: the
projector reads the source work-item row keyed by `StateEvent.ID` at
projection time (`RowFetcher`, `projector.go:48-63`; reconcile's twin is
`ListFetcher`/`WorkItemRef.Row`, `reconcile.go:50-78`).

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
`Registry.SetKanbanEventPublishing(bool)` (plus the plan/step/goal
setters, `internal/plan/manager.go:1826`, `internal/employee/goal.go:720`).
Default is false: zero events are constructed or published and behavior
is byte-identical to pre-kanban builds. The daemon turns the gate on
only when the kanban feature is enabled; that wiring is leaf 07/01.
Unit tests drive both sides through the setters.

## Grants (C5, leaf 04/01)

Three string grants: `board.read`, `board.mutate`, `board.takeover`
(`internal/kanban/authz.go`). With `multiuser.enabled=false` the owner
holds all three. Consumers: the sidecar gateway
(`internal/kanbansvc`, see kanbansvc.md — takeover is conjunctive:
mutate AND takeover) and the `kanban.*` RPC handlers (committed leaf
05/01, live registration at 07/01).

## Config: enabling the feature

All knobs live in the `[kanban]` block of `~/.meept/meept.json5`
(`KanbanConfig`, `internal/config/schema.go:230-275`; C9 defaults at
`internal/config/schema.go:5250-5255`; commented template block in
`config/meept.json5` — an absent block is identical to disabled):

```json5
// drift: commented template block; absent from installed homes by
// design (default-off: an absent block is identical to disabled).
// "kanban": {
//   "enabled": false,            // master switch: store + projector + RPC
//   "sidecar_enabled": false,    // daemon-spawns meept-kanban (needs enabled
//                                // + transport.rpc.enabled; spawn doc below)
//   "sidecar_bind": "127.0.0.1:8790",  // C7: a non-loopback value is the
//                                // explicit opt-out (loud boot warning; TLS
//                                // terminates at YOUR proxy, see below)
//   "wip_mode": "warn",          // C9/C10: "warn" | "block" (any other
//                                // value fails config validation)
//   "reconcile_interval": "0s",  // C9: Go duration string; "0s" disables the
//                                // periodic rebuild (on-demand stays on)
//   "github_sync": { "enabled": false },   // deferred knob (issue #133)
// },
```

`enabled=false` (default): the daemon opens no kanban store, starts no
projector, registers no `kanban.*` RPC — byte-identical boot.
`wip_mode` is validated at load (`schema.go:269-275`).
`reconcile_interval` is held as a string and parsed at wiring time by
07/01 (`schema.go:252-257`).

## Projection rules (the bootstrap story)

Rules map `(kind, from_state) -> column` — the state a transition
LEAVES — and live in `kanban_projections` (C3), which is the mapping
AUTHORITY that both the projector and the reconciler read
(`projection_rules.go:12-31`, `reconcile.go:28-33`).

As built there is **no user-facing rule editor yet**. The store-level
API is `Store.SetProjectionRule` (upsert, `projection_rules.go:73`) and
`Store.LoadProjectionRules` (`:112`); both are currently deadcode stock
awaiting the 07/01 daemon wiring, which will seed default rules at boot
and hand the projector its rule snapshot. Until then the board is
manual-cards-only: an unmapped state creates no card, and cards park in
their last mapped column (`projector.go:127-131`). Do not invent a rules
RPC on the side — the C4 method set is frozen.

## Human commands (three surfaces + tools)

**CLI** — `meept kanban ...` (leaf 05/02, `cmd/meept/kanban.go:31-66`;
root registration lands with 07/01). Every verb hits one of the 13
frozen `kanban.*` RPC methods (C4):

| Command | Short |
|---|---|
| `meept kanban list` | list boards |
| `meept kanban show <board_id>` (`--card <id>`) | board columns+cards, or one card |
| `meept kanban board create <name>` / `list` / `columns <board_id>` / `cards <column_id>` | board/column/card reads + board create |
| `meept kanban create <board_id> <column_id> <title>` | create a card |
| `meept kanban move <card_id> <column_id>` | move a card (lease rule enforced) |
| `meept kanban takeover <card_id>` | cancel the episode, release the lease, move |
| `meept kanban wip <column_id> <limit>` | set the column wip limit (0 = no limit) |
| `meept kanban assign <card_id> <employee_id>` | assign an employee to a card |
| `meept kanban comment <card_id> <text...>` | append a card event |
| `meept kanban timeline <card_id>` | read the card event log |
| `meept kanban reconcile` | run a projection rebuild pass now |

All go through `connectDaemon()` like plans/projects; `--json` prints
the raw result.

**TUI** — the kanban board view (leaf 05/03, `internal/tui/kanban_view*.go`).
Open with the `view_kanban` keybinding, default `b`
(`internal/tui/config.go:123,186`), palette item "kanban board"
(`internal/tui/modal.go:212`). Keys
(`internal/tui/kanban_view_update.go:144-200`): `h`/`←` and `l`/`→` move
the selected card one column; `enter` opens card detail; `t` takeover
confirm; `a` assign prompt; `c` comment prompt; `r` reload board; `R`
reconcile now; `[`/`]` cycle boards.

**GUI** — Flutter command-palette item with keybinding `b` → route
`/kanban` (`ui/flutter_ui/lib/widgets/command_palette.dart:96-103`,
`core/router.dart:109-113`; TUI parity per the root convention). The
board view (`features/kanban/board_view.dart`) moves cards tap-to-move
(drag-and-drop deliberately not implemented, `board_view.dart:7`,
`card_tile.dart:3`), opens card detail sheets, and reaches the daemon
through the SIDECAR:
`KanbanService` dials the C7 loopback default port 8790
(`features/kanban/kanban_service.dart:134-135`) REST + `/api/v1/events`
WS relay.

**Agent tools** — `kanban_list/show/create/move/takeover/assign/comment`
(leaf 06/01, `internal/tools/builtin/kanban_tools.go`), registered via
the `toolprov` "kanban" provider (`kanban_register.go`). `kanban_claim`
is deliberately excluded — claim belongs to the machines, and the board
enforces lease rule A against claimed work instead.

## Error UX (C10)

The machine errors carry frozen text (`internal/kanban/types.go:58-82`)
and all three human surfaces parse exactly those strings off the error
path — never move the text to a success envelope:

- **Lease held**: `ErrLeaseHeld` = "lease held by <holder> since
  <RFC3339>". TUI renders holder + humanized age plus the takeover
  hint — "lease held by X for Y — press t to takeover"
  (`internal/tui/kanban_view.go:556-570`); pressing `t` replays the
  refused move as the takeover bypass
  (`internal/tui/kanban_view_update.go:221-228`). CLI: `kanbanError`
  extracts holder+age (`cmd/meept/kanban.go:477`). GUI: a snackbar
  "lease held by <holder> (<age>) — takeover overrides" with a
  takeover button (`ui/flutter_ui/lib/features/kanban/board_view.dart:213-218`),
  parsed by `_parseLeaseHeld` (`kanban_service.dart:610-617`).
- **WIP full** (`wip_mode="block"`): `ErrWIPFull` = "WIP limit reached
  for column <id>: <limit>" → TUI "wip full: column X limit N
  (wip_mode=block)", CLI names column+limit, GUI raises
  `KanbanWipFullException` (`kanban_errors.dart:72-74`). With
  `wip_mode="warn"` the move succeeds, appends a `wip_warning`
  timeline row, and notes the board feed (`internal/kanban/human.go:452-491`).
- **Missing grant**: 403 sidecar-side naming the grant; RPC lane
  returns "missing grant <g>" which the CLI parses
  (`internal/rpc/kanban.go:198-212`); GUI: "forbidden: takeover needs
  the board.takeover grant" (`board_view.dart:239`).
- **Unknown card**: `ErrUnknownCard` = "unknown card: <id>"; the RPC
  `kanban.card.get` answers exactly that text
  (`internal/rpc/kanban.go:427-428`); the sidecar maps an "unknown
  method/not found" daemon error to 404 (`gateway.go:370-376`).

## Reconcile (rebuild)

Three ways to force the authoritative rebuild, one code path
(`Reconciler.ReconcileNow`, `internal/kanban/reconcile.go:190-196`):

- TUI: `R` in the board view (`kanban_view_update.go:190-191`).
- CLI: `meept kanban reconcile` (`cmd/meept/kanban.go:377-378`).
- RPC: `kanban.reconcile.now` (no params; sidecar REST forwards it —
  see the route table in kanbansvc.md).

Reconcile is idempotent (a no-drift pass changes zero rows —
`ReconcileStats`), never deletes when a kind's enumeration errors
(fail-safe), parks unmapped items, and publishes nothing on the bus.
The periodic driver `RunPeriodicReconcile` honors
`[kanban].reconcile_interval` ("0s" default = disabled; ticker lands in
the 07/01 wiring via `RegisterDefaultReconciler`).

## Sidecar spawn supervision (leaf 04/04, committed)

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
5s HTTP drain, `cmd/meept-kanban/main.go:116`, then SIGKILL) BEFORE the
RPC socket closes. Disabled (default): no resolve, no process, no pid
file — byte-identical boot. A spawn failure is non-fatal: the board
gateway degrades, never the daemon.

## Exposing the sidecar: TLS-termination proxy

The sidecar serves **cleartext HTTP only** — no TLS in the binary, by
contract (C7; `cmd/meept-kanban/main.go:7`). Bearer tokens ride the
`Authorization` header and the `Sec-WebSocket-Protocol` bearer lane, so
any non-loopback exposure MUST terminate TLS in an operator proxy.
Bind the sidecar to loopback, have the proxy speak to `127.0.0.1:8790`,
and point DNS at the proxy.

Requirements the proxy MUST satisfy:

1. **Websocket upgrade passthrough for `/api/v1/events`** — HTTP/1.1
   with `Upgrade`/`Connection` forwarded.
2. **`Sec-WebSocket-Protocol` preserved in BOTH directions.** The
   browser's bearer lane is a subprotocol named `bearer.<token>`
   (`internal/kanbansvc/gateway.go:11`), and the gateway MUST get its
   offered subprotocol echoed back or the browser aborts the connection
   (`gateway.go:330-346`). A proxy that rewrites, drops, or renegotiates
   subprotocols kills the live board feed (REST keeps working, so the
   symptom is a silently stale board).
3. Keep `Authorization` header passthrough (the default everywhere, but
   check any header-scrubbing middleware).

Caddy (automatic HTTPS, correct WS handling out of the box —
`reverse_proxy` relays the upgrade and passes both
`Sec-WebSocket-Protocol` request and response headers verbatim):

```caddy
kanban.example.com {
    reverse_proxy 127.0.0.1:8790
}
```

nginx (TLS cert paths omitted; the explicit subprotocol lines document
the contract even where your nginx version relays it by default):

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name kanban.example.com;
    # ssl_certificate ...; ssl_certificate_key ...;

    location / {
        proxy_pass http://127.0.0.1:8790;
        proxy_http_version 1.1;                       # required for the upgrade
        proxy_set_header Upgrade $http_upgrade;       # websocket handshake
        proxy_set_header Connection $connection_upgrade;
        # the bearer lane rides the subprotocol — pass it through unchanged,
        # both in the request and (untouched) in the 101 response:
        proxy_set_header Sec-WebSocket-Protocol $http_sec_websocket_protocol;
        proxy_set_header Host $host;
        proxy_read_timeout 300s;                      # long-lived event feed
    }
}
```

**Origin policy:** the gateway's WS origin allow-list accepts
`localhost` / `127.0.0.1` / `::1` (or a missing Origin, non-browser
clients) BY DEFAULT. For the TLS-proxy deployment above — where the
proxy presents the PUBLIC browser Origin — set
`[kanban].allowed_origins: ["https://board.example.com"]`: exact
`scheme://host[:port]` entries (no wildcard, no path, no suffix
matching — origin trust is exact; validation in
`internal/config/schema.go`, normalization + match in
`internal/kanbansvc/gateway.go` `SetAllowedOrigins`/`originAllowed`).
The daemon passes each entry to the sidecar as a repeated `-allow-origin`
flag (`internal/daemon/kanban_boot.go` `buildKanbanArgs`); the sidecar
stays config-blind (C7). An empty/absent list keeps the loopback-only
posture byte-identical; a request carrying no Origin at all passes the
exact-match gate (non-browser clients never send one).

## Verification

- Coverage pins: `internal/kanban/events_test.go` walks both transition
  tables and asserts every legal edge has a publisher path;
  `internal/queue/kanban_events_test.go` and
  `internal/task/kanban_events_test.go` pin gate-on emission, gate-off
  silence, and the publish-nothing dispositions.
  `internal/kanban/reconcile_test.go` pins the fail-safe dispositions
  (`TestReconcile_EnumerationErrorNeverDeletes`), idempotence, and the
  projector-restart recovery (`TestReconcile_KillProjectorRestart`);
  `internal/kanban/human_test.go` pins lease fail-closed
  (`TestHumanMove_LeaseLookupErrorFailsClosed`) and takeover
  audit-before-effect (`TestHumanTakeover_AuditBeforeEffectOrdering`).
- `internal/rpc/kanban_test.go` pins the 13-method C4 surface and the
  frozen refusal texts; `cmd/meept/kanban_test.go` drives the CLI tree
  against a fake transport.
- e2e: suite `kanban-board` (`e2e/suites/kanban-board/`) covers the
  real sidecar binary; manifest maps `internal/kanban/`,
  `internal/kanbansvc/`, `cmd/meept-kanban/` to it.
