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

The wire layer (`Transport`) correlates JSON-RPC ids, fans notifications,
and can `Reply` to inbound requests.

## Configuration

```json5
{
  acp: {
    enabled: false,
    agents_file: "~/.meept/acp_agents.json5",
    dial_timeout: 10,
    call_timeout: 120,
    max_agents: 3,
    permission_mode: "permissive"
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

## Edge Cases

- Missing catalog file loads as empty, not an error.
- Unknown permission_mode fails config validation at load.
- Transport close unblocks in-flight calls.
- Inbound permission requests keep their JSON-RPC id so `Reply` can answer.
- The daemon working directory is not the user project. Sessions pass the
  session working dir, never `os.Getwd()`.
