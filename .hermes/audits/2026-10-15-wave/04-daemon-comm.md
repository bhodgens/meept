# Bughunt Audit: Meept Daemon + Communication Surface

**Date:** 2026-10-15
**Scope:** internal/daemon, internal/comm (including internal/comm/http and internal/comm/wsclass), internal/rpc, internal/bus, internal/transport, internal/logrotate

## Findings

### F1: WS Event Classification - `chat.response` Relay Check
- **Location:** `internal/comm/http/server.go:941-959`
- **Mechanism:** The `transformBusEventToWS` function explicitly filters out `chat.response` topic, returning nil (not relayed). Test files confirm this invariant.
- **Severity:** LOW
- **Test:** `go test -v ./internal/comm/http/ -run TestTransformBusEventToWS`

### F2: Bus Topic Integrity - Raw `Publish` with No Subscribers
- **Location:** `internal/bus/bus.go:115-285`
- **Mechanism:** `MessageBus.Publish` silently drops messages with no subscribers (direct or wildcard) and logs at DEBUG level. For `PublishExternalOnly`, it logs at DEBUG. For `PublishBlocking`, it logs at WARN. This is by design but could hide miswired topics.
- **Severity:** LOW
- **Test:** `go test -v ./internal/bus/ -run TestPublish_DebugOnNoSubscribers`

### F3: Log Cap - `os.Stderr` Usage Outside Rotation Seam
- **Location:** `internal/daemon/daemon.go:188`, `internal/daemon/components.go:7211`, `internal/daemon/launchd.go:44`
- **Mechanism:** The daemon's main logger uses `cfg.LogStream` (the logrotate cap) when set, but falls back to `os.Stderr` when `cfg.LogStream == nil`. This is intentional for tests and non-daemon callers. However, `components.go:7211` and `launchd.go:44` write directly to `os.Stderr` for subprocess stderr, which is outside the logrotate seam. These are not daemon.log writes but could fill disk if subprocesses leak.
- **Severity:** MEDIUM
- **Test:** Verify that `cmd/meept-daemon/main.go` passes a logrotate cap to `Daemon.New` with `LogStream` set.

### F4: Auth and Pairing - Dev Key Logging
- **Location:** `internal/comm/http/server.go:264-266`
- **Mechanism:** When `RequireAuth` is on and `APIKeys` is empty, the dev key is used. The logger warns "using development API key" with `\"default_key_visible\": false`. The key itself is NOT logged.
- **Severity:** LOW
- **Test:** Grep for dev_key in logs: `grep -rn 'dev_key' internal/ | grep -v 'DevAPIKey()'`

### F5: Pairing Code Single-Use Invariant
- **Location:** `internal/comm/http/pairing.go:117-142`
- **Mechanism:** `exchange()` performs constant-time comparison and then immediately zeroizes `p.code` and `p.printed`. This ensures single-use. The code is printed to `os.Stdout` (not slog), which is the designed delivery channel.
- **Severity:** LOW
- **Test:** Call `handleExchange` twice with the same code; second call should return failure.

### F6: Runtime Spawn Guards - `StartupOrphanSweep` Call Site
- **Location:** `internal/daemon/daemon.go:1970`
- **Mechanism:** `StartupOrphanSweep` is called during daemon startup. The orphan sweep is the only path that reaps orphaned runtime processes. No other spawn paths bypass this check (based on code review of `daemon.go` and `orphan.go`).
- **Severity:** LOW
- **Test:** Search for `os.StartProcess` or similar spawn calls outside of `StartupOrphanSweep` context.

### F7: Bus Proxy Registration - Live Responder Requirement
- **Location:** `internal/comm/AGENTS.md` (invariant doc)
- **Mechanism:** A bus proxy registration needs a live responder on both sides. The typed-bus `Topic[T]` mechanism enforces payload shape agreement at compile time via `PublishT`/`SubscribeT`. Raw `Publish` can miss subscribers if the topic name is mistyped.
- **Severity:** MEDIUM
- **Test:** Run `make graphs` and check `docs/generated/bus-topology.md` for topics with publishers but no subscribers.