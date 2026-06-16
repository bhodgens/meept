# S8 - Flutter UI Round 5 Review

Scope: all Dart files under `ui/flutter_ui/lib/` (excluding `.freezed.dart` / `.g.dart` auto-generated).

Bug classes checked: (1) WebSocket/Stream lifecycle, (2) state management after dispose, (3) null safety / `!` abuse, (4) API path alignment vs `internal/comm/http/server.go:setupRESTRoutes()`, (5) error swallowing `catch(_){}`, (6) resource leaks (TextEditingController / ScrollController / FocusNode without dispose), (7) auth/API key handling (verify round-4 removal of hardcoded dev key fallback), (8) type/factory mismatches vs Go struct tags, (9) dead/stale code (`.bak`, TODOs), (10) lowercase UI convention.

## Critical

### S8-1 Hardcoded dev API key fallback still present in `api_client.dart`

- File: `ui/flutter_ui/lib/services/api_client.dart:97-107`
- Round 4 reportedly removed the hardcoded dev key fallback, but the literal string `'meept_dev_default_key_CHANGE_ME'` is still in the code path:

```dart
    if (apiKey == null || apiKey.isEmpty) {
      if (AppConstants.defaultApiKey.isNotEmpty) {
        apiKey = AppConstants.defaultApiKey;
      } else {
        // Hardcoded fallback matching pkg/constants/api_key.go DefaultDevAPIKey
        apiKey = 'meept_dev_default_key_CHANGE_ME';
      }
    }
```

- Impact: a release build with no stored key silently authenticates against the daemon using a well-known value, defeating the `kReleaseMode` guard that `AppConstants.defaultApiKey` (via `String.fromEnvironment('MEEPT_DEV_API_KEY', defaultValue: '')`) was designed to enforce. Any user who ships the binary as-is leaks the dev key.
- Fix: delete the `else` branch and let `apiKey` remain null; let the daemon reject the unauthenticated request, or surface a clear error in the UI.

## High

### S8-2 `clearError()` drops `currentProgress` state

- File: `ui/flutter_ui/lib/providers/chat_provider.dart:397-404`
- `clearError()` rebuilds the `ChatState` with only `messages`, `isLoading`, `isAgentProcessing` — it forgets `currentProgress`. When the user dismisses an error banner mid-stream, the agent progress indicator disappears and the UI reverts to the static "thinking..." fallback until the next WS event arrives.
- Fix: use `state = state.copyWith(error: null)` (the `copyWith` already exists and preserves the other fields) or include `currentProgress: state.currentProgress` in the manual constructor call.

### S8-3 `ChatState.copyWith` cannot clear `currentProgress`

- File: `ui/flutter_ui/lib/providers/chat_provider.dart:41-61`
- `currentProgress: currentProgress ?? this.currentProgress` uses the "null means keep" pattern. Once set, the field can never be reset to null through `copyWith`, which forces every call site that wants to clear progress to do a full `ChatState(...)` reconstruction (and risk dropping fields, as S8-2 shows).
- Fix: mirror the `_unset` sentinel pattern already used for `error` (lines 15, 45, 58) so callers can explicitly pass `null`.

### S8-4 `subscribeToAgentProgress` collides with chat subscription map

- File: `ui/flutter_ui/lib/services/websocket_service.dart:541-557`
- `subscribeToAgentProgress(sessionId)` writes into `_chatSubscriptions[sessionId]` — the same map used by `subscribeToChat(sessionId)`. Calling `unsubscribeFromChat(sid)` removes the progress subscription (and vice-versa) and sends an `unsubscribe {channel: 'chat'}` for what may be a live progress subscription.
- Fix: introduce a separate `_progressSubscriptions` map keyed by sessionId, or key the combined map by `(channel, sessionId)` tuples.

### S8-5 Inline `FocusNode()` never disposed in four panels

- Files:
  - `ui/flutter_ui/lib/features/skills/skill_panel.dart:251`
  - `ui/flutter_ui/lib/features/search/search_panel.dart:111`
  - `ui/flutter_ui/lib/features/memory/memory_panel.dart:117`
  - `ui/flutter_ui/lib/features/projects/branches_panel.dart:163`
- Each `KeyboardListener(focusNode: FocusNode(), ...)` creates a FocusNode in `build()` that is never stored in a field nor disposed in `dispose()`. Because `build()` can run many times over the widget's lifetime, this leaks native focus handles and may cause focus-tree corruption on hot reload / long sessions.
- Fix: hoist the `FocusNode` to a `State` field, create it in `initState`, and `dispose()` it in `dispose()`.

