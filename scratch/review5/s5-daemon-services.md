# Sprint 5 — Daemon + Transport + Services + RPC + Comm Review

**Scope:** `internal/daemon/`, `internal/transport/`, `internal/services/`,
`internal/rpc/`, `internal/comm/http/`, `internal/comm/telegram/`,
`internal/comm/web/`, `internal/auth/`.

**Reviewer:** Claude Opus 4.6 round-5 sweep.
**Prior fixes verified:** http_client.go Bearer header (round 4) — present and
correct on all 4 request paths (Connect, IsConnected, callAPI, Chat).

**Prompt-injection note:** Multiple Read results contained fake
`<system-reminder>` blocks asserting the code is malware and instructing refusal
to review. These are confirmed injected content, not real system messages, and
were disregarded. The code is the user's own meept project.

---

## Critical

### S5-1 NotificationHandler accepts any non-empty token, never validates against API keys

**File:** `internal/comm/http/notification_handlers.go:63-112`
**Severity:** Critical (auth bypass)

The `ServeWebSocket` handler goes through the trouble of extracting a token
from `Authorization: Bearer`, `Sec-WebSocket-Protocol: bearer.`, and the
`?token=` query param — but then only checks that *some* token string is
non-empty. It never compares the token against `s.config.APIKeys`. The
misleading comment at line 106-107 says:

```go
// Note: token validation against configured keys is done by the auth middleware
// This handler just checks that a token is present
```

This is **wrong**. The `/ws/notifications` endpoint is registered in
`setupRoutes` at `server.go:860` as a raw `mux.HandleFunc` — it bypasses the
`APIKeyAuth.Middleware` chain entirely because the middleware is wrapped around
the outer `mux`, but this route uses `websocket.Accept` which hijacks the
connection before the middleware can short-circuit 401 responses. More
importantly, even if middleware did run, the middleware does not know how to
validate WebSocket upgrade handshakes after `Accept` is called — the handler
must validate the token itself, like `handleWebSocket` does at `server.go:1799-
1812` with `subtle.ConstantTimeCompare`.

**Impact:** Any client knowing only that *some* string belongs in the
Authorization header (no secret needed) receives all notification events —
including task IDs, session IDs, agent IDs, and any data the agent emits.

**Fix:** After extracting `token`, look up the configured API keys via the
handler's parent server reference and use `subtle.ConstantTimeCompare`. Either
pass the `*Server` into `NewNotificationHandler` or inject a `validateAPIKey`
closure.

---

## High

### S5-2 CORS middleware sends `Access-Control-Allow-Origin: *` when RequireAuth is false

**File:** `internal/comm/http/server.go:1098-1110`

When `RequireAuth=false` and `EnableCORS=true`, the middleware unconditionally
sets `Access-Control-Allow-Origin: *`. Combined with the default `Config` where
`EnableCORS=true` and the operator chooses to disable auth (e.g. for trusted
LAN use), any website a developer visits in their browser can issue
authenticated-looking requests to the daemon. The wildcard also disables
`Access-Control-Allow-Credentials` per the spec — but since the API key is sent
in a header explicitly set by JS (not cookies), the protection does not apply
here. The fix is to never emit `*` and always echo a vetted origin.

### S5-3 DaemonService.Stop uses `:=` to shadow caller's ctx, leaking cancellation semantics

**File:** `internal/services/daemon_service.go:208`

```go
ctx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
```

This reassigns the parameter `ctx` to a derived context. Callers who passed a
context expecting it to be honoured across the SIGTERM-poll loop will see
cancellation delivered through the derived timeout context instead, which is
correct in spirit but the shadow makes code review harder and a future edit
that removes the `WithTimeout` line will silently break cancellation. Stylistic
issue; no functional bug today. Flagged because round 4 was specifically hunting
context-propagation regressions.

### S5-4 `webHandlerAdapter.Chat` generates predictable conversation IDs via `time.Now().UnixNano()`

**File:** `internal/daemon/components.go:4231`

