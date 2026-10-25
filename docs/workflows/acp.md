---
title: Acp
---

# Acp

## Overview

Meept can drive external coding agents over the Agent Client Protocol (ACP).
The client lives in `internal/acp`. It speaks JSON-RPC 2.0 over stdio with
newline-delimited messages. Protocol version is the integer `1`.

This is the opposite of `meept mcp-chat-server`. MCP exposes meept *to*
other agents. ACP lets meept *drive* other agents (codex-acp, opencode acp).

## Problem

Codex, OpenCode, and similar harnesses do not share meept's tool loop.
Without ACP, meept cannot treat them as full agents. MCP tools only expose
narrow verbs. ACP carries sessions, prompts, permission requests, and
streaming updates.

## Behavior

Disabled by default (`[acp] enabled = false`). No subprocess starts until
the flag is on.

When enabled, meept launches a cataloged agent command, runs `initialize`,
then `session/new` / `session/prompt`. Inbound `session/update` notifications
become session events. Inbound `session/request_permission` requests are
answered according to `[acp] permission_mode`:

- `permissive` (default): auto-approve and log
- `deny`: auto-deny and log
- `decision`: ask the decision model (the resolved `decision_model` slot) and
  **fail closed** — approve only when the decider explicitly answers `allow`
  with confidence at or above `[acp] decision_threshold`; every other outcome
  cancels the request
- `human`: pause each request for a person to answer over the pending-permission
  relay (see "Human mode (permission relay)" below); an unanswered request
  cancels fail-closed after `[acp] relay_timeout`

The wire layer (`Transport`) correlates JSON-RPC ids, fans notifications,
and can `Reply` to inbound requests.

### Decision mode

Each decision-mode request asks one typed `noul` question — options
`allow` / `deny`, prompt "May this action run?", state = the agent id plus
the raw permission params truncated to 300 runes (v1 parses nothing). The
ask is bounded by a fixed 15 s timeout; a slow decider never wedges the
wire loop.

Outcomes appear as a `permission_decision` session event whose text is
`allow`, `deny`, or a deny-reason token:

| Token | Cause |
|-------|-------|
| `no_decisioner` | no decision client injected at wiring time |
| `unavailable` | client present but the `decision_model` slot resolves nothing |
| `ask_error` | the Ask call errored (transport, unavailability, parse failure) |
| `deny` | the decider answered `deny` |
| `answer_deny` | the answer was neither `allow` nor `deny` (noise treated as deny) |
| `low_confidence` | answered `allow` below `decision_threshold` |

Fail-closed is deliberate and the OPPOSITE polarity of the tool-call decision
gate (`decision_gate_enabled`, which fails open): a permission request guards
an external side effect inside someone else's agent session, so a down
decider must not silently grant it.

Independence: the lane's knobs live under `[acp]` only
(`permission_mode`, `decision_threshold`). Enabling decision mode requires a
configured `decision_model` slot but never reads or flips
`decision_gate_enabled`, `decision_escalation_threshold`, or
`compaction.decision_*`. The daemon shares ONE decision client instance
between the tool-call gate and the ACP lane when both are enabled.

### Human mode (permission relay)

`permission_mode = "human"` puts a person in the loop for every inbound
`session/request_permission`. The pending registry lives in
`internal/acp/relay.go` (the only package that knows ACP request ids);
bus/RPC/HTTP/TUI/GUI are consumers. Flow, fail-closed at every step:

```
requestPermission arrives
  -> register PendingPermission {id "perm-…", agent_id, session_id,
     request_id, summary (300-rune render of raw params), created_at}
     - registry full (32 pending)  -> immediate deny + WARN reason "full"
  -> emit permission_requested (text = pending id) + publish typed bus
     topic acp.permission.requested -> WS frame (agent_progress) to every
     surface listening; no listener = the wait STILL runs (a reconnecting
     GUI can answer) with an INFO "no listener, waiting for reconnect"
  -> wait on select: human resolve vs relay_timeout timer
     - resolve allow -> ACP reply {outcome: "selected", optionId:
       "allow-once"}; agent proceeds
     - resolve always -> ACP reply {outcome: "selected", optionId: the
       agent's own allow_always option (fallback: the allow-once chain)};
       the AGENT persists the grant
     - resolve deny  -> ACP reply {outcome: "cancelled"}
     - timeout       -> ACP reply {outcome: "cancelled"} + WARN
       "acp permission denied: human relay timed out"
  -> emit permission_resolved (text: "allow" | "always" | "deny" | "timeout") +
     publish acp.permission.resolved (decision + reason)
```

