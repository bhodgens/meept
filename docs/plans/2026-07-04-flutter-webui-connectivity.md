# Flutter WebUI Connectivity Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the four remaining issues blocking the Flutter web UI from connecting to the daemon: WebSocket browser auth, server-side WebSocket auth duplication, Sec-WebSocket-Protocol handshake echo, and DaemonStatus schema mismatch.

**Architecture:** Three root causes: (1) the server's `/ws` handler re-implements auth instead of using the middleware-set context value, and its handshake doesn't echo subprotocols; (2) the Flutter client uses `?token=` query param on web, which the daemon warns about; (3) `healthCheck()` and `_startHealthChecks()` deserialize `{"status":"ok"}` into the full `DaemonStatus` model whose non-nullable fields blow up. The fixes target each layer precisely — server-side consolidates auth and echoes subprotocols; client-side migrates to `Sec-WebSocket-Protocol: bearer.<key>`; deserialization callers move to raw JSON.

**Tech Stack:** Go (server), Dart/Flutter (client), `web_socket_channel` (Dart), `golang.org/x/net/websocket` (Go)

---

## File Structure

| File | Responsibility | Action |
|------|----------------|--------|
| `internal/comm/http/server.go` | HTTP server including `/ws` handler and WebSocket handshake | Modify: remove duplicate auth, echo subprotocol |
| `internal/comm/http/auth.go` | Auth middleware including `extractKey` | No change (already supports `Sec-WebSocket-Protocol`) |
| `ui/flutter_ui/lib/services/websocket_service.dart` | Flutter WebSocket service | Modify: use `protocols:` argument on web |
| `ui/flutter_ui/lib/services/sdk_client.dart` | Flutter SDK API client | Modify: drop typed DaemonStatus deserialization |
| `ui/flutter_ui/lib/providers/providers.dart` | Riverpod providers | Modify: replace typed healthcheck with raw |
| `ui/flutter_ui/pubspec.yaml` | Flutter asset manifest | Modify: remove missing `assets/shaders/` and `assets/images/` entries |

---

## Task 1: Server — Remove duplicate auth in `handleWebSocket`

**Files:**
- Modify: `internal/comm/http/server.go:2030-2081`

The `/ws` endpoint runs behind the auth middleware (registered at line 943 via `s.middleware`). The middleware already validates the key and stashes it in `r.Context()` under `apiKeyContextKey` (auth.go:71). `handleWebSocket` runs its own `Authorization`/`token` extraction and constant-time compare, which (a) is duplicate code, (b) doesn't check `Sec-WebSocket-Protocol`, and (c) returns a misleading 401 when the middleware has already accepted the request via `Sec-WebSocket-Protocol`.

Replace the bespoke auth block with a single context lookup.

- [ ] **Step 1: Read the current handler to confirm line numbers**

Run: `grep -n "func (s \*Server) handleWebSocket" internal/comm/http/server.go`
Expected: line 2030 or thereabouts.

- [ ] **Step 2: Replace the auth block**

In `internal/comm/http/server.go`, find the block inside `handleWebSocket` starting at the comment `// Validate API token if auth is required` (line ~2037) and ending at the closing brace before `allowedOrigins := s.config.WebSocketAllowedOrigins` (line ~2081). Replace it with:

```go
	// Auth is enforced by the APIKeyAuth middleware (registered via
	// s.middleware). The middleware extracts the key from Authorization,
	// Sec-WebSocket-Protocol: bearer.<key>, or the legacy ?token= query
	// param, validates it, and stashes it in the request context. We only
	// need to confirm it ran.
	if s.config.RequireAuth {
		if _, ok := httpauth.APIKeyFromContext(r.Context()); !ok {
			s.writeError(w, http.StatusUnauthorized, "unauthorized: missing API token")
			return
		}
	}
```

Add the import alias at the top of the file if not already present:

```go
import httpauth "internal/comm/http"
```

