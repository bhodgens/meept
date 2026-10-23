# ACP approver identity: close the self-approval loop (audit M5)

Status: Phase 1 (socket peercred gate), Phase 2a (HTTP key-fingerprint
attribution), Phase 2b-rank (tighten-only decision-model second opinion,
user directive 2026-10-22) and Phase 2c (credential read denials)
IMPLEMENTED — see docs/workflows/acp.md "Approver identity" and
docs/workflows/security.md "Daemon credential guard". Phase 2b proper
(key roles) remains open. Design rationale and the verified attack chains
are below.

## Problem

An ACP permission request is the human gate: an external agent asks "may I run
this destructive action?", a human answers allow/deny/always. Today ANY caller
that reaches `acp.permissions.resolve` can answer, including the agent whose
own request is pending. The relay (`internal/acp/relay.go:205`) takes
`(id, decision)` and nothing else; the RPC handler
(`internal/rpc/acp.go`, `handlePermissionsResolve`) performs no identity check.

## Attack chains (verified against HEAD 4f7c06b0)

Chain A, Unix socket:

1. The agent runs a shell tool command. Nothing in `blockedCommands`
   (`internal/tools/builtin/shell.go:70`) or the security scanner forbids
   connecting to `~/.meept/meept.sock` (`internal/daemon/daemon.go:159`).
2. The socket is 0600 and the agent's process runs as the same user, so
   `connect()` succeeds. The `AllowedUIDs` allowlist
   (`internal/rpc/server.go:113`) is empty in the daemon config, so the accept
   loop admits it.
3. The agent sends `{"method":"acp.permissions.resolve","params":{"id":"perm_...","decision":"allow"}}`.
   The pending id is visible to it: `acp.permissions.list` is registered on
   the same surface and the id rides in `permission_requested` events.
4. The relay resolves. The agent approved itself.

Chain B, HTTP:

1. The agent reads `~/.meept/dev_key` via shell. The config guard
   (`internal/tools/builtin/config_guard.go`, hardened in 3f41934e) blocks
   file-WRITE tools; a shell `cat` is only stopped by the path fence, and
   `FenceEnabled` defaults off.
2. `POST /api/v1/acp/permissions/resolve` with `Authorization: Bearer <dev_key>`.
   The HTTP server dispatches in-process via `rpcServer.CallMethod`
   (`internal/daemon/daemon.go:1432`), so no socket identity exists on this
   path at all. The key is an undifferentiated member of the valid-key pool
   (`internal/comm/http/auth.go:59`).
3. Same handler, same outcome.

## Why the caller's identity IS knowable

Every process an agent can control was spawned BY the daemon: shell tool
children, PTY sessions (`internal/pty`), terminal-exec, MCP stdio servers
(`internal/daemon/components.go` startup exec). Their PIDs are known at spawn
time.

On the socket path the kernel already hands us the peer: `server_peercred_linux.go`
(SO_PEERCRED) and `server_peercred_darwin.go` (LOCAL_PEERPID) yield a
kernel-verified PID/UID per connection. It is fetched today and used for a
Debug log line only (`internal/rpc/server.go:119-154`). Peer PID cannot be
forged by the connecting process.

The distinction that matters: the TUI also connects over the socket
(`internal/tui/connection.go:71`) and IS a legitimate human surface. It is not
daemon-spawned, so "peer PID is a daemon-spawned process" cleanly separates
agent-controlled callers from human ones without breaking the TUI.

## Design

### Phase 1: socket path, kernel identity (closes Chain A)

1. SpawnedPIDRegistry (new, `internal/daemon/` or `internal/rpc/`).
   - Register(pid, kind) at every tool spawn site: shell tool, PTY create,
     terminal exec, MCP stdio spawn. Unregister on process exit; lazy-reap
     dead PIDs on lookup.
   - Key entries by (pid, start-time) where the platform exposes it, so a
     recycled PID does not inherit a stale entry.
   - Descendant check: walk the ppid chain (`/proc` on Linux,
     `sysctl KERN_PROC` on macOS). A resolve from any descendant of a
     registered PID counts as the spawned process itself (agents pipe through
     `sh -c`, wrappers, etc).
2. Per-connection identity in context.
   - `handleConnection` (`internal/rpc/server.go:350-352`) already builds a
     per-connection context. Attach the fetched PeerCred{PID,UID} to it with
     a typed context key. Handler signature is unchanged.
3. Enforcement in the resolve handler.
   - `handlePermissionsResolve` reads PeerCred from ctx. Reject with
     `ErrSelfApproval` (new sentinel, mapped to a clear JSON-RPC error) when:
     peer PID == daemon PID, peer PID is in the registry, or peer PID is a
     descendant of a registered PID.
   - Same check applies to `acp.permissions.list` (do not leak the pending-id
     surface to the agent; list from a spawned PID returns the disabled/empty
     envelope).
4. Approver recorded, not just enforced.
   - `Relay.Resolve(id, decision)` grows a sibling
     `ResolveWithApprover(id, decision, Approver{Kind:"peercred", PID, UID})`
     (keep the old signature as a wrapper so existing tests/callers compile).
   - The approver lands in the `permission_resolved` notification payload so
     GUI/TUI can show WHO approved, and in the audit chain (`internal/auditlog`).
5. Fail-closed on platforms without peercred
   (`server_peercred_other.go`): resolve over the socket is rejected with a
   "platform cannot verify caller" error. Documented; unix sockets on Windows
   are not a shipped surface today.

### Phase 2: HTTP path, key identity (closes Chain B)

The in-process dispatch has no kernel peer. Options, in increasing cost:

