# testhttp — loopback test HTTP plumbing

Package: `internal/testhttp`. Test-support only: no production path imports it.

## Problem

A full `make test` sweep (package parallelism `-p 2`) fails a cluster of tests
with:

```
dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address
```

This is not a code bug. It is macOS ephemeral-port exhaustion:

- macOS hands outbound connections local ports from a range of only 16,384
  (`net.inet.ip.portrange.first`/`last` = 49152–65535).
- After a connection closes, the OS holds that port in TIME_WAIT for
  `2*MSL` = 30 seconds (`net.inet.tcp.msl` = 15000 ms).
- A full sweep opens thousands of loopback connections. The range transiently
  saturates, and the most socket-hungry package — the one that runs last —
  cannot get a local port and fails.

Measured on 2026-11-01 at HEAD `58901ca2`: 55 such failures under `make test`
(35 hermes, 14 web_fetch, 6 github, all in `internal/tools/builtin`), zero
under `go test -p 1`, and zero when `internal/tools/builtin` runs alone even at
`-count=5`. So the failure is concurrency-specific, not scenario-specific.

## Behavior

`internal/testhttp` gives every socket-heavy fixture one shared implementation
of two mitigations:

- `IsEphemeralExhaustion(err)` — classifies a dial failure as port exhaustion
  (EADDRNOTAVAIL, matched on the OS message text) as opposed to a refused or
  unreachable peer. Only this class is safe to retry.
- `DialWithEphemeralRetry` — a `net.Dialer` `DialContext` that retries ONCE,
  after a short backoff, and only when the first failure is exhaustion. A
  refused connection still fails fast.
- `WrapDialer(inner)` — wraps an existing dialer (for example an SSRF-checked
  dialer). The inner dialer runs on every attempt, so the retry re-enters the
  guard and never bypasses it.
- `BoundedTransport()` — an `*http.Transport` with the retry dialer plus tight
  idle-connection caps (`MaxIdleConnsPerHost` 2, `IdleConnTimeout` 1s). Idle
  connections are released promptly, so a finished test does not hold
  client-side ports in TIME_WAIT while later packages need them.
- `Client(timeout)` — an `*http.Client` over `BoundedTransport`. Pass `0` for
  `timeout` to leave the request unbounded (the caller's context governs),
  which the SSE path needs.

The pattern was extracted from the file-local helper in
`internal/comm/http/unified_http_test.go` (forest F3 close-out) so all fixtures
share one implementation.

### Wiring

- `internal/tools/builtin/fixture_http_test.go` — `boundedTransport`,
  `boundedHTTPClient`, and `allowPrivateBounded` helpers. The hermes, github,
  and web_fetch fixtures use them.
- `internal/hermes` — `Client.SetTransport`, `Client.SetSSETransport`, and
  `Client.CloseIdleConnections` let a test bound the sync and streaming
  clients' idle connections. `sse.go` prefers a per-client stream client when
  set, otherwise the shared timeout-less default. Production callers leave
  these alone.

## Configuration

None. The bounds are fixed constants tuned for single-host loopback test
servers. There is no runtime switch: the retry is gated on EADDRNOTAVAIL, so a
healthy run never pays the backoff.

## Edge cases

- A refused or unreachable peer is NOT retried. `IsEphemeralExhaustion` returns
  false for those, so a real test failure surfaces immediately.
- The classifier matches on the OS error message text.
  `TestExhaustionMarkerMatchesStdlibText` logs the platform's own
  EADDRNOTAVAIL wording and asserts the marker still classifies, so a platform
  with different wording fails the test rather than silently disabling the
  retry.
- `WrapDialer` must never bypass a security dialer. The unit test asserts the
  inner dialer runs on both attempts.
- The retry is test-only by policy. Production code must not hide port
  exhaustion; it should surface it.

## Related

- `AGENTS.md` — "Static Analyzers" and the e2e testing policy.
- `docs/workflows/e2e-testing.md` — sweep tiers and the manifest.
- `e2e/manifest.json` — `internal/testhttp/` maps to the `github-pr-ci` suite.