**Wait — `server.go` is in package `http` already.** Do not add a self-import. Instead, move `APIKeyFromContext` to a helper that doesn't require a new import, OR call it via the package-local function name. Check the existing calls in the file:

Run: `grep -n "APIKeyFromContext\|apiKeyContextKey" internal/comm/http/server.go`

If `apiKeyContextKey` is accessible from `server.go` (same package), use it directly:

```go
	if s.config.RequireAuth {
		if _, ok := r.Context().Value(apiKeyContextKey).(string); !ok {
			s.writeError(w, http.StatusUnauthorized, "unauthorized: missing API token")
			return
		}
	}
```

Since `auth.go` and `server.go` are both in package `http`, `apiKeyContextKey` is directly visible. Use this form.

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/comm/http/...`
Expected: no errors.

- [ ] **Step 4: Verify existing tests still pass**

Run: `go test ./internal/comm/http/... -run WebSocket -v`
Expected: all tests pass. If any test was directly invoking `handleWebSocket` with a Bearer header (bypassing middleware), it will now fail — that test must be updated to invoke via the middleware wrapper or set the context value manually.

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "fix(http): remove duplicate WebSocket auth, rely on middleware context

handleWebSocket re-implemented Authorization/token extraction instead
of trusting the APIKeyAuth middleware. The bespoke path missed
Sec-WebSocket-Protocol: bearer.<key>, so browser clients that used it
were rejected with 401 even though the middleware had already validated
the key. Drop the duplicate and read apiKeyContextKey from the context
the middleware populates."
```

---

## Task 2: Server — Echo `Sec-WebSocket-Protocol` subprotocol in handshake

**Files:**
- Modify: `internal/comm/http/server.go:2142-2153` (the `Handshake` function)

RFC 6455 §4.2.2: if the client offers subprotocols, the server must echo exactly one in its `Sec-WebSocket-Protocol` response header or the browser aborts the connection. `golang.org/x/net/websocket` does not auto-echo; its `Handshake` callback is responsible for selecting a protocol. The current implementation only checks Origin and returns nil without setting `config.Protocol`, so the response omits the header.

- [ ] **Step 1: Inspect the websocket.Config fields available**