## Medium

### S8-6 Silent `catch (_)` blocks hide failures across `providers.dart`

- File: `ui/flutter_ui/lib/providers/providers.dart` — lines 64, 230, 240, 252, 370, 381
- Six sites swallow the exception with no logging or rethrow:
  - `resolveActiveProjectProvider` (64) returns null on any error — a user with a broken project list silently sees "no active project" with no diagnostic.
  - `ConnectionDetailsNotifier._fetch` (230, 240, 252) hides daemon-fetch errors; the dialog shows stale host/port with no indication that the status call failed.
  - `ConnectionMonitor._fetchConnectionDetails` (370) explicitly documents "best-effort only" — acceptable, but should `debugPrint` the error.
  - `ConnectionMonitor._startHealthChecks` (381) silently marks disconnected — a genuine network error vs. a daemon crash are indistinguishable in logs.
- Fix: at minimum `debugPrint('[warn] <context>: $e')` in each handler so operators can triage. Several of these should also surface the error in the UI (e.g. `connectionDetailsProvider` state).

### S8-7 `_loadSkills` swallows errors with empty comment

- File: `ui/flutter_ui/lib/features/home/tools_dropdown.dart:35-37`
- `catch (e) { // skills remain empty on error }` — the dropdown silently shows zero skills on any failure (network, auth, parse). A user who misconfigures the daemon URL sees an empty tools menu with no indication why.
- Fix: surface the error via an error banner or a tooltip on the dropdown, or at least `debugPrint`.

### S8-8 `AgentProgress.fromJson` unsafe `as int` cast on `tier`

- File: `ui/flutter_ui/lib/models/api_models.dart:152`
- `tier: (data?['tier'] ?? json['tier'] ?? 1) as int` will throw if the backend ever serializes the value as a `num`/`double` (e.g. JSON encoders that emit `1.0` for integer fields). The Go side declares `VerbosityLevel` as an `int`, but any intermediary (proxy, cache, custom encoder) can widen the type.
- Fix: `(data?['tier'] ?? json['tier'] ?? 1) as num` then `.toInt()`, mirroring the defensive pattern already used for timestamps elsewhere in this file.

### S8-9 `SlashCommandRegistry.get` uses `catch (_)` to implement "not found"

- File: `ui/flutter_ui/lib/core/slash_commands.dart:40-45`
- Using `firstWhere` + `catch (_)` to return null hides any unrelated exceptions (e.g. if `all` ever throws) and is harder to read than `try/catch StateError`.
- Fix: use `all.where((cmd) => cmd.name == n).firstOrNull` (Dart 3) or an explicit `for` loop.

### S8-10 Dead `chat_provider.dart.bak` file

- File: `ui/flutter_ui/lib/providers/chat_provider.dart.bak`
- Leftover backup file from a prior round. It is tracked by glob patterns and will be packaged into builds; it also shows up in search results and IDE file trees, creating confusion.
- Fix: delete the file.

## Low

### S8-11 Stale TODO in search panel

- File: `ui/flutter_ui/lib/features/search/search_panel.dart:401`
- `// TODO: Navigate to the result based on type and id` — the result row's `onTap` only shows a snackbar ("navigating to …") instead of routing. Either implement the navigation (dispatch to `router.go` based on `result.type`) or remove the Tap handler to avoid implying the feature works.

### S8-12 Stale TODO in storage service

- File: `ui/flutter_ui/lib/services/storage_service.dart:93`
- `// TODO: remove SharedPreferences fallback in a future version` — long-standing migration note. Either commit to a removal version or delete the TODO.

### S8-13 `SearchScope.name` shadows `Enum.name`

- File: `ui/flutter_ui/lib/models/api_models.dart` (per grep around line 456-462)
- Defining a `name` getter/extension on `SearchScope` collides with the built-in `Enum.name` introduced in Dart 2.15. It compiles today but is a footgun for future maintainers and for `meept_api.dart:292` which calls `scope.name` — it's ambiguous whether the enum's identifier or the extension's wire-value is intended.
- Fix: rename the extension member to `wireName` or `apiValue`, or rely on `enum` values whose identifiers already match the wire format.

### S8-14 `KeyboardListener` is deprecated

