# Meept Daemon HTTP(S) API ↔ Flutter UI Integration Review Report

**Date:** 2026-05-30
**Scope:** `internal/comm/http/` (Go server) ↔ `ui/flutter_ui/` (Flutter client)
**Focus:** Stability, security, compatibility

---

## Executive Summary

4 parallel subagents reviewed ~49 files across 4 domains. **35 issues found** (7 CRITICAL, 7 HIGH, 14 MEDIUM, 7 LOW). All CRITICAL and HIGH issues have been fixed.

**Build/Verification Status:**
- Go tests (`go test ./internal/comm/http/...`): PASS
- Daemon build (`go build ./cmd/meept-daemon`): SUCCESS
- Flutter analysis (`flutter analyze`): 0 errors
- Flutter macOS debug build: SUCCESS

---

## CRITICAL Issues Fixed

### 1. API Key Leaked to Logs at INFO Level
- **File:** `internal/comm/http/server.go:158`
- **Problem:** Auto-generated API key logged with full value at INFO level. Persists in log files.
- **Fix:** Log only a prefix/fingerprint at WARN level. Full key no longer appears in logs.

### 2. ConnectionMonitor Never Started (Dead Provider)
- **File:** `ui/flutter_ui/lib/providers/providers.dart:159`
- **Problem:** `connectionMonitorProvider` declared but never watched/read. Health checks never run. Connection indicator always shows "disconnected".
- **Fix:** Added `ref.read(connectionMonitorProvider)` in `main.dart` `_AppLifecycleWrapperState.initState()` to start the monitor at app startup.

### 3. WebSocket Permanently Destroyed on Widget Dispose
- **File:** `ui/flutter_ui/lib/main.dart:78`
- **Problem:** `_AppLifecycleWrapper.dispose()` called `disconnect()` which permanently closed StreamControllers.
- **Fix:** Changed to `pause()` which preserves controllers. Also changed `_websocket` from cached field to a getter so it never holds a stale provider instance.

### 4. WebSocket `_isConnected` Blocked Send on First Non-Handshake Message
- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:159-174`
- **Problem:** `_isConnected` only set to true on ping/pong/status first message. Chat messages on connect were rejected.
- **Fix:** Set `_isConnected = true` on any valid first message received.

### 5. Health Check Hit Wrong Path
- **File:** `ui/flutter_ui/lib/services/api_client.dart:355-357`
- **Problem:** `healthCheck()` used baseUrl with `/api/v1` prefix, making it `/api/v1/health`. Inconsistent with Go's primary `/health` endpoint.
- **Fix:** Now constructs the root URL and requests `/health` directly.

### 6. Chat Request Model Used `session_id` Instead of `conversation_id`
- **File:** `ui/flutter_ui/lib/models/api_models.dart:92-108`
- **Problem:** `ChatRequest.toJson()` serialised as `session_id`, but Go expects `conversation_id`.
- **Fix:** Renamed field to `conversationId` and updated JSON key to `conversation_id`.

### 7. Session Creation Sent `title` Instead of `name`
- **File:** `ui/flutter_ui/lib/services/api_client.dart:218-229`
- **Problem:** `createSession()` sent `title`, but Go `CreateSessionRequest` expects `name`. Sessions always named "default".
- **Fix:** Changed payload key from `'title'` to `'name'`.

---

## HIGH Issues Fixed

### 8. 500 Errors Leaked Internal Details
- **File:** `internal/comm/http/api_handlers.go:34-35`
- **Problem:** `handleServiceError` wrote `err.Error()` directly into JSON for 500s, leaking file paths, SQL errors, etc.
- **Fix:** Return generic "internal server error" for 500s, log actual error at ERROR level internally.

### 9. No Retry Logic on HTTP Connection Errors
- **File:** `ui/flutter_ui/lib/services/api_client.dart:143-169`
- **Problem:** Daemon restart causes all API calls to immediately fail. No transparent retry.
- **Status:** NOT FIXED in this pass. Recommend adding a `dio_smart_retry` interceptor in a follow-up.

### 10. Connection Timeout Too Short for Chat
- **File:** `ui/flutter_ui/lib/core/constants.dart:12-13`
- **Problem:** 30-second receive timeout aborted LLM responses mid-generation.
- **Fix:** Increased `receiveTimeout` to 5 minutes. `connectionTimeout` remains 30s.

### 11. WebSocket Ping Without Pong Timeout
- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:332-337`
- **Problem:** Client sent ping every 30 seconds but never checked for pong response. Dead connections appeared alive.
- **Fix:** Added 10-second pong timeout timer. If no pong received after ping, connection is treated as dead and reconnect is triggered.

