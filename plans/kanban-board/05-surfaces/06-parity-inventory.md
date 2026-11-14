# 05/06-parity-inventory

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 03-board-projection, 05/03-tui-board

## Scope
Implement the parity validation for the Kanban board view to ensure TUI and Flutter GUI consistency.
- **Inventory Regeneration:** Regenerate `internal/tui/testdata/gui_inventory.json` to include all new Kanban board features (columns, cards, status, etc.).
- **Parity Verification:** Implement/update tests in `internal/tui/parity_test.go` to verify that the Kanban board view's elements are present in the GUI inventory.
- **Audit:** Perform a lowercase-text audit on all new Kanban-related UI text in both TUI and Flutter code.

## Estimated Context (45K)
- `internal/tui/testdata/gui_inventory.json`
- `internal/tui/parity_test.go`
- `internal/tui/kanban_view.go`

## Interface Contract (exact signatures exposed to siblings)
- None (Testing/Auditing task)

## Tasks
- [ ] TDD: Write `internal/tui/parity_test.go` verifying the board view's presence in `gui_inventory.json`.
- [ ] Implementation: Regenerate `internal/tui/testdata/gui_inventory.json` with Kanban features.
- [ ] Implementation: Run lowercase audit on Kanban UI strings.
- [ ] Verify: `go test ./internal/tui/...` and `make graphs-check`

## Self-Verification Checklist
- [ ] `gui_inventory.json` contains all Kanban board components.
- [ ] Parity tests pass for both TUI and Flutter views.
- [ ] All Kanban UI text is lowercase.

## Review Checklist
- [ ] Inventory is properly updated.
- [ ] No uppercase UI text found in Kanban features.
