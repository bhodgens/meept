# Leaf 05 — Flutter GUI relay surface (Contract E)

Parent: `master.md` Contract E. Depends: leaf 03. Branch: gui.
PARALLEL with leaf 04 (disjoint file trees; both read leaf 03's payloads).

## Precedents to read first

- ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart — dialog
  shape.
- ui/flutter_ui/lib/providers/chat_provider.dart — WS agent_progress
  frame pump; where to hook the acp.permission.* payload keys.

## Tasks

1. New widget `AcpPermissionDialog` (lowercase UI text): agent id,
   summary, created-at, allow/deny buttons → POST
   /api/v1/acp/permissions/resolve via the existing api client layer.
2. Provider state: pending list keyed by id; dialog shows head; on
   `acp.permission.resolved` frame the entry clears (and dialog closes if
   head). Badge/count in the existing status area (parity: TUI status
   line; deviation recorded in docs/workflows if GUI badge deferred —
   prefer NOT deferring).
3. Web-safe: no top-level dart:io; dialog purely state-driven.
4. Tests: the flutter suite layout — grep existing widget tests for the
   dialog precedent; add a dialog render + resolve-tap test. `dart format`
   EVERY dart file in the commit's pathspec BEFORE git add (repo hook
   trap; orchestrator commits).

## Verify

`flutter analyze` clean + `flutter test` for touched areas (or the repo's
gui lane from ui/flutter_ui). Do NOT commit.