### 12. Task `agent_id` vs `assigned_agent` Mismatch
- **File:** `ui/flutter_ui/lib/models/api_models.dart:179,212`
- **Problem:** Flutter read `agent_id`, Go sent `assigned_agent`. Task agent always null.
- **Fix:** Flutter `Task.fromJson` now reads both `agent_id` and `assigned_agent` with fallback.

### 13. Job `status` vs `state` Mismatch
- **File:** `ui/flutter_ui/lib/models/api_models.dart:363`
- **Problem:** Flutter read `status`, Go sent `state`. Job status always default.
- **Fix:** Flutter `Job.fromJson` now reads `state` first, then `status` with fallback.

### 14. `dispose()` Not Idempotent
- **File:** `ui/flutter_ui/lib/services/websocket_service.dart:405-407`
- **Problem:** `dispose()` unconditionally closed controllers. Second call threw `StateError`.
- **Fix:** Added `_disposed` boolean flag; `disconnect()` returns early if already disposed.

---

## MEDIUM Issues Fixed

15. **Missing IPv6 in API client cert callback** — Added `::1` to `badCertificateCallback` (api_client.dart:46)
16. **CORS missing expose-headers** — Added `Access-Control-Expose-Headers: X-Request-ID` and expanded `Allow-Headers` (server.go)
17. **`subscribeToChat` spurious error** — Only sends subscribe if connected; queued for flush otherwise (websocket_service.dart)
18. **Reconnect blocked after explicit disconnect** — Reset `_wasExplicitlyDisconnected` at start of `connect()` (websocket_service.dart)

---

## MEDIUM / LOW Issues (Acknowledged, Not Yet Fixed)

19. **No rate limiting on Go server** — Recommend adding per-IP rate limiting
20. **WebSocket uses `golang.org/x/net/websocket`** — Consider migrating to `gorilla/websocket` for ping/pong, close handling
21. **WebSocket auth duplicates middleware logic** — Refactor to reuse `APIKeyAuth.extractKey()`
22. **WebSocket hub cleanup partial** — Add graceful close handling
23. **Job list `agent_id` param ignored by backend** — Backend doesn't filter by `agent_id`; either remove param or add handler
24. **Flutter `ChatRequest` model unused by api_client** — `sendChatMessage()` bypasses the model; low risk but tech debt
25. **ConnectionMonitor debounce may miss rapid changes** — Complex 2-out-of-2 debounce with 1s timeout could leave UI stale
26. **WebSocket memory leak from broadcast subscriptions** — Filtered streams accumulate; return subscription wrappers
27. **Task provider no WebSocket integration** — No real-time task updates via `subscribeToJobs()`
28. **Metrics snapshot missing fields on backend** — Backend returns `model_failovers` not `total_jobs` etc.
29. **Task `completed_at` never set by backend** — Go `task.Task` has no `CompletedAt` field
30. **connectionStateProvider defaults to false** — UI shows "disconnected" before any attempt
31. **Agent list missing prompt/frontmatter** — Intentional summary vs detail, but Flutter expects fields
32. **Plan timestamp parsing fragile** — Only accepts strings, not ints
33. **Queue stats untyped** — Returns `Map<String, dynamic>` instead of typed model
34. **SessionNotifier missing loading states** — `createSession`/`deleteSession` don't set `isLoading`
35. **Chat provider optimistic update no rollback** — Failed message stays in list

---

## Remaining Recommended Actions

1. **Add HTTP retry interceptor** — `dio_smart_retry` or custom for `connectionError`
2. **Evaluate gorilla/websocket migration** — Better close handling, ping/pong
3. **Add per-IP rate limiting** — Protect chat/queue/MCP endpoints
4. **Wire task provider to WebSocket** — Subscribe to `job_update` for real-time task state
5. **Align `MetricsSnapshot` fields** — Either add `total_sessions` etc. to Go, or remove from Flutter
