# 05/04-flutter-bridge

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 01-foundation (C1), 05/05-flutter-board-view

## Scope
Implement the Dart service layer in `ui/flutter_ui/lib/services/kanban_service.dart` and its associated models.
- **Service Layer:**
  - Implement an RPC/WS client mirroring the Kanban tasks feature.
  - Implement methods for all C4 Kanban RPC calls (list boards, create cards, move cards, etc.).
  - Implement a WebSocket stream for `WSBoard` event updates.
- **Data Models:**
  - Define Dart models corresponding to `internal/kanban/types.go` (C1).
  - Use exact JSON keys from C1 verbatim in the models.
- **Web Compatibility:**
  - Use `kIsWeb` guards for any platform-specific logic.
  - Avoid `dart:io` in shared code.

## Estimated Context (70K)
- `ui/flutter_ui/lib/services/kanban_service.dart`
- `ui/flutter_ui/lib/models/kanban_models.dart`
- `internal/kanban/types.go` (C1)

## Interface Contract (exact signatures exposed to siblings)
- `Future<List<KanbanBoard>> listBoards();`
- `Future<void> moveCard(String cardId, String columnId);`
- `Stream<KanbanBoardEvent> boardEvents();`

## Tasks
- [ ] TDD: Write `test/kanban_service_test.dart` mocking RPC/WS responses.
- [ ] Implementation: Implement `kanban_service.dart` with RPC/WS client logic.
- [ ] Implementation: Implement Dart models mirroring C1 JSON keys.
- [ ] Verify: `flutter test`

## Self-Verification Checklist
- [ ] Models use exact JSON keys from C1.
- [ ] `kIsWeb` guards are present in shared services.
- [ ] All C4 methods are covered in the service.

## Review Checklist
- [ ] No `dart:io` imports in shared Dart code.
- [ ] Error handling correctly maps RPC/WS errors to Dart exceptions.
