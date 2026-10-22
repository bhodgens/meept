## Flutter TUI/GUI Slash-Command Parity Fix

**File changed:** `ui/flutter_ui/lib/core/slash_commands.dart`

### What was done

Added two missing slash commands to the Flutter GUI's command registry to bring it into parity with the TUI:

1. **`/dnd`** -- toggle do-not-disturb (suppresses toasts)
   - TUI implementation: `internal/tui/command_handler.go:218`, backed by `notifications.set_dnd` RPC (`:830`)
   - Flutter GUI: Added as a SlashCommand entry with description and usage
   - **Deviation:** The Dart client has no generic RPC-call helper for making RPC calls like `notifications.set_dnd`. The WebSocket service is event-stream only (pub/sub for chat messages, progress, jobs, metrics, plans). A documented deviation note is added to the code.

2. **`/implement-improvements`** -- review and apply or skip pending improvement proposals
   - TUI implementation: `internal/tui/command_handler.go:222`, backed by daemon RPCs for the improvements queue
   - Flutter GUI: Added as a SlashCommand entry with description and usage
   - **Deviation:** The Dart client has no generic RPC-call helper for this endpoint. A documented deviation note is added to the code.

### Verification

- `cd ui/flutter_ui && dart analyze lib/core/slash_commands.dart` -> **No issues found**

### Gates run

- `dart analyze lib/core/slash_commands.dart` -> clean (exit 0)

### Observations

- None.
