# 05/02-cli

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 01-foundation (C6)

## Scope
Implement the `meept kanban` command-line interface in `cmd/meept/kanban.go`.
- **C6 Verbs Implementation:**
  - `meept kanban list` (list boards)
  - `meept kanban show <board_id>` (show board/column/card details)
  - `meept kanban board <command>` (create/list/etc boards)
  - `meept kanban create <board_id> <column_id> <title>` (create card)
  - `meept kanban move <card_id> <column_id>` (move card)
  - `meept kanban takeover <card_id>` (cancel episode and move card)
  - `meept kanban wip <column_id> <limit>` (set WIP limit)
  - `meept kanban reconcile` (trigger `kanban.reconcile.now`)
- **Output:** Use `tableutil` for all structured command outputs.

## Estimated Context (55K)
- `cmd/meept/kanban.go`
- `cmd/meept/main.go`
- `internal/rpc/client.go`

## Interface Contract (exact signatures exposed to siblings)
- None (CLI is a consumer)

## Tasks
- [ ] TDD: Write `cmd/meept/kanban_test.go` verifying all C6 verbs via a mock RPC client.
- [ ] Implementation: Implement `cmd/meept/kanban.go` using `spf13/cobra`.
- [ ] Verify: `go build ./cmd/meept` and `go test ./cmd/meept/...`

## Self-Verification Checklist
- [ ] All C6 verbs are implemented.
- [ ] Output is structured and uses `tableutil` where appropriate.
- [ ] Commands work with and without a session/dev key.

## Review Checklist
- [ ] Commands match C6 exactly.
- [ ] No hardcoded board/column IDs in code.
