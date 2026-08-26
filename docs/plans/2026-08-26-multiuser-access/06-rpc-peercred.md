# Leaf 06 — RPC peer-credential verification (defense in depth)

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: 01-auth-store NOT required; can run
concurrently with leaves 02-05 (touches only internal/rpc + internal/tui
read-side). Est. context ~40K.

## Goal

Add kernel-enforced identity checks on Unix-socket RPC connections. RPC
remains single-user by design, but the daemon now LOGS the connecting OS
user and refuses connections from UIDs outside an optional allowlist —
closing the "shared machine, shared socket" gap without adding tokens to
the socket path.

## Design

- macOS: getsockopt(LOCAL_PEERCRED) via unix.GetsockoptInt +
  LOCAL_PEERCRED → struct xucred (cr_uid). Linux: unix.GetsockoptUcred(fd,
  SOL_SOCKET, SO_PEERCRED) → Ucred{Uid}.
- Use golang.org/x/sys/unix (check go.mod first; vendored or direct dep
  both fine).
- Extract raw conn from *net.UnixConn: `conn.(*net.UnixConn)` inside the
  accept loop's connection wrapper.
- Config addition (internal/config/schema.go RPCTransportConfig):

```go
AllowedUIDs []int  `json:"allowed_uids" toml:"allowed_uids"` // empty = same-user only check disabled, log-only mode
PeerCredLog bool   `json:"peer_cred_log" toml:"peer_cred_log"` // default true: log uid per connection
```

Semantics:
- PeerCredLog=true (default): every accepted connection logs the peer UID
  at Debug level.
- AllowedUIDs non-empty: reject (close immediately) any peer whose UID is
  not in the list; log at Warn.
- AllowedUIDs empty: no rejection (current behavior preserved); log only.
- Platform without support (non-unix build): skip silently.

## Files

Modify:
- internal/rpc/server.go (accept loop: extract creds after Accept)
- internal/rpc/server_test.go (unit tests with real sockets)
- internal/config/schema.go (two fields above)
- tests for config round-trip

## Tasks (TDD)

1. Probe the current go.mod for x/sys availability; note result before coding.
2. Add peerCredential(conn net.Conn) (uid int, ok bool) with build-tag
   split files: server_peercred_unix.go (darwin+linux) and a no-op fallback
   (_other.go).
3. Wire into acceptLoop: log when enabled; enforce allowlist when set.
4. Tests: dial own socket → uid == os.Getuid(); allowlist containing own
   uid connects; allowlist excluding it gets closed before RPC handshake.
5. Config test: defaults preserve current behavior (empty list = log only).

## Self-Verification Checklist

- [ ] Cross-platform build passes: GOOS=windows go build ./internal/rpc/
      and GOOS=darwin / linux equivalents
- [ ] No I/O under mutex in accept loop changes
- [ ] Race clean: go test -race ./internal/rpc/ ./internal/config/
- [ ] Default behavior byte-identical when allowed_uids unset
- [ ] No new exported symbols beyond the two config fields

## Review Checklist (orchestrator)

- [ ] Build-tag files correctly named (no accidental double-registration)
- [ ] Warn-level rejection log includes peer UID and configured allowlist
      size but never token material
- [ ] AGENTS.md invariant note reported back: "RPC peer verification" under
      Critical Invariants if AllowedUIDs documented
