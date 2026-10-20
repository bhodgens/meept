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
become session events. Inbound `session/requestPermission` requests are
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
`session/requestPermission`. The pending registry lives in
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

## Edge Cases

- Missing catalog file loads as empty, not an error.
- Unknown permission_mode fails config validation at load.
- Transport close unblocks in-flight calls.
- Inbound permission requests keep their JSON-RPC id so `Reply` can answer.
- The daemon working directory is not the user project. Sessions pass the
  session working dir, never `os.Getwd()`.
- The Flutter status bar shows `acp:N` when live sessions exist. Disabled
  catalogs render nothing.
