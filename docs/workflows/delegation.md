---
title: Delegation
---

# Delegation

Status: Shipped. Subagent orchestration surface: the `delegate_task` tool,
its control tools, the confined `workflow` orchestration tool, durable child
descriptors with cold resume, and the `[delegation]` model-route policy.

## Overview

Delegation lets a parent agent (or a chat turn) fan work out to child
subagents that run in isolated contexts — their own conversation, terminal
session, and toolset. Children know nothing of the parent conversation; every
piece of shared background must ride the task's `context` parameter. Only the
child's final summary returns to the parent.

Core implementation:

- `internal/agent/delegate.go` — the C1 async `DelegateManager` (spawn/settle/
  interrupt; `internal/agent/AGENTS.md` for contracts)
- `internal/agent/delegate_fork.go` — B2 history-seeded fork snapshots
- `internal/agent/delegate_descriptor.go` — B6 durable child descriptors +
  `DelegateManager.Resume` cold resume
- `internal/tools/builtin/delegate_control.go` — the model-facing control
  tools (`subagent_send_message`, `subagent_interrupt`,
  `subagent_list_children`)
- `internal/tools/builtin/delegate_workflow.go` — the confined `workflow`
  orchestration tool
- `internal/config/schema.go` — `DelegationConfig` (`[delegation]` block)

## delegate_task

`delegate_task` spawns a child subagent and returns immediately with a child
handle (`child_id` + `status: "running"`); settlement arrives later via the
`agent.delegate.result` bus event. When the daemon has no `DelegateManager`
bound, the tool falls back to the legacy synchronous path (blocking
`RunAgent`) byte-compatibly — the manager is bound once at daemon startup by
the delegate wiring seam (typed-nil guarded).

Parameters:

| Parameter | Type | Meaning |
|-----------|------|---------|
| `agent_id` | string | The specialist agent to run (e.g. `coder`, `planner`, `analyst`); discovered via `platform_agents` |
| `message` | string | The task to perform |
| `context` | string | Optional background the child needs — repeat everything non-obvious; children see only this |
| `output_schema` | object | Optional JSON Schema the child's final answer must validate against; on schema failure the parent gets one bounded correction retry before the result is marked invalid |
| `delegate_context` | enum | Request-side context handoff scope: `full` requests the child be seeded with this conversation's completed-turn transcript, `task` (default) starts the child from this task alone. The request is GATED by the target agent's own `delegate_context: full` AGENT.md frontmatter — a task-scope child degrades the fork to a fresh brief. |

> Note: `delegate_context` names the knob as shipped in this doc revision;
> the authoritative field list is the tool's `Parameters()` in
> `internal/tools/builtin/platform.go` — check there first if this page and
> the code disagree.

### Modes: spawn / fork / acp

The underlying `DelegateRequest.Mode` accepts three values (default `spawn`):

- **spawn** (default) — a fresh child conversation; nothing of the parent
  transcript is carried over. Cheapest and safest; pair with explicit
  `context`.
- **fork** — the child is requested to be seeded with a snapshot of the
  parent's conversation history, captured ONCE at spawn from the B2
  fork-seed seam (`completedTurnPrefix`, through the last COMPLETED turn —
  an in-flight tool-call turn is excluded). Seeding is GATED by the target
  child agent's own `delegate_context` frontmatter (see below): only an
  agent whose config sets `delegate_context: full` inherits the
  transcript; a default (`task`) child silently (well: with one Info log
  line) degrades to a fresh brief no matter what the request asks. The
  snapshot is frozen: later parent turns never bleed into a running child,
  and on cold resume the child replays the same spawn-time snapshot rather
  than re-forking the parent's newer history.
- **acp** — the child is an external ACP agent (Codex, OpenCode, ...) driven
  over JSON-RPC stdio; gated by `[acp] enabled` (default false).

The `delegate_task` tool surface exposes transcript inheritance through the
`delegate_context` argument (`full` maps onto fork seeding, `task`/absent
maps onto fresh spawn); `mode` is the internal request field the C1 manager
validates (`spawn|fork|acp`, anything else fails the spawn visibly).

### The `delegate_context` knob (DSH-1)

`delegate_context` is a PER-AGENT frontmatter key on the DELEGATED agent's
own `AGENT.md` — the requesting parent cannot force transcript inheritance
onto an agent that did not opt in:

```yaml
---
id: analyst
name: Analyst
delegate_context: full   # fork requests seed this child with the parent transcript
---
```

