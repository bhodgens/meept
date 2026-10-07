# internal/comm/AGENTS.md

Guidance for AI agents working in `internal/comm/` (WS relay) and bus event
classification. Referenced from the root AGENTS.md; full WS classification
prose moved here from the root file. Update both in the same commit per the
root maintenance rule.

## WS event type classification

`transformBusEventToWS` in `internal/comm/http/server.go` maps bus topics to
frontend event types. Only the `chat_message` and `chat.message.received`
topics produce `type: "chat_message"`. The `chat.response` topic is
intentionally EXCLUDED from WS relay: it is an RPC reply consumed by
ChatService for the HTTP response body, and relaying it would double-deliver
the reply to HTTP+WS clients (Flutter GUI) — do not add it to the
`chat_message` bucket. All other `chat.*` lifecycle topics (heartbeats,
processing, worker events) produce `type: "agent_progress"`. The Flutter
client creates a visible message bubble for every `chat_message` event —
misclassified lifecycle events appear as blank messages.

Quota events on `agent.quota_wait` MUST be classified as `agent_progress`, never
`chat_message`. Classification derives from each payload's `WSClass()` marker
(`internal/comm/wsclass`) where the payload is typed (currently
`turn.terminal`/`TurnTerminalEvent`); all other topics classify through the
legacy topic-prefix table in `transformBusEventToWS` (kept as a labeled
fallback until every WS-visible topic is typed). Switch coverage is enforced by
the `exhaustive` linter (`//exhaustive:enforce` on
`wsclass.WSClass.String()`); a new `WSClass` constant without a covering case
fails CI. `agent.quota_wait` is intentionally RAW (not a typed `Topic[T]`):
it carries multiple payload shapes (`QuotaEvent`, `ParkTurnEvent`, and a
job-level map payload), and consumers discriminate by class/reason keys.

Future agents adding new bus topics: declare stable, single-shape topics as
`bus.Topic[T]` vars beside their payload structs (see
`internal/agent/topics.go`) and publish via `bus.PublishT` / subscribe via
`bus.SubscribeT`. Give any WS-visible payload a `WSClass()` method.

## The per-connection session filter is (channel, session)-scoped

`WebSocketHub` keeps one entry per connection (`wsConnSubs`): `sessions` is the
channel-agnostic grant set, `suppressed` is a `wsSuppression{channel, session}`
opt-out ledger. Both relay paths call `ShouldSend(wc, eventType, sessionID)`,
which resolves the event's channel through `wsEventChannel` (`chat_message` →
`chat`, `agent_progress` → `progress`, everything else → `all`).

Three rules, all pinned:

- **The GRANT is channel-agnostic, the OPT-OUT is not.** The Flutter client
  subscribes to one session three times — `subscribeToChat` (`chat`),
  `subscribeToAgentProgress` and `subscribeToTurnTerminal` (both `progress`)
  — and `unsubscribeFromChat` sends ONE `unsubscribe` frame naming `chat`.
  A connection-blind filter map made that single frame silence all three
  streams: navigating away from a session in the GUI also dropped its
  progress and turn-terminal, and the turn false-stalled to the 120 s
  liveness timer. A subscribe re-arms only ITS channel; an unsubscribe mutes
  only its own.
- **An unsubscribe never deletes the grant.** It records a suppression, so a
  connection that unsubscribed stays suppressed for non-subscribed sessions
  (least-surprise opt-out; deleting the entry returned the connection to
  broadcast mode and re-delivered what the client opted out of — pinned by the
  ws-filter-05 e2e scenario). A channel-LESS unsubscribe on a NAMED session
  records the `all` channel and therefore suppresses that session on every
  channel, which is the connection-wide opt-out an old client meant.
- **A channel-level unsubscribe with NO session sets a FLAG, never deletes.**
  `handleWSUnsubscribe` routes `{channel, no session_id}` to
  `SuppressAll`, which sets `wsConnSubs.suppressedAll` and KEEPS the grants
  (bughunt H3). It used to `delete(sessionSubs[wc])`, which does not suppress
  delivery at all: `ShouldSend` treats an absent filter set as BROADCAST mode,
  so the connection immediately began receiving every session's events again —
  including the ones it had explicitly unsubscribed from, the exact failure this
  filter exists to prevent. `SubscribeSession` clears the flag, so a client that
  unsubscribed channel-wide and then re-subscribed one session resumes normally
  instead of staying muted forever. The ws-filter-05 e2e scenario asserts this
  contract; it previously asserted the OPPOSITE (that delivery was restored) and
  would have blocked the fix.
- **A session-less event broadcasts to everyone.** Both relay call sites carry
  the `eventSessionID == "" ||` bypass. `SynthesizedProgressEvent.SessionID`
  derives from the source `AgentEvent.ConversationID`, which is empty for a
  session-less event; filtering on `""` matched no filter set and dropped the
  progress line for every armed connection while every other relay path
  broadcast it. `ShouldSend` is a pure filter query and deliberately does NOT
  repeat this rule — the bypass belongs at the call sites, where the decision
  is made.

Both subscribe frame shapes keep working: the `{type,data:{channel,session_id}}`
envelope and the flat Flutter frame `{type,channel,session_id}`.

## Bus topics: typed when stable, raw when open-ended

New bus topics with stable, single-shape payloads are declared as
`bus.Topic[T]` vars beside their payload structs (`internal/agent/topics.go`
is the worked example); publishers use `bus.PublishT` (or
`bus.PublishBlockingT` for must-not-drop events), subscribers use
`bus.SubscribeT`. The compiler then enforces publisher/subscriber payload
agreement. Raw string `Publish`/`Subscribe` remains for: wildcard
subscriptions, request/response bus patterns (`chat.request`/`chat.response`),
and multi-shape/open-ended topics — `agent.quota_wait` carries `QuotaEvent`,
`ParkTurnEvent`, and a job-level map payload and is deliberately raw. Do not
wrap `map[string]any` in a `Topic[T]`; that is ceremony without a guarantee.

## Bus proxy registrations must have a live responder

`internal/rpc/proxy.go makeProxy` publishes a request topic and waits on a
response topic. Before adding a proxy registration, verify BOTH sides exist
in source: a subscriber for the request topic AND a publisher that echoes
`ReplyTo` on the response topic (see `internal/memory/handler.go` for the
correct pattern). A proxy with no responder blocks the caller for the full
timeout (10-30s) and then fails — worse than method-not-found. Prefer a
direct `RegisterHandler` closure (the epistemic/memory_rpc/scheduler
pattern). The generator's `ANNOTATED_ORPHANS` table
(`scripts/gen-connectivity-graph.py`) suppresses topics with documented
external-only paths (e.g. `dispatcher.stats` via the `bus.publish` RPC);
add entries there instead of deleting intentional external surfaces.