`relay_timeout` (seconds, default 120) is enforced `> 0` by config
validation only when `permission_mode = "human"` or
`decision_escalate_to_human = true`; a non-positive value at runtime falls
back to the 120 s default inside the session. Human answer verbs are
exactly `allow` / `deny` / `always` — the resolve boundary
(RPC/HTTP/CLI) rejects any other verb before touching the registry, so a
typo never becomes a silent deny. The decision lane (model vote) keeps
`allow` / `deny` only; `always` is a human-only verb.

### Always-allow semantics

`always` makes meept a TRANSPORT, not a memory (acp-always-allow master.md
design decisions 1-2). When a human answers "always", the ACP reply echoes
the AGENT's own `allow_always`-kind optionId from its requestPermission
`options` array — the agent persists the grant and stops re-asking within
its own session/scope. Meept persists NOTHING: no policy store, no
cross-session grants. An agent that offers no `allow_always` option (or no
`options` array at all) resolves "always" exactly like "allow" (allow-once
echo + default fallback) — never an error, never a silent deny. Surfaces
render the affordance from the agent-declared capability: the TUI footer
and the GUI dialog show the always key/button only when the pending
request carries an `allow_always`-kind option.

### Decision-lane escalation (`decision_escalate_to_human`)

With `permission_mode = "decision"` and
`decision_escalate_to_human = true`, every NON-allow decision outcome
(`deny`, `answer_deny`, `low_confidence`, `ask_error`, `no_decisioner`,
`unavailable`) routes into the SAME human relay wait instead of cancelling
outrightly. An `allow` outcome never touches the relay. The terminal
`permission_request`/`permission_decision` event texts are:

