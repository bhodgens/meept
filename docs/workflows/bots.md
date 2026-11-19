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

## Durable execution history (issue #160 follow-up)

The live WS relay is fire-and-forget: a client that disconnects misses runs.
The daemon therefore also projects every `bot.result` / `employee.result`
event into a durable SQLite store, so runs are queryable after reconnect AND
after daemon restart.

- Store: `internal/activity/execution_store.go` (`ExecutionStore`), table
  `execution_runs` in `$MEEPT_HOME/execution_runs.db`. Bounded retention
  (newest 5000 rows). One row per execution: tier (derived from the topic
  name), agent_id, status, output, tokens_used, error, duration_ms,
  stop_reason, trigger_ref, session_id, created_at (server-stamped UTC).
- Projector: `internal/activity/execution_projector.go`
  (`WireExecutionProjector`) subscribes to both typed topics; wired in
  `internal/daemon/activity_wiring.go`, torn down with the activity ring.
  Store-open failure degrades the query surface to a clean RPC error and
  never fails boot.
- RPC: `agents.executionRuns` (`{agent_id?, limit?, since?}` ->
  `{runs: [...], total}` newest-first, total counted before the limit) and
  `agents.executionHistogram` (`{agent_id}` -> `{agent_id, cells: [8 ints]}`,
  token volume per 7.5-minute cell over the last hour, oldest first).
  Registered in `internal/daemon/execution_runs_rpc.go`.
- HTTP: `GET /api/v1/executions` and `GET /api/v1/executions/histogram`
  (`internal/comm/http/execution_handlers.go`), plus
  `GET /api/v1/agents/roster` bridging the unified roster for active counts.
- GUI: the agents tab polls the history every 5s
  (`ui/flutter_ui/lib/providers/execution_runs_provider.dart`), merges live
  WS frames between polls, renders the executions list (colored status dot +
  `session:<worktype>` from trigger_ref) and the per-agent 8-bit token
  histogram on each agent card. The whole agents sidebar scrolls as one
  column (goals included).
- TUI: the agents panel `runs` sub-view (key `5`) pages
  `agents.executionRuns` on entry and every 5s while open; live frames still
  merge between polls.
- e2e: `e2e/suites/execution-observability` scenario 05
  (`TestExecutionObservability_PersistedHistory`) drives a real bot round,
  restarts the daemon, and asserts the same run survives.

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