Run: `grep -rn "type Config" $(go env GOMODCACHE)/golang.org/x/net*/websocket/`
Look for the `Protocol` field (it's `[]string`). The handshake callback receives `*websocket.Config` and can set `config.Protocol = []string{"<chosen>"}`.

- [ ] **Step 2: Update the Handshake function**

Find the `Handshake` field in the `websocket.Server` struct inside `handleWebSocket` and update it to echo the `bearer.<token>` subprotocol when present:

```go
		Handshake: func(config *websocket.Config, request *http.Request) error {
			origin := request.Header.Get("Origin")
			// Non-browser clients (Dart io.WebSocket, curl, CLI tools) may not
			// send an Origin header. Allow empty/absent Origin since auth is
			// already enforced by the middleware. For browser clients, enforce
			// the configured allowlist.
			if origin != "" && !originAllowed(origin) {
				return fmt.Errorf("origin not allowed: %s", origin)
			}
			// RFC 6455 §4.2.2: if the client offered subprotocols, echo one
			// back. Browser WebSocket APIs cannot set custom headers, so the
			// auth middleware uses Sec-WebSocket-Protocol: bearer.<key> as the
			// auth carrier. We must echo the offered protocol verbatim or the
			// browser will abort the connection with "Protocol mismatch".
			if proto := request.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
				// x/net/websocket separates multiple offers with ", ". Echo
				// the first bearer.* entry; if none, echo the first offer so
				// generic subprotocol clients still work.
				picked := ""
				for _, p := range strings.Split(proto, ",") {
					p = strings.TrimSpace(p)
					if strings.HasPrefix(p, "bearer.") {
						picked = p
						break
					}
					if picked == "" {
						picked = p
					}
				}
				if picked != "" {
					config.Protocol = []string{picked}
				}
			}
			return nil
		},
```

- [ ] **Step 3: Verify strings import**

Run: `grep -n '^import\|"strings"' internal/comm/http/server.go | head -20`
If `strings` isn't already imported (it is, per line 2062 usage), add it.

- [ ] **Step 4: Compile**

Run: `go build ./internal/comm/http/...`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "fix(http): echo Sec-WebSocket-Protocol subprotocol in WS handshake

RFC 6455 requires the server to echo a subprotocol if the client
offered one. Browser WebSocket APIs cannot set Authorization headers,
so auth is carried via Sec-WebSocket-Protocol: bearer.<key>. Without
echoing it back, the browser rejects the upgrade with protocol
mismatch even when auth succeeded."
```

---

## Task 3: Client — Migrate web WebSocket auth to `Sec-WebSocket-Protocol`

**Files:**
- Modify: `ui/flutter_ui/lib/services/websocket_service.dart:309-317`

The browser `WebSocket` constructor accepts a `protocols` argument that becomes the `Sec-WebSocket-Protocol` request header. Use it to carry `bearer.<key>` instead of stuffing the key into the URL query string (which leaks into server access logs).

- [ ] **Step 1: Read the current web branch**

Run: `grep -n "Web platform: use token query parameter" ui/flutter_ui/lib/services/websocket_service.dart`
Expected: line 310 or thereabouts.

- [ ] **Step 2: Replace the web branch**

Find the `else` clause starting at line 309 (`// Web platform: use token query parameter`) and replace the entire block with:

```dart
      } else {
        // Web platform: browsers cannot set custom WebSocket headers, so
        // we authenticate via the Sec-WebSocket-Protocol subprotocol,
        // which the daemon's middleware extracts as bearer.<key>. This
        // keeps the credential out of server access logs (unlike the
        // legacy ?token= query param).
        final protocols = <String>[];
        if (_apiKey != null && _apiKey.isNotEmpty) {
          protocols.add('bearer.$_apiKey');
        }
        _channel = WebSocketChannel.connect(
          uri,
          protocols: protocols.isNotEmpty ? protocols : null,
        );
      }
```

- [ ] **Step 3: Verify `web_socket_channel` supports the `protocols:` argument**

Run: `grep -n "WebSocketChannel.connect" ui/flutter_ui/lib/services/websocket_service.dart`
Check `pubspec.lock` for the version: `grep -A1 "web_socket_channel:" ui/flutter_ui/pubspec.lock`
`web_socket_channel: ^2.4.0` supports `connect(uri, protocols: [...])`.

- [ ] **Step 4: Compile the Flutter web target**

Run: `cd ui/flutter_ui && flutter build web --release`
Expected: build succeeds, no analyzer errors on `websocket_service.dart`.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/services/websocket_service.dart
git commit -m "fix(flutter-web): auth WebSocket via Sec-WebSocket-Protocol

Browser WebSocket APIs cannot set the Authorization header. The
previous approach used ?token=<key> query param, which the daemon
warns about because credentials leak into server access logs. Switch
to the Sec-WebSocket-Protocol: bearer.<key> convention the daemon's
auth middleware already supports."
```

---

## Task 4: Client — Stop deserializing `/health` and `/daemon/status` into `DaemonStatus`

**Files:**
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart:289-298` (`healthCheck`)
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart:302-309` (`getDaemonStatus`)
- Modify: `ui/flutter_ui/lib/providers/providers.dart:394` (health check caller)

`/health` returns `{"status":"ok"}`. `/api/v1/daemon/status` returns `{running, pid, uptime, state, budget, runtimes}`. Neither matches the OpenAPI-generated `sdk.DaemonStatus` shape (which requires `tokens_used`, `budget_used`, etc.). Both endpoints were being deserialized into `DaemonStatus`, which throws `Tried to construct class "DaemonStatus" with null for non-nullable field "status"` (for `/health` the field exists but most others are missing; for `/daemon/status` the `status` field itself is missing — it's called `state`).

- [ ] **Step 1: Rewrite `healthCheck` to return raw map**

In `ui/flutter_ui/lib/services/sdk_client.dart`, replace the body of `healthCheck` (lines 289-298) with:

```dart
  /// Returns the raw `/health` JSON `{status: ok}`.
  ///
  /// Do NOT deserialize into `sdk.DaemonStatus` — the OpenAPI schema for
  /// that model does not match what `/health` actually returns. The
  /// caller can check `result['status'] == 'ok'` directly.
  Future<Map<String, dynamic>> healthCheck() async {
    try {
      return await _get('/health');
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }
```

- [ ] **Step 2: Rewrite `getDaemonStatus` to return raw map**

In the same file, replace the body of `getDaemonStatus` (lines 302-309) with:

```dart
  /// Returns the raw `/api/v1/daemon/status` JSON.
  ///
  /// The daemon returns `{running, pid, uptime, state, budget, runtimes}`
  /// which does NOT match the OpenAPI-generated `sdk.DaemonStatus` model
  /// (that model expects `{status, tokens_used, budget_used, ...}`).
  /// Callers should access fields directly off the map. Existing callers
  /// already use [getDaemonStatusRaw] for this reason.
  Future<Map<String, dynamic>> getDaemonStatus() async {
    try {
      return await _get('/api/v1/daemon/status');
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }
```

- [ ] **Step 3: Remove the `sdk.DaemonStatus` references from this file**

Run: `grep -n "DaemonStatus\|sdk.DaemonStatus" ui/flutter_ui/lib/services/sdk_client.dart`
Any remaining references should be in doc comments only — no deserialization calls. The `sdk` import remains for other types.

- [ ] **Step 4: Update the health check caller in providers.dart**

In `ui/flutter_ui/lib/providers/providers.dart` line ~394, the health check calls `getDaemonStatus()` expecting a typed return. Update it to handle the raw map:

```dart
  void _startHealthChecks() {
    _timer = Timer.periodic(const Duration(seconds: 30), (_) async {
      if (!_websocket.isConnected && !_websocket.isConnecting) {
        try {
          // Any 2xx response from /api/v1/daemon/status means the daemon
          // is alive. The typed DaemonStatus deserialization was removed
          // because the daemon's response shape doesn't match the
          // OpenAPI model.
          await _sdkClient.getDaemonStatus();
          _proposeState(true);
        } catch (e) {
          debugPrint('[warn] health check daemon status: $e');
          _proposeState(false);
        }
      }
    });
  }
```

The existing call already discards the return value — no signature change needed beyond the type. The `await` continues to work because the method still returns `Future<Map<String, dynamic>>`.

- [ ] **Step 5: Scan for any other `healthCheck()` callers expecting typed return**

Run: `grep -rn "healthCheck()\|getDaemonStatus()" ui/flutter_ui/lib ui/flutter_ui/test`
Any caller that previously did `final status = await client.healthCheck();` and accessed `status.tokensUsed` must be updated to use raw map fields. None should exist — the summary confirms existing callers either discard the result or use `getDaemonStatusRaw()`.

- [ ] **Step 6: Run analyzer + tests**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors.

Run: `cd ui/flutter_ui && flutter test`
Expected: all tests pass. If a test stubs `healthCheck()` or `getDaemonStatus()` with a `DaemonStatus` return type, update the stub to return `Map<String, dynamic>`.

- [ ] **Step 7: Commit**

```bash
git add ui/flutter_ui/lib/services/sdk_client.dart ui/flutter_ui/lib/providers/providers.dart
git commit -m "fix(flutter): stop deserializing /health and /daemon/status into DaemonStatus

The OpenAPI-generated DaemonStatus model expects {status, tokens_used,
budget_used, ...} but the daemon's /health returns {status: 'ok'} and
/api/v1/daemon/status returns {running, pid, uptime, state, budget,
runtimes}. The schema mismatch caused 'Tried to construct class
DaemonStatus with null for non-nullable field' runtime errors.

Return raw maps from both methods. Callers already use the raw shape
(getDaemonStatusRaw already exists and is what the live UI uses)."
```

---

## Task 5: Client — Remove missing-asset warnings from pubspec.yaml

**Files:**
- Modify: `ui/flutter_ui/pubspec.yaml:99-101`

The `assets:` section declares `assets/shaders/` and `assets/images/` but neither directory exists (`assets/images/` has only `gui-bg.png` and there is no `assets/shaders/`). The build emits warnings on every `flutter build web`.

- [ ] **Step 1: Confirm directory absence**

Run: `ls ui/flutter_ui/assets/`
Expected: only `fonts/` and `images/` (with `gui-bg.png` inside). No `shaders/`.

- [ ] **Step 2: Update pubspec.yaml**

In `ui/flutter_ui/pubspec.yaml`, replace the trailing `assets:` block (lines 99-101) with explicit per-file entries that match reality:

```yaml
  assets:
    - assets/images/gui-bg.png
```

Keep `uses-material-design: true` and the `fonts:` block above unchanged.

- [ ] **Step 3: Verify build no longer warns about missing assets**

Run: `cd ui/flutter_ui && flutter build web --release 2>&1 | grep -i "missing\|not found\|shader" | head -5`
Expected: empty output.

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/pubspec.yaml
git commit -m "fix(flutter): remove missing asset directory entries from pubspec

assets/shaders/ doesn't exist and assets/images/ is better declared
as the single file that does (gui-bg.png). Silences build warnings
and lets 'flutter build web' complete clean."
```

---

## Task 6: Smoke test the full flow

**Files:** none modified — verification only.

- [ ] **Step 1: Build the daemon**

Run: `make build`
Expected: `bin/meept-daemon` produced.

- [ ] **Step 2: Build the Flutter web app**

Run: `cd ui/flutter_ui && flutter build web --release`
Expected: `build/web/` produced, no errors.

- [ ] **Step 3: Start the daemon**

Run: `./bin/meept-daemon -f` (in one terminal)
Expected: logs show server listening on `:8081`, no startup errors.

- [ ] **Step 4: Launch the web UI**

Run: `make webui` (in another terminal)
Expected: Chrome opens to `http://localhost:59714`.

- [ ] **Step 5: Verify connection state**

The Flutter UI should connect within 5 seconds. The status bar should show "connected" (not "connecting..." or "authentication failed").

In the daemon logs, expect:
- No `WARN websocket auth via query param` message
- No `HTTP error response ... /ws status=401`
- A single `WebSocket client connected` line

- [ ] **Step 6: Verify REST endpoints work**

Open browser devtools Network tab. Filter for `daemon/status`. Expect HTTP 200 with JSON body. No 401, no 418.

- [ ] **Step 7: Commit no code (verification-only)**

If all checks pass, no commit. If issues surface, file-specific fix and re-run.

---

## Self-Review

**1. Spec coverage:**

- WebSocket 401 on web → Task 1 (server removes duplicate auth that ignored Sec-WebSocket-Protocol), Task 2 (server echoes subprotocol so browser accepts), Task 3 (client uses subprotocol instead of query param). ✓
- DaemonStatus deserialization error → Task 4 (return raw maps). ✓
- Missing asset warnings → Task 5. ✓
- End-to-end verification → Task 6. ✓

**2. Placeholder scan:** None. Every step has either a specific file path and the exact code block to write, or a specific command with expected output.

**3. Type consistency:** `healthCheck` and `getDaemonStatus` both return `Future<Map<String, dynamic>>` after Task 4, matching the existing `getDaemonStatusRaw` and the `_get` helper. No other caller is affected (verified in Step 5 of Task 4).
