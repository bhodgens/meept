# Bots and Employees

Both execution tiers are supported and live at runtime: **persistent bots** and
**AI employees**. An employee is a bot with a constitution, a goal loop, and an
enforcement engine — the employee layer (`internal/employee`) wraps the same
persistent bot runtime (`internal/bot`) rather than replacing it. See
[AI Employees](employees.md) and the
[AI Employee Design spec](../superpowers/specs/2026-06-23-ai-employee-design.md)
for the employee model in full.

## History note

The old `meept bots` CLI command tree was removed; use `meept agents` to list,
show, create, pause/resume, steer, and audit both bots and employees. That was a
CLI-surface removal, **not** a runtime removal. The bot runtime is live whenever
`bots.enabled=true` in `~/.meept/meept.json5`; it is also live when
`employees.enabled=true`, because the employee layer reuses `bot.Manager` and
`bot.Store` (the daemon initializes bot persistence when either flag is true).
When both flags are false the runtime stays dormant and byte-identical to a
no-bot boot.

## Execution observability

Both tiers publish their execution outcome and a shared turn-state edge over the
message bus. Each tier emits a `result event` on its **own** result topic; the two
share one wire payload (`agent.ExecutionResultEvent`, defined in
`internal/agent/execution_events.go`).

| Topic | Publisher | Payload |
|-------|-----------|---------|
| `bot.result` | `internal/bot/runner.go` (`BotRunner` / `bot.Manager`) | `agent.ExecutionResultEvent` |
| `employee.result` | `internal/employee/goal_loop.go` (`GoalLoop`) | `agent.ExecutionResultEvent` |

- The payload has **no `kind` field**. The topic name alone is the tier
  discriminator: `bot.result` means a bot, `employee.result` means an employee.
  `status` is a string carrying `completed` / `failed` / `skipped`;
  `stop_reason` follows the shared `#139` vocabulary (`end_turn` / `error` /
  `cancelled` for the three statuses respectively).
- Both tiers also publish turn-state edges on the shared `agent.turn.state`
  topic (`agent.TurnStateEvent`): `running` at execution entry, `idle` with the
  mapped stop reason at exit. No new turn-state type was added.
- Both result topics classify as `agent_progress` on the websocket relay
  (never `chat_message`), via the typed-wsclass decoder path in
  `internal/comm/http/server_wsclass.go`. Both are in the relay's explicit
  subscription list in `internal/comm/http/server.go`.
- Publishers are nil-safe: without a bus wired, the emitters are no-ops and a
  surface stays byte-identical. The daemon wires `c.msgBus` onto the bot manager
  and every goal loop at construction (`internal/daemon/components.go`).

See [inter-agent coordination](inter-agent-coordination.md) for the shared
payload and turn-state contract; issue #140 tracks this work. The generated
topology in `docs/generated/bus-topology.md` shows the publisher/subscriber
edges (regenerate with `make graphs`).

## Bot inbox addressability

Bots are first-class `send_agent_message` targets, alongside delegated agents and
employees. The messaging roster (`internal/tools/builtin/agent_message.go`)
resolves a target id against the agent registry, the employee manager, and the
bot manager, so a bot id is accepted and a bad id reports the valid targets.

Messages addressed to a bot (or employee) are stored in the shared employee
message store and drained at the start of the target's next turn. The drainer is
wired onto the per-turn loop clone — a message sent to a bot while it is idle
is visible on that bot's next goal round. Bots can be triggered by bus events
through the event-action router (`internal/daemon`); the router is started when
the bot manager starts, so bus-event triggers reach the handler.

The structured `AgentInboxBody` envelope (`body` plus optional `structured`
`tool_targets` / `file_paths` / `plan_entries`) applies to bot and employee
inboxes alike; see [inter-agent coordination](inter-agent-coordination.md) for
the wire shape.
