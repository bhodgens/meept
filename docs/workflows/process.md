# internal/process — kernel process facts for caller identity

`internal/process` answers one question: **is this process a descendant of
that process?** It exists for the ACP approver-identity gate (audit M5,
`docs/plans/2026-10-22-acp-approver-identity.md`), which must distinguish
agent-controlled RPC callers from human ones.

## Why ancestry is the identity

Every process an agent can control — shell-tool children, PTY sessions,
terminal-exec commands, MCP stdio servers — was spawned, directly or
transitively, by the daemon. Human surfaces (TUI, menubar app, GUI, CLI)
connect from processes the daemon did NOT spawn. So "peer PID is the daemon
or a daemon descendant" separates the two classes with no registry, no
cooperation from the caller, and nothing the caller can forge: the PID comes
from the kernel (socket peer credentials), and the parent chain comes from
the kernel process table.

## API

```go
// ParentPID returns the parent of pid.
// (0, false) when the process is gone or the platform has no lookup.
func ParentPID(pid int) (int, bool)

// IsDescendantOf walks pid's ancestor chain looking for ancestor.
// Terminates at PID <= 1, on lookup failure, or after 64 hops.
func IsDescendantOf(pid, ancestor int) bool
```

Platform implementations:

| Platform | Source | Notes |
|----------|--------|-------|
| linux | `/proc/<pid>/status` `PPid:` | direct read, no cgo |
| darwin | `sysctl kern.proc.pid` (`unix.SysctlKinfoProc`) | uses `Eproc.Ppid` — the authoritative parent. `ExternProc.P_oppid` is ONLY valid under ptrace and must not be used |
| other | stub, always `(0, false)` | callers must fail closed |

## Usage (the RPC accept path)

`internal/rpc/server.go acceptLoop` calls this ONCE per connection, at
accept time, while the peer is provably alive:

```go
spawnedByDaemon := pidOK && (pid == os.Getpid() || process.IsDescendantOf(pid, os.Getpid()))
```

The result is stamped on the per-connection `PeerIdentity` and rides the
handler context. Walking at accept time (not at method-call time) avoids a
PID-reuse race: between accept and a later call the original peer could have
exited and its PID been recycled.

## Failure semantics

- Lookup failure (`ok=false`) is never "not a descendant" in security
  decisions — the ACP gate treats an unverifiable peer as unidentifiable and
  rejects privileged methods (fail closed).
- `IsDescendantOf(pid, 1)` is always false: PID 1 terminates the walk, so
  "descendant of init" (which every process technically is) never matches.
- The 64-hop bound (`maxAncestryDepth`) makes a pathological process table
  a false-negative, never a hang.

## Tests

`ancestry_test.go` exercises the walk on the LIVE process tree (self →
parent → grandparent), the reaped-child miss path, and the guard rails. On
platforms with the stub, the live-tree tests skip.
