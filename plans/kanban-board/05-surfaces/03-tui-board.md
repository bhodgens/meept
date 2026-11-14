# 05/03-tui-board

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 03-board-projection

## Scope
Implement a Kanban board view in the TUI via `internal/tui/kanban_view.go`.
- **View Implementation:**
  - Use `internal/tui/tableutil.Size` on BOTH axes to handle viewport resizing.
  - Columns are represented as table columns; cards are represented as rows.
  - Support standard TUI navigation (arrow keys/j/k).
- **Interactivity:**
  - Implement keybindings for card movement and takeover.
  - Support context switching/refreshing via existing app.go-editing patterns.
  - All TUI text must be lowercase.

## Estimated Context (90K)
- `internal/tui/app.go`
- `internal/tui/tableutil.go`
- `internal/tui/components/`
- `internal/rpc/client.go`

## Interface Contract (exact signatures exposed to siblings)
- None (TUI is a consumer)

## Tasks
- [ ] TDD: Write `internal/tui/kanban_view_test.go` verifying table sizing and navigation.
- [ ] Implementation: Implement `internal/tui/kanban_view.go` using `tableutil`.
- [ ] Verify: `go test -v ./internal/tui/...`

## Self-Verification Checklist
- [ ] Table renders correctly with width-0 viewport (no empty/broken lines).
- [ ] Navigation works for scrolling through many cards.
- [ ] Text is all lowercase.

## Review Checklist
- [ ] TUI view is serializable/reproducible with existing app.go-editing patterns.
- [ ] No hardcoded board/column IDs in the TUI component.
