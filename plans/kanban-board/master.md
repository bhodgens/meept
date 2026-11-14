# Master — Kanban Board Capability

## Goal

Add a user-facing kanban layer to meept without duplicating work-item state.
The board is a **rebuildable projection** over the existing work-item machines
(plan / task / step / goal / job / ticket). One canonical card per work item.
Humans interact through four surfaces (CLI, TUI, Flutter GUI, sidecar web
pane); agents interact through builtin tools and MCP. meept owns the store.
The only network-exposed process is the sidecar gateway binary, which holds
no logic and no database handle.

Ratified decisions (do not re-litigate in leaves):

1. **Lease rule (A):** a human card move onto a column mapped to an active
   state of a work item holding a live claim is REFUSED with `ErrLeaseHeld`
   (holder + age). A separate verb `takeover` (grant `board.takeover`) cancels
   the episode, releases the token, audits, then moves.
2. **Store model (A):** dedicated `internal/kanban` tables for the few facts
   the board owns (column, order, labels, notes, board/column defs) + a
   projector that derives state-dependent fields from bus events. The derived
   state is disposable: full rebuild from source tables at boot + on demand.
   Contract: after rebuild, board == source (pinned by e2e).
3. **Sidecar TLS (A):** the sidecar does NO TLS. Default bind loopback.
   LAN bind requires the explicit config knob. TLS terminates at an operator
   proxy (Caddy/nginx/Tailscale); plan ships a proxy example leaf.
4. **GitHub Projects (deferred):** ship outbound-only. Inbound sync is
   deferred to opt-in `[kanban] github_sync.enabled` (default false) tracked
   by GitHub issue bhodgens/meept#133. Do NOT plan inbound work here.

## Architecture Overview

Daemon side: `internal/kanban` owns boards/columns/cards (SQLite), a
projector that subscribes to work-item state-change bus events, and
`kanban.*` RPC handlers behind a permission gate. Every work-item state
machine must publish a typed event on every transition (event-coverage
requirement; task and queue publish nothing today). Sidecar: `cmd/meept-kanban`
— daemon-spawned, voice-child confinement precedent — serves the Flutter web
bundle and a narrow REST/WS API, and forwards all reads/mutations over the
Unix-socket RPC. The daemon keeps binding loopback/socket only.

## Interface Contracts

These are FROZEN. Leaves reference them by ID; a leaf that needs a change to
a contract must stop and report, not edit the contract.

### C1 Go types — `internal/kanban/types.go` (owner: 01-foundation/01)

```go
type Board struct { ID, Name string }
type Column struct { ID, BoardID, Name string; Position int; WIP int } // WIP 0 = no limit
type WorkRef struct { Kind, ID string } // Kind: step|goal|job|ticket|plan
type Card struct {
    ID, BoardID, ColumnID, Title string
    Ref WorkRef
    Labels []string
    GoalID, EmployeeID, PlanID, SessionID, TriggerRef string // provenance, may be ""
    Order int // manual ordering within column
}
type StateEvent struct { Kind, ID, From, To, Reason string; TS time.Time } // JSON keys: kind,id,from,to,reason,ts
type CardMoved struct { CardID, FromColumnID, ToColumnID, Actor string; LeaseBlocked bool }
```

Errors: `ErrLeaseHeld{Holder string, Since time.Time}`, `ErrUnknownCard`,
`ErrWIPFull{ColumnID string, Limit int}`.

### C2 Bus topics (typed, `internal/bus` Topic[T])

| Topic | Payload | Publisher |
|---|---|---|
| `kanban.event.step.state`, `.goal.state`, `.job.state`, `.task.state`, `.plan.state` | StateEvent | per-machine event publishers (branch 02) |
| `kanban.card.moved` | CardMoved | kanban RPC (branch 03) |
| `kanban.board.changed` | `{board_id}` | projector + CRUD |
| `kanban.human.command` | `{verb, card_id, column_id, actor, token}` | sidecar (branch 04) → consumed by daemon |

### C3 Table names (migration-owned by 01-foundation/01)

`kanban_boards`, `kanban_columns`, `kanban_cards`, `kanban_projections`
(board↔column mapping rules: kind + from_state → column_id).

### C4 RPC method names (exact strings)

`kanban.board.list`, `kanban.board.create`, `kanban.column.list`,
`kanban.column.set_wip`, `kanban.card.list`, `kanban.card.get`,
`kanban.card.create`, `kanban.card.move`, `kanban.card.takeover`,
`kanban.card.assign`, `kanban.card.comment`, `kanban.card.timeline`,
`kanban.reconcile.now`.

Read methods require grant `board.read`; mutations `board.mutate`;
takeover additionally `board.takeover`.

### C5 Grants — `internal/kanban/authz.go` (owner: 04-sidecar/01)

Three string grants: `board.read`, `board.mutate`, `board.takeover`.
With `multiuser.enabled=false` the owner (Unix socket / dev_key) holds all
three byte-identically to legacy behavior. With multiuser on, grants come
from the user store. The sidecar authenticates browsers with a
board-scoped bearer token mapped to a grant set (pairing flow, C7).

### C6 CLI — `cmd/meept/kanban.go`

`meept kanban list|show|board|create|move|takeover|wip|reconcile`.

### C7 Sidecar binary — `cmd/meept-kanban/`

