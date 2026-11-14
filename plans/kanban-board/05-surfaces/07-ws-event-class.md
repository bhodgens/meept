# 05/07-ws-event-class

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 03-board-projection, 05/05-flutter-board-view

## Scope
Implement the `WSBoard` event class and its wiring into the system (C8).
- **WS Class Implementation:** 
  - Implement `WSBoard` in `internal/comm/wsclass/wsclass.go`.
  - Update valid-type sets in `wsclass_test.go` and `fuzz_test.go`.
- **Dart Implementation:** Update the Dart event model in `ui/flutter_ui/lib/models/` to support `WSBoard`.
- **Graphing:** Run `make graphs` to update `docs/generated/ws-event-map.json` and ensure it matches the new `WSBoard` structure.
- **Serialisation:** Ensure `WSBoard` events are properly serialized and relayed to Flutter clients.

## Estimated Context (60K)
- `internal/comm/wsclass/wsclass.go`
- `internal/comm/wsclass/wsclass_test.go`
- `internal/comm/wsclass/fuzz_test.go`
- `ui/flutter_ui/lib/models/kanban_models.dart`

## Interface Contract (exact signatures exposed to siblings)
- None (Relay infrastructure)

## Tasks
- [ ] TDD: Write `internal/comm/wsclass/wsclass_test.go` verifying `WSBoard` registration and serialization.
- [ ] Implementation: Implement `WSBoard` in `wsclass.go`.
- [ ] Implementation: Update Dart models and run `make graphs`.
- [ ] Verify: `go test ./internal/comm/wsclass/...` and `make graphs-check`

## Self-Verification Checklist
- [ ] `WSBoard` is correctly registered in `wsclass.go`.
- [ ] `make graphs-check` is green after `make graphs`.
- [ ] Dart event model supports `WSBoard`.

## Review Checklist
- [ ] `WSBoard` follows the `board_event` relay pattern (no `chat_message`).
- [ ] `ws-event-map.json` is updated and valid.