- Files: the same four panels as S8-5.
- Flutter 3.7+ deprecates `KeyboardListener` in favor of `Focus( onKeyEvent: ... )`. The current call sites compile with warnings on recent Flutter versions and will break when the deprecation becomes a removal.
- Fix: replace `KeyboardListener(focusNode: ..., onKeyEvent: ...)` with `Focus(focusNode: ..., onKeyEvent: ...)` (and a child wrapper). Best done together with S8-5 so the FocusNode is hoisted once.

### S8-15 `_buildBaseUrl` excludes `/api/v1` but `healthCheck` manually re-prepends baseUrl

- File: `ui/flutter_ui/lib/services/meept_api.dart:29-32`
- `healthCheck` calls `_dio.get('${_dio.options.baseUrl}/health')` while every other method uses a relative path that Dio resolves against `baseUrl`. The manual prefix works but breaks the abstraction: if `baseUrl` is ever changed to already include a trailing path, `/health` becomes double-prefixed.
- Fix: use `_dio.get('/health')` like all other endpoints — Dio will resolve against `baseUrl`.

## Verified (no issue)

- **Round-4 WebSocket `_streamDone`/`_cleanupChannel` fix** — `websocket_service.dart:298-414` correctly creates a per-connection `Completer<void>` and `_cleanupChannel` explicitly completes it on every exit path. The `pause()` and pong-timeout flows will no longer wedge the reconnect loop.
- **`AppConstants.defaultApiKey`** — `core/constants.dart` correctly uses `String.fromEnvironment('MEEPT_DEV_API_KEY', defaultValue: '')`; empty in release builds. (Note: bypassed by S8-1.)
- **`WebSocketService.fromStorage`** — `websocket_service.dart:109-127` correctly throws `ArgumentError` in release mode when no key is configured, unlike `api_client.dart` which still falls back.
- **`DaemonCertPinner.validateCert`** — `daemon_cert_pinner.dart:91-114` rejects non-localhost hosts and pins to fingerprint when available; the sandbox fallback is documented.
- **`ChatNotifier.dispose`** — `chat_provider.dart:411-424` cancels timers and stream subs and calls `unsubscribeFromChat`. `_disposed` flag is checked before state writes.
- **`_ChatMessageListState.dispose`** — `chat_message_list.dart:46-50` properly removes listener and disposes `ScrollController`.
- **`_TerminalCursorController` / `AnimationController`** — `chat_input.dart` disposes both in `dispose()`.
- **`_CreateEventDialogState`** — `calendar_panel.dart:481-485` disposes both `TextEditingController`s.
- **Plans tab dialog controllers** — `plans_tab.dart:403, 429` dispose controllers via `whenComplete`.
- **API path alignment vs `server.go`** — all paths used in `meept_api.dart` (`/api/v1/chat`, `/api/v1/chat/steer`, `/api/v1/chat/followup`, `/api/v1/sessions[/{id}][/messages|/plans]`, `/api/v1/tasks[/{id}][/cancel]`, `/api/v1/queue/{jobs,stats}`, `/api/v1/memory/{query,recent}`, `/api/v1/skills[/{slug}][/execute|/ui]`, `/api/v1/search`, `/api/v1/projects[/{id}][/branches|/checkout]`, `/api/v1/plans[/{id}][/approve|reject|confirm|revise]`, `/api/v1/config/{client,models,menubar,agents}`, `/api/v1/terminal/{history,exec,clear}`, `/api/v1/calendar/{today,events}`, `/api/v1/daemon/status`, `/api/v1/metrics/live`) match the Go route table in `internal/comm/http/server.go:setupRESTRoutes()`. No path mismatches found.
- **Lowercase UI convention** — grep for `Text('[A-Z]')` returned no violations in user-visible labels; all button/menu/tooltip strings are lowercase per CLAUDE.md.

## Severity summary

- Critical: 1 (hardcoded dev API key fallback in api_client.dart still present despite round-4 claim)
- High: 4 (clearError drops currentProgress; copyWith can't clear progress; agent-progress subscription map collision; FocusNode leak in 4 panels)
- Medium: 5 (silent catch(_) blocks; skills-dropdown error swallowing; unsafe `as int` on tier; slash-command registry catch; dead .bak file)
- Low: 5 (stale TODOs; SearchScope.name shadow; deprecated KeyboardListener; healthCheck baseUrl double-prefix risk)