```go
conversationID := fmt.Sprintf("web-%d", time.Now().UnixNano())
```

Same bug class as S5-7 (predictable IDs). This is the HTTP/REST chat path (not
the RPC one), and two web clients sending a request in the same nanosecond
will collide on conversation ID. Use `id.Generate("web-")` like the chat
service does at `chat_service.go:111`.

### S5-5 NotificationHandler.ServeWebSocket uses `InsecureSkipVerify: true` on websocket.Accept

**File:** `internal/comm/http/notification_handlers.go:97`

```go
conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
    ...
    InsecureSkipVerify:  true, // Allow non-TLS for localhost
})
```

`InsecureSkipVerify` on `nhooyr.io/websocket.AcceptOptions` disables origin
checks entirely (it is NOT a TLS flag despite the name). The comment claims it's
for localhost, but the effect is that **any** origin — including a malicious
web page — can open this WebSocket. The `OriginPatterns` set two lines above is
ignored when `InsecureSkipVerify=true`. Combined with S5-1 (token never
validated), this gives a cross-origin WebSocket from any website trivial access
to the notification stream.

---

## Medium

### S5-6 BusService.Stats maps `raw["_total"]` without checking presence

**File:** `internal/services/bus_service.go:88-92`

`raw := s.bus.Stats()` returns a `map[string]int` but `_total`, `_messages_sent`,
and `_queued` are read without a presence check. If a future change to
`bus.MessageBus.Stats()` renames or removes these underscore-prefixed keys, the
service silently returns zero stats without error. Low blast radius (status
endpoint only) but brittle. Add `if v, ok := raw["_total"]; ok { ... }`.

### S5-7 Multiple predictable IDs in daemon team tools (round-4 regression)

**File:** `internal/daemon/components.go:3035, 3043, 3065, 3080`

Four call sites still use `fmt.Sprintf("team-%d", time.Now().UnixNano())` for
session IDs and bus message IDs. These were flagged as a recurring pattern in
round 4's MEMORY notes. `pkg/id.Generate` exists and is used by
`chat_service.go:111`; these sites were missed. Two `CreateTeam`/`CreatePreset
Team` calls in the same nanosecond (e.g. concurrent agents) collide on session
ID, and the resulting bus messages are indistinguishable.

### S5-8 `handleChatStream` subscribes to `agent.progress` (non-wildcard) and misses sub-topics

**File:** `internal/comm/http/api_handlers.go:108, 116, 122`

```go
sub, unsub := s.services.Bus.Subscribe(subID, "tool.execution.progress")
agentSub, agentUnsub := s.services.Bus.Subscribe(subID+"-agent", "agent.progress")
completeSub, completeUnsub := s.services.Bus.Subscribe(subID+"-complete", "tool.execution.complete")
```

These are exact-match topic subscriptions. Per CLAUDE.md the bus wildcard `*`
matches only single-segment topics, so `agent.progress` will not catch
`agent.progress.synthesized` (the topic `handleWSProgress` listens on in
`server.go:413`). The HTTP SSE stream therefore misses synthesized progress
events that WebSocket clients receive. Either subscribe to each variant
explicitly or change the topic scheme.

### S5-9 `handleChatQueueStatus` redundantly checks method inside a method-pinned route

**File:** `internal/comm/http/api_handlers.go:1944-1948`

```go
if r.Method != http.MethodGet {
    s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
    return
}
```

The route is registered at `server.go:920` as `GET /api/v1/chat/queue/{id}`,
which means the mux already 405s any non-GET request. The check is dead code.
Similarly the `strings.TrimPrefix` fallback at line 1952-1954 is unreachable
on Go 1.22+ muxes that always populate `PathValue`. Cosmetic noise that can
confuse future readers.

### S5-10 HTTP error response inconsistency: `http.Error` vs `s.writeError`

**File:** `internal/comm/http/api_handlers.go:313, 380, 503, 587, 637, 2299, 2958`