| Outcome | Text | ACP reply |
|---------|------|-----------|
| relay allow / always | `escalated_human_allow` | selected / allow-once (always echoes the agent's own allow_always option when offered) |
| relay deny | `escalated_human_deny` | cancelled |
| relay timeout | `deny` (parent cancel semantics kept) | cancelled |
| escalate armed, relay not wired | original decision text + WARN reason `escalated_human_no_relay` | cancelled |

Fail-closed polarity is preserved: `escalate = false` leaves the decision
lane byte-identical to the parent tree, and every escalation failure path
cancels exactly like a plain decision deny.

### Relay surfaces

| Surface | Entry point | Notes |
|---------|-------------|-------|
| TUI | `internal/tui/acp_permission.go` modal | FIFO queue per request; `y` allow, `a` always, `n` deny, `esc` dismiss-without-answering (stays pending, timeout fails it); the footer advertises `[a] always` ONLY when the head carries an allow_always-kind option (agent-declared capability; 3-key footer otherwise); auto-dismiss on the resolved frame from ANY surface + toast; failed resolve keeps the modal for retry |
| GUI | `ui/flutter_ui/lib/widgets/acp_permission_dialog.dart` + `acp_permission_provider.dart` | dialog host in `MaterialApp.builder` (rootNavigatorKey), allow/always/deny buttons POST the resolve route — the third `always` button renders iff the event's `options` carry an allow_always-kind entry; status-bar badge while pending |
| CLI | `meept acp permissions list [--json]`, `meept acp permissions resolve <id> allow\|deny\|always` | `cmd/meept/acp_cmd.go`; bad verb refused before any daemon call |
| HTTP | `GET /api/v1/acp/permissions`, `POST /api/v1/acp/permissions/resolve` `{id, decision}` | `internal/comm/http/acp_status.go`; disabled/nil manager = 200 with `{"enabled": false, "pending": []}` |
| RPC | `acp.permissions.list`, `acp.permissions.resolve {id, decision}` | `internal/rpc/acp.go`; registered even with a nil manager (list = empty, resolve = not-found) |

Resolve error sentinels (frozen strings): `acp: relay request not found`,
`acp: relay request already answered`, `acp: relay pending queue full`.
First answer wins; concurrent resolves of one id get
`already answered`.

### Events and topics

Session event kinds (internal/acp `SessionEvent.Kind`):

| Kind | Emitted by | Text |
|------|-----------|------|
| `permission_request` | permissive/deny/decision lanes | `approved`/`denied` or decision outcome text (escalation: the table above) |
| `permission_decision` | decision lane (+ escalation) | outcome / `escalated_human_*` text |
| `permission_requested` | human relay wait | the pending id (`perm-…`) |
| `permission_resolved` | human relay wait | `allow` / `always` / `deny` / `timeout` |

Bus + WS (typed `bus.Topic[T]`, payload `bus.ACPPermissionEvent`; keys
`id`, `agent_id`, `session_id`, `summary`, `created_at`, the additive
`options` array (`{optionId,name,kind}`, omitted when the agent offered
none), plus
`decision`/`reason` on the resolved frame):

| Topic | WS type | decision | reason |
|-------|---------|----------|--------|
| `acp.permission.requested` | `agent_progress` | empty | empty |
| `acp.permission.resolved` | `agent_progress` | `allow`/`always`/`deny` | `answered` / `timeout` (the fail-closed reject reasons `no_relay`/`full` land in the WARN log fields, not on a published frame — nothing reached the registry/surfaces in those paths) |

The payload carries a `wsclass` marker (`WSClass()` → progress), so
classification is marker-driven, never the legacy prefix table — relay
frames must never render as `chat_message` (blank-bubble invariant). WS
relay requires the `acp.*.*` wildcard in the proxy subscription list
(`internal/comm/http/server.go`): `bus.matchWildcard` compares segment
counts, so the leading `*` alone never matches these 3-segment topics —
the same gap was pinned and fixed in the TUI's EventStream list.

### Surface label parity note

The pending-count label differs per surface BY DESIGN: the TUI renders
`acp perms: N pending` (modal footer + status bar after esc-dismiss) and
the GUI status bar renders `N acp permissions pending`. Both strings fit
their surface's column width and vocabulary precedent (TUI terse status
tokens; GUI sentence-style `<n> … pending` spans, e.g. "n pending
changes", "n claims to review"). The capability itself is at full parity;
no code change is planned — this note is the record.

## Configuration

```json5
{
  acp: {
    enabled: false,
    agents_file: "~/.meept/acp_agents.json5",
    dial_timeout: 10,
    call_timeout: 120,
    max_agents: 3,
    permission_mode: "permissive",
    decision_threshold: 0.5,
    relay_timeout: 120,
    decision_escalate_to_human: false
  }
}
```

Catalog template: `config/acp_agents.json5` (copied on `make install`).
Each entry has `id`, `command`, `enabled` (per-agent, also default false).

## HTTP status

`GET /api/v1/acp/agents` (same auth as `/api/v1/mcp/servers`) returns:

```json
{"enabled": false, "agents": []}
```

when ACP is off or the manager is nil (still 200). When enabled, each agent has
`id`, `enabled`, `running`, `state`, and `uptime_s` (0 if untracked). RPC method
`acp.list` returns the same envelope.

Security: `acp_agent` verbs `launch`/`send` are HIGH; `read`/`stop` are LOW.
See [http-api](../reference/http-api.md#acp-client-agents). The Flutter GUI
consumes this endpoint in the same tree (leaf 09).

## HTTP relay surface

With the human relay armed (`permission_mode = "human"`, or `"decision"` +
`decision_escalate_to_human`), two more endpoints share the same auth
middleware:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/acp/permissions` | Pending permission snapshot: `{"enabled": true, "pending": [{id, agent_id, session_id, summary, created_at, options?}]}` (`options` is the additive agent-offered `{optionId,name,kind}` array, omitted when none); disabled/nil manager = 200 `{"enabled": false, "pending": []}` |
| POST | `/api/v1/acp/permissions/resolve` | Body `{"id": "perm-…", "decision": "allow"\|"deny"\|"always"}`; echo `{"decision": …, "reason": "answered"}`; 400 on missing id or a verb other than allow/deny/always |

RPC equivalents: `acp.permissions.list` / `acp.permissions.resolve`. CLI:
`meept acp permissions list [--json]` / `meept acp permissions resolve <id>
allow|deny|always`.

### Approver identity (self-approval gate)

`acp.permissions.resolve` and `.list` are gated on caller identity (audit
M5): a permission request is the *human* gate, so an agent must not be able
to answer its own request.

- **Socket callers** are identified by the kernel-verified peer PID
  (`SO_PEERCRED` on Linux, `LOCAL_PEERPID` on macOS). At accept time the
  daemon walks the peer's ancestor chain (`internal/process.IsDescendantOf`);
  a peer that IS the daemon or a daemon descendant — every shell-tool child,
  PTY session, and MCP stdio server — is rejected with `ErrACPSelfApproval`
  and sees only the disabled/empty list envelope. Human surfaces (TUI,
  menubar, GUI-over-RPC) are not daemon-spawned and pass. A peer whose PID
  cannot be verified fails closed.
- **HTTP callers** dispatch in-process (no socket peer), so the boundary
  attaches an `acp.Approver` fingerprint: `http-key:<sha256 prefix>` (never
  the raw key) or `http-anonymous:<remote addr>` for a `require_auth=false`
  loopback deployment. An in-process caller with NO attached approver is
  rejected — internal daemon code must never silently answer the gate.
- **Attribution**: the resolved approver rides the relay decision into the
  `permission_resolved` notification and the `acp.permission.resolved` bus
  event (`approver` key, additive), so every approval records WHO answered.
- **Resolve rank** (`[acp] resolve_rank`, opt-in, default false): a
  tighten-only decision-model second opinion on allow/always resolves that
  passed the identity gate. The rank (the `decision_model` slot, same
  `Decisioner` seam as the permission-decision lane) can reject an allow
  with `ErrResolveRanked` — the pending request stays answerable — but it
  never approves, never sees a deny verb, and never rescues an identity-gate
  rejection. No model, rank error, non-deny answer, or confidence below
  `decision_threshold` all fail OPEN: the identity gate is the hard
  boundary, and a dead ranker must not lock humans out of their own gate.
  The agent-controlled summary rides the ranked state inside an
  `<<<UNTRUSTED_REQUEST_SUMMARY>>>` fence, beside an extracted target line
  (toolCall title / content paths / rawInput command re-read from the raw
  params kept on the pending entry — the 300-rune summary truncation cut
  the path out of the ranker's reach, live finding 2026-10-25). Prompt V3:
  deny only on a clear deny-category match (credentials/keys, access
  control, security-policy config, remote-code piping), never on
  uncertainty — V1 under-flagged `authorized_keys`, V2 over-flagged a
  benign scratch edit.
- **Key roles** (`internal/auth`, audit M5 Phase 2b): when multi-user auth
  is active, each stored key carries a role — `human` (default; may answer
  the permission gate) or `api` (authenticates everywhere ordinary keys do,
  but `POST /api/v1/acp/permissions/resolve` returns 403 for it before any
  dispatch). Role-less legacy keys print and behave as human (no migration
  lockout). Mint with `meept keys add <user> --role api`. Legacy
  single-key mode (multiuser off) has no identities, so the role gate is
  inert there by design — the socket peercred gate, fingerprint
  attribution, and the resolve rank still apply.
- **Config**: `[acp] approver_identity` = `""`/`"enforce"` (default, gate on)
  or `"off"` (documented escape hatch; approvals stay attributed).

Phase 2b (key-role gating: only `role: human` keys may resolve over HTTP
when multi-user auth is active) is IMPLEMENTED — see "Key roles" above;
the socket peercred gate plus fingerprint attribution plus resolve rank
cover legacy single-key mode, which has no per-key identities.

## Personas and agent-definition executors (design, landing in waves)

Status: the persona lane and the `executor: acp` agent-definition branch are
DESIGN-STAGE, spec'd and approved in
[acp-employees.md](acp-employees.md) (decisions, phases, security analysis,
evidence — that doc is the authority). The pieces below land in code waves;
this section is the doc-side contract. What is ALREADY SHIPPED: the 9-entry
default ACP agent catalog (`config/acp_agents.json5`, commit 13eb1c63) — the
client, permission modes (permissive/deny/decision/human relay), approver
identity, and the `acp_agent` tool as documented above.

### The persona concept

N named personas share ONE catalog entry. Each persona is its own subprocess
session with its own argv tail (`command_args`, e.g. `-p <profile>` for a
hermes persona) and its own permission mode. The catalog entry stays generic
(one `hermes` entry, command `["hermes", "acp"]`); personas are defined by
the agent definitions that reference it. "have bob check that issue" routes
by agent name through the existing dispatcher — bob IS an agent definition
(`config/agents/bob/AGENT.md`), appears in `agents list`, the TUI roster, and
the GUI agents tab with no surface changes. A copy-pasteable example lives at
`docs/examples/agents/bob-acp-hermes/AGENT.md`.

### Agent-definition shape (`executor: acp`)

Frontmatter (verbatim from the spec):

```yaml
id: bob
name: bob
role: executor
executor: acp
acp:
  agent: hermes          # catalog id in acp_agents.json5
  persona: bob           # session key; multi-session lane (dim 2B)
  command_args: ["-p", "borg"]   # argv appended after the catalog command
  permission_mode: human # optional override of [acp] permission_mode
  decision_threshold: 0.7 # optional, only with permission_mode: decision
```

The fields flow through a 5-step pipeline (`AgentMetadata` →
`parseMetadata` altMeta merge for underscore keys → `AgentSpec` →
`definitionToSpec` → `mergeSpec`); skipping any step silently drops the
field — see the spec's "Field pipeline" section. When
`spec.Executor == "acp"`, the agent loop routes the turn to an `ACPExecutor`
instead of the LLM `reasoningCycle` (wave 2; final agent text still passes
the same output path — reply guard, scaffolding strip, filter chain).

`command_args` is OPERATOR-AUTHORED config — same trust class as `command`
in `acp_agents.json5`, never model-writable. Agent definitions are
operator-only files.

### `GetOrCreatePersona` contract (Manager multi-session lane)

- New method: `GetOrCreatePersona(ctx, agentID, persona string, opts
  PersonaOpts) (*Session, error)`. `PersonaOpts` carries CommandArgs
  (appended to `entry.Command` at spawn), a PermissionMode override, a
  DecisionThreshold override, and a Cwd override.
- Session key becomes the composite `agentID + "\x00" + persona` in both
  `m.sessions` and `m.pending`.
- Back-compat: `GetOrCreate(ctx, agentID, workdir)` (no persona) keeps
  working — it delegates with persona `""` and the key `agentID + "\x00"`,
  so existing callers and tests see identical behavior. Every
  permissive/deny `SessionConfig` without a persona stays byte-identical.
- `SessionConfig` gains `Persona string` and `CommandArgs []string`.
- `MaxAgents` now counts persona sessions (it already counts map entries;
  the semantics widen). A runaway roster of 50 bobs hits `ErrMaxAgents`,
  not fork-bomb.

### Relay persona attribution

`PendingPermission.AgentID` stays the catalog id; the struct gains
`Persona string` so the human relay UI shows `hermes/bob`, not bare
`hermes`. `renderDecisionState` includes the persona. Bus/WS session events
gain the persona field additively; existing consumers ignore unknown fields.

### Per-agent `permission_mode` override precedence

Precedence: agent-definition `acp.permission_mode` (frontmatter) >
`[acp] permission_mode` (global config fallback). The override flows through
`PersonaOpts` into `SessionConfig.PermissionMode`; the decision/relay lane
stamps follow the existing conditional-stamp rule (decision seam only in
decision mode, relay only in human mode or decision+escalate). Kill switch
unchanged: `[acp] enabled = false` disables every persona; per-entry
`enabled: false` disables a catalog agent and every persona on it.

## Edge Cases

- Missing catalog file loads as empty, not an error.
- Unknown permission_mode fails config validation at load.
- Transport close unblocks in-flight calls.
- Inbound permission requests keep their JSON-RPC id so `Reply` can answer.
- The daemon working directory is not the user project. Sessions pass the
  session working dir, never `os.Getwd()`.
- The Flutter status bar shows `acp:N` when live sessions exist. Disabled
  catalogs render nothing.
