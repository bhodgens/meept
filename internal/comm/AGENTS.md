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