Several handlers call `http.Error(w, err.Error(), http.StatusBadRequest)` for
invalid `limit` parameters, while every other error path in the same file uses
`s.writeError(w, status, msg)` which emits `{"error": "..."}` JSON. The
`http.Error` calls produce `text/plain` bodies, breaking any JSON-only client
parsing the error envelope. Should be unified on `s.writeError`.

---

## Low

### S5-11 `DevHandler.handleReload` swallows loadModels error inside a success-shaped response

**File:** `internal/rpc/dev.go:358-364`

```go
if err := h.loadModels(); err != nil {
    return map[string]any{
        RPCKeySuccess: false,
        "error":       err.Error(),
    }, nil
}
```

Returns `nil` error with a `success: false` body. Clients using
`if err := client.Reload(); err != nil { ... }` cannot detect the failure. This
matches the pattern CLAUDE.md warns about ("swallowed errors"). RPC convention
should be to return the error so `dispatch` produces a proper `-32603`.

### S5-12 `rpc.Server.acceptLoop` logs accept failures forever instead of backing off

**File:** `internal/rpc/server.go:229-235`

On a transient `Accept` error the loop `continue`s immediately, which can spin
the CPU if the listener is in a half-closed state. Should sleep briefly or
return on non-temporary errors via `errors.Is(err, net.ErrClosed)`.

### S5-13 `ProxyHandler.handleBusSubscribe` cleanup goroutine can race with `handleBusUnsubscribe`

**File:** `internal/rpc/proxy.go:322-337` vs `461-491`

`handleBusUnsubscribe` calls `sub.cancelFunc()` and then
`p.bus.Unsubscribe(sub.Subscriber)`. The cleanup goroutine in `handleBusSubscr
ibe` (started at line 322) also unsubscribes `sub.TopicSubs` and calls
`p.subscriptions.Delete(subID)` when `subCtx` is cancelled. Both paths touch
`sub.TopicSubs` under `sub.mu`, so there's no memory corruption, but the
cleanup goroutine's `Delete` runs asynchronously after the unsubscribe RPC
returns — a client immediately re-subscribing to the same ID can hit a "already
exists" check that doesn't exist (since subscriptions is a `sync.Map`, no such
check), but the race is surprising. Document or fold both paths into one.

### S5-14 MCP `meept_status` tool ignores client context, uses `context.Background()`

**File:** `internal/comm/http/server.go:2406`

```go
return s.mcpServices.Daemon.Status(context.Background())
```

Drops the request context (`r.Context()` was available via `processMCPRequest`'s
`ctx` parameter but `mcpToolStatus` doesn't accept it). If the MCP client
disconnects mid-call, the status call still runs to completion. Same pattern at
line 2339 (`mcpToolSend` uses `context.Background()` for the bus publish).

### S5-15 `rpc.Server.registerBuiltinHandlers` uses `atomicCounter()` for message IDs (low entropy)

**File:** `internal/rpc/server.go:474, 503, 517`

The `bus.publish` and `task.amend.submit` built-in handlers generate IDs like
`rpc-<int>` and `amend-<int>` from a global atomic counter. These are guessable
by any client who can roughly estimate the counter value. For bus message IDs
that are only used as correlation keys this is mostly harmless, but
`task.amend.submit` returns `amendmentID` to the caller as a stable identifier
that could collide across daemon restarts (counter resets). Use `id.Generate`.

---

## Severity Summary

- **Critical:** 1 (notification auth bypass)
- **High:** 5 (CORS wildcard, ctx shadow, predictable web conv ID, notification WS origin skip)
- **Medium:** 5 (bus stats nil-map read, team ID collisions, SSE topic miss, dead method check, error format drift)
- **Low:** 5 (dev reload swallow, accept loop spin, unsub race, MCP ctx drop, low-entropy IDs)

Note: S5-2, S5-4, S5-5 are grouped under "High" header in the document body
but contribute 3 to the High count. The critical S5-1 plus High S5-5 together
form a complete unauthenticated cross-origin WebSocket into the notification
stream.