- `task` (default, and the meaning of an unset value) — the agent is treated
  exactly like any meept agent given a task: a fork request degrades to the
  fresh task brief, with one Info line naming the agent so the degradation is
  visible in the daemon log.
- `full` — a fork spawn seeds the agent's conversation with the parent's
  completed-turn snapshot BEFORE its first turn, and prepends a fork
  preamble so the child knows the inherited history is its own.
- Unknown values fail safe to `task` (no parse-time validation; the gate at
  `internal/agent/delegate.go` `childWantsForkSeed` accepts only `full`).

### Settlement and steering

- Spawn returns the child handle immediately; results arrive via
  `agent.delegate.result` (one event per settled child; background children
  re-enter the parent between turns).
- `subagent_list_children` enumerates the caller session's children
  (`child_id`, `agent_id`, `mode`, `state`, `model`).
- `subagent_send_message` delivers steering text a running child consumes at
  its next step; a settled child can no longer be messaged — continue it
  with a fresh `delegate_task` spawn.
- `subagent_interrupt` cancels a child (fire-and-return); the child's
  partial result is retained for audit.

## workflow tool

`workflow` orchestrates fan-out/fan-in in ONE tool call via a confined
JavaScript script (dsh-convergence leaf 05). The script body runs inside
meept's codemode goja VM (`internal/codemode` — no fs/network/timer/Node
APIs) and drives `agent()` spawns through the same C1 `DelegateManager`
seam the `delegate_task` tool uses.

- `meta` is a tool argument (name kebab-case, description, phases) — a
  script-body `export const meta` is a fatal `SCRIPT_PARSE` error.
- Config knobs (tool arguments, validated BEFORE any child spawns, never
  clamped): `max_concurrent_agents` (default `min(16, cores-2)`, ceiling 64),
  `max_total_agents` (default 1000, ceiling 100000), `max_items_per_call`
  (default 4096, ceiling 65536), `sync_timeout_ms` (default 5000),
  `engine_timeout` (default 30m wall).
- Errors are classed verbatim to the model, fail-close:
  `SCRIPT_PARSE`, `META_INVALID`, `INVALID_ARGUMENT`, `AGENT_CAP`,
  `ITEM_CAP`.
- Rendered output is capped at 50000 chars.

## Child descriptors and cold resume

Every spawn persists a durable child descriptor (B6, version-pinned:
`childDescriptorVersion = 1`; a future build's descriptor is REJECTED by
`Validate` rather than silently resumed with wrong semantics) in the
daemon-owned session store. The descriptor carries the routing pins
(provider/agent model), the mode, and the frozen fork snapshot seed.

`DelegateManager.Resume` re-spawns an interrupted or idle child from its
descriptor after a parent or daemon restart — replaying the frozen prompt +
snapshot rather than re-deriving anything from the parent's current state.
An interrupted child cannot be messaged back to life; resume is the only
continuation path. Optional seam: a manager built without the resume store
disables cold resume.

## Allowed routes ([delegation] config)

```json5
{
  delegation: {
    // Per-child model selection (default false: children inherit the
    // configured or parent route; provider/model request fields are ignored).
    enabled: true,
    // Exact provider+model allowlist a child's resolved route must match
    // before it may spawn. EMPTY = no allowlist in force (allow all).
    allowed_routes: [
      { provider: "openai",    model: "gpt-5" },
      { provider: "anthropic", model: "claude-sonnet" },
    ],
  },
}
```

Enforcement is `modelselect.PreflightRoute` at the spawn boundary: an
unlisted route fails the delegation VISIBLY (named error, no child created) —
never a silent downgrade to the parent's route.

## Edge cases

- **Child sees nothing but its brief.** Forgetting `context` is the #1
  delegation failure: pass file paths, error text, and constraints inline.
- **Settled children are terminal.** Steering a settled child returns a
  named error suggesting a fresh spawn; only `Resume` continues an
  interrupted one.
- **Fork snapshots are frozen at spawn.** A long-running fork child drifts
  from the parent's newer history by design; re-spawn to pick up new
  context.
- **Descriptor version skew.** After a meept downgrade, descriptors stamped
  by a newer build fail resume loudly instead of running with wrong
  semantics.
- **Route denial is a spawn failure.** A child that would exceed the
  allowlist never starts half-configured.

## See also

- `internal/agent/AGENTS.md` — delegation runtime contracts
- docs/workflows/agent-orchestration.md — the planner-side decomposition
  that rides delegation
- docs/workflows/acp.md — the external-agent (`acp` mode) surface
- docs/workflows/codemode.md — the sandbox the workflow script runs in
