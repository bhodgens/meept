# 05/05-flutter-board-view

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 03-board-projection, 05/04-flutter-bridge

## Scope
Implement the Flutter Kanban board widget in `ui/flutter_ui/lib/features/kanban/board_view.dart`.
- **View Implementation:**
  - Render a horizontal column list (each column as a vertical list of cards).
  - Implement drag-and-drop (or tap-to-move) logic for card movement.
  - Display a "lease error" snackbar when a move is refused due to `ErrLeaseHeld` (C10).
  - Implement route registration for the board view.
- **Dart Implementation:**
  - Use `kanban_service.dart` for all data operations.
  - Handle `WSBoard` events from the service to update the UI reactively.

## Estimated Context (95K)
- `ui/flutter_ui/lib/features/kanban/board_view.dart`
- `ui/flutter_ui/lib/services/kanban_service.dart`
- `ui/flutter_ui/lib/models/kanban_models.dart`

## Interface Contract (exact signatures exposed to siblings)
- None (UI is a consumer)

## Tasks
- [ ] TDD: Write `test/kanban_board_view_test.dart` verifying board layout and card movement.
- [ ] Implementation: Implement the Flutter Kanban board widget and drag-and-drop logic.
- [ ] Implementation: Wire the widget to the `KanbanService` and WebSocket events.
- [ ] Verify: `flutter test`

## Self-Verification Checklist
- [ ] Board view handles card moves via `kanban_service.dart`.
- [ ] User sees a snackbar for `ErrLeaseHeld`.
- [ ] UI updates reactively on `WSBoard` events.

## Review Checklist
- [ ] All UI text is lowercase.
- [ ] Handles `ErrLeaseHeld` (C10) correctly.