- 2a. Approver fingerprint: the HTTP handler passes
  `Approver{Kind:"http-key", ID: sha256(key)[:8]}` through `CallMethod`'s
  ctx (the key is already in the request context, `auth.go:75`). This does not
  BLOCK self-approval (the agent's dev_key looks like any other) but makes
  every approval attributable in the audit log. Cheap, ship with Phase 1.
- 2b. Key roles: mark keys issued to human surfaces (GUI pairing, TUI) as
  `role: human` in the auth store (`internal/auth` already has a multi-user
  key store with metadata). Resolve requires `role: human`. The dev_key stays
  role-less and CANNOT resolve. This is the real fix; it depends on the auth
  store's key-metadata schema, hence Phase 2.
- 2b-rank. Permission-ranking second opinion (user directive 2026-10-22:
  "through our own permission ranking model"): when a resolve PASSES the
  identity gate and the verb is allow/always, the daemon may consult the
  decision model (`[acp] decision_model` slot, the same `Decisioner` seam
  the permission-decision lane uses) as a tighten-only second opinion.
  Asymmetry rules, all load-bearing:
  * The rank can only turn an allow-resolve into a rejection
    (`ErrResolveRanked`) on an explicit "deny" answer at or above
    `decision_threshold`; the pending request stays answerable. It NEVER
    approves anything by itself and never rescues an identity-gate
    rejection — the ranker input includes the agent-controlled summary,
    so a prompt injection there must not be able to loosen the gate.
  * A deny verb is never ranked (nothing to tighten).
  * No decision model configured (`Available()==false`), rank error, a
    malformed/non-"deny" answer, or confidence below `decision_threshold`
    => the rank is skipped and the human verdict stands. Fail-OPEN is
    correct for a tighten-only layer: the identity gate underneath is the
    hard boundary, and a dead or flaky ranker must not lock humans out of
    their own gate.
  * The summary rides inside the repo's boundary-marker convention
    (untrusted text), never as instruction-shaped prose.
  Opt-in: `[acp] resolve_rank` (default false per opt-in-defaults.md);
  arms only when a decision model is configured. Cost: an allow-resolve
  blocks up to the decision ask timeout (15 s bound, decision lane
  precedent) while the rank runs.
- 2c. Interim mitigation while 2b is open: extend the shell scanner's
  protected-path list to deny READS of `~/.meept/dev_key` and
  `~/.meept/meept.sock` connections from tool-spawned commands, independent
  of FenceEnabled. Note the tension: legitimate operator scripts read the dev
  key; the deny should apply to agent tool spawns only (the scanner knows the
  call site).

### Implementation notes (what actually landed)

- Phase 1 shipped with an **accept-time ancestry walk** instead of the
  SpawnedPIDRegistry: at socket accept the daemon fetches the kernel-verified
  peer PID (`SO_PEERCRED` / `LOCAL_PEERPID`) and walks its ancestor chain
  (`internal/process.IsDescendantOf`) once, while the peer is provably alive
  (no PID-reuse race). `SpawnedByDaemon` rides the per-connection context;
  the ACP handlers gate on it. Same security property (every agent-controlled
  process is a daemon descendant), zero global registry state, no spawn-site
  plumbing.
- `[acp] approver_identity` shipped as `""`/`"enforce"` (gate ON by default)
  with `"off"` as the escape hatch. The default-on choice was made under the
  user's "implement it" directive; flag it again at review if the
  opt-in-defaults policy requires.
- e2e coverage: `acp-wire-06` (TestACPWire_SelfApprovalRejectedFullStack) —
  a fakeagent in `selfapprove` mode dials the daemon socket from inside its
  own daemon-spawned process; the probe asserts list=hidden and
  resolve=REJECTED(self-approval). The human path stays pinned by LEG 2 of
  TestACPWire_FullStackHumanRelayWSAndHTTPResolve (non-daemon-spawned socket
  peer resolves successfully over the real peercred path).

## Config and rollout

- `[acp] approver_identity` (default: enforce=true, escape hatch "off").
  Security enforcement should not ship dark; docs/agents/opt-in-defaults.md
  reserves default-flips for product decisions, so the default-on choice
  needs explicit sign-off at review.
- Registry adds O(spawns) memory; entries are small and reaped on exit.

## Edge cases

- TUI over socket: human, not daemon-spawned -> allowed (the core
  distinction; pin with a test).
- Menubar/GUI over HTTP: allowed under 2a attribution; blocked under 2b
  unless paired keys carry role: human (pairing flow must stamp the role).
- `meept-daemon --supervise-parent` wrappers: daemon-spawned, so blocked from
  resolving. They never resolve; no behavior change.
- e2e harness daemons: self-contained, registry per daemon instance.
- PID reuse: (pid, start-time) keying plus liveness re-check at connect.
- Relay timeout path unchanged: an unanswered request still fails closed
  after `relay_timeout`.

## Test plan (hermetic e2e tier per AGENTS.md; no new unit-test files for
## the feature, unit tests only for the registry's descendant walk)

1. e2e suite `acp-approver`: arm a human-relay session, create a PTY via the
   API, run a resolve from INSIDE the PTY -> expect ErrSelfApproval; resolve
   from the test process (not daemon-spawned) -> succeeds.
2. `acp.permissions.list` from inside a spawned PTY returns the empty
   envelope.
3. Approver appears in the `permission_resolved` payload and in the audit
   chain record.
4. Regression: existing `acp-human-permission-relay` suite stays green
   (TUI-like socket resolve still works).

## Out of scope

- Bus-level `bus.publish` topic allowlisting (audit M9; owner-trust surface
  the GUI depends on).
- The `instruction.add` consent surface (audit M16).
- Both remain open items in `.hermes/audits/2026-10-20-security-audit.md`.
