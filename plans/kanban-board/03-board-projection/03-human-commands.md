# 03-board-projection/03-human-commands

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 03-board-projection
Dependencies: 01-foundation, 02-workflow-authority

## Scope
Implement human-facing command logic for interacting with Kanban cards, including movement, takeover, assignment, and commenting.

## Estimated Context
100K

## Interface Contract
Exposes:
- `MoveCard(cardID, fromCol, toCol, actor)` (subject to C10 Lease rule)
- `Takeover(cardID, actor)` (cancels episode, releases token, audits, then moves)
- `Assign(cardID, employeeID)`
- `Comment(cardID, text)`

## Tasks
- [ ] Implement `internal/kanban/human.go`: `MoveCard` logic enforcing C10 Lease rule (returns `ErrLeaseHeld` with holder + age).
- [ ] Implement `internal/kanban/human.go`: WIP check per C10: target column count vs `Column.WIP` (0 = unlimited) BEFORE the move; `wip_mode: "block"` refuses with `ErrWIPFull{ColumnID, Limit}`, `"warn"` moves + notes.
- [ ] Implement `internal/kanban/human.go`: `Takeover` logic (audit-before-effect; release claim token; cancel underlying episode). Inject a `LeaseAuthority` interface (claim lookup, token release, episode cancel) so this package does not import the task registry / plan store directly; the concrete implementation is wired by 07/01-daemon-wiring.
- [ ] Implement `internal/kanban/human.go`: `Assign` and `Comment` handlers. `Comment` writes to the `kanban_card_events` table (C3, migration in 01-foundation/01); `kanban.card.timeline` reads the same table.
- [ ] Write `internal/kanban/human_test.go`: Test lease refusal (C10), WIP block/warn, and takeover audit sequence.
- [ ] Verify: `go test -v -run TestHumanCommands ./internal/kanban/...`

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] All errors wrapped with `%w`.
- [ ] No I/O inside any mutex.
- [ ] `human_test.go` uses `$TMPDIR` for SQLite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] Every new Set*/setter has nil guard
- [ ] No I/O under mutex
- [ ] IDs from `pkg/id.Generate()` only
- [ ] `enabled=false` path compiles and is byte-identical
- [ ] `make graphs` re-run after topic/RPC/WS change