Default bind `127.0.0.1:8790`. Non-loopback bind requires
`[kanban] sidecar_bind` set to the non-loopback address AND logs a loud
boot warning (mirrors the voice non-loopback invariant with flipped
polarity: knob required, never accidental). No TLS in the binary. Static
Flutter web bundle from `$MEEPT_HOME/webui` (`WEBDIR`, Makefile:127).
Every read/mutation goes to the daemon Unix socket RPC; the binary opens
NO SQLite handle. Browser auth: pairing-style single-use code printed to
the daemon console, exchanged once at `POST /kanban/pair/exchange` for a
stored bearer token carrying a grant set (pattern: internal/comm/http/pairing.go).

### C8 WS event class

New `WSClass` `WSBoard` in `internal/comm/wsclass/wsclass.go` → wire type
`board_event`, relayed like progress (never `chat_message`). Registration
must update: wsclass.go, wsclass_test.go, fuzz_test.go valid-type sets,
GUI Dart event model, and `make graphs` output (ws-event-map). Owner:
05-surfaces/06. Until it lands, board deltas ride the progress class.

### C9 Config — `[kanban]` block (all default OFF, opt-in-defaults.md rule)

```json5
enabled: false, sidecar_enabled: false, sidecar_bind: "127.0.0.1:8790",
wip_mode: "warn", // warn | block
github_sync: { enabled: false } // issue #133; code must NOT read this beyond the knob
```

### C10 Lease rule (decision 1)

`card.move` into a column whose mapping targets an active state of a card
whose work ref holds a live claim → `ErrLeaseHeld`. The UI must display
holder + age from the error. `card.takeover` is the only bypass; it emits
an audit record before acting (audit-before-effect precedent, voice dial).

## Child Index

| Child | Type | Deps | Est ctx | Files |
|---|---|---|---|---|
| 01-foundation | branch | — | — | store, config |
| 02-workflow-authority | branch | — | — | guards, events, wip, leases |
| 03-board-projection | branch | 01, 02 | — | projector, rebuild, commands |
| 04-sidecar-gateway | branch | 01 (C5 self-owned) | — | grants, sidecar, security, spawn |
| 05-surfaces | branch | 03 | — | rpc, cli, tui, flutter, parity, ws |
| 06-wiring-delivery | branch | all | — | mcp, e2e, docs, intake-bridge |

Concurrency plan: Wave A = 01 and 02 (fully parallel, disjoint packages).
Wave B = 03 and 04 (04 depends on 01 only). Wave C = 05 (needs 03).
Wave D = 06 (needs all). Serialize leaves that edit `internal/daemon/components.go`
or `internal/tui/app.go` — one per wave.

## Dispatch Protocol

For each child in dependency order:

1. Read the child's `orchestrator.md`; let the branch dispatch its own leaves.
2. `delegate_task` per leaf with: leaf path, pinned contracts C1-C10 verbatim,
   coding conventions block, exact verify commands, and the line
   "Do NOT commit. Do NOT run git add."
3. When an async completion arrives: run `git status --short` and
   `git diff --stat HEAD` before reviewing — catch unreported test/support files.
4. Review in-session (main model): read changed files, verify contracts,
   run `go build ./...` + leaf's tests + `go vet ./<pkg>/...`.
5. Pass → stage the leaf's exact files, commit `feat(kanban): <leaf>`.
   Fail → re-dispatch with gap list (max 3 iterations).
6. Update the Completion Tracking Table below and the branch's own table.

## Review Checklist

- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim; no silent drift
- [ ] Every new Set*/setter has nil guard; no I/O under mutex (mutexio)
- [ ] IDs from pkg/id.Generate() only
- [ ] `enabled=false` path compiles and is byte-identical (opt-in rule)
- [ ] make graphs re-run after any topic/RPC/HTTP/WS change; graphs-check green
- [ ] gui_inventory.json regenerated with feature if TUI view added
- [ ] No debug prints, TODOs, placeholder values in diff

## Coding Conventions

- Go stdlib + existing deps only; no new module deps without approval.
- gofmt; errors wrapped with %w; typed sentinel errors via errors.Is.
- Table-driven tests, t.Cleanup for temp SQLite under $TMPDIR.
- Match neighbor file style; comments explain WHY, cite invariant docs.
- Dart: lowercase UI text; kIsWeb guards; no top-level dart:io in shared code.

## Completion Tracking Table

| Child | Status | Notes |
|---|---|---|
| 01-foundation | PENDING | |
| 02-workflow-authority | PENDING | |
| 03-board-projection | PENDING | |
| 04-sidecar-gateway | PENDING | |
| 05-surfaces | PENDING | |
| 06-wiring-delivery | PENDING | |

## Integration Test Plan

After all branches reach COMPLETE, in-session:

1. `go test -p 2 -short ./...` full sweep, then `-race ./internal/kanban/...`.
2. e2e suite `e2e/suites/kanban`: (a) rebuild equality — kill projector,
   restart, board == source; (b) lease refusal returns ErrLeaseHeld with
   holder+age; (c) takeover moves + audits; (d) WIP counter accuracy;
   (e) sidecar request without token → 401; LAN bind without knob → boot fail;
   (f) parity snapshot (gui_inventory includes board view).
3. `make graphs-check`, `make lint-ci`, TUI parity test.
4. Manual smoke: `meept kanban board`, sidecar curl against loopback,
   Caddy example in docs.
