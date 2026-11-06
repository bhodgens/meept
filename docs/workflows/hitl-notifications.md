# HITL Notifications: Slack Approval Channel, Gate Parks, Deep Links

Human-in-the-loop notifications (plan tree `docs/plans/20261031-hitl-notifications`,
GitHub issue #86) give an operator three ways to see and act on work that
waits for a human: a Slack approval channel for pending ACP permission
requests, checkpointed gate parks that survive a daemon restart, and
clickable desktop-notification deep links into the session/activity surface.

Feature mapping: `internal/comm/slack/` (channel), `internal/agent/gate_park.go` +
`internal/daemon/gate_park_wiring.go` (parks), `ui/flutter_ui/lib/core/deep_link.dart` +
`menubar/MeeptMenuBar/Services/NotificationManager.swift` (deep links).

## The Slack approval channel (`[comm.slack]`, key `slack`)

When `[acp] permission_mode = "human"` (or `decision_escalate_to_human =
true`) parks a permission request in the ACP relay, an armed
`SlackChannel` publishes it into a fixed Slack channel as an interactive
card with `allow` / `deny` / `always` buttons; an approver's click comes
back as a signed interaction callback and resolves the request through the
relay with approver kind `slack`.

Enable it in `~/.meept/meept.json5` (schema: `config.SlackConfig`,
internal/config/schema.go; runtime shape: `slack.SlackConfig`, mapped by
the bridge in internal/daemon/slack_wiring.go):

```json5
"slack": {
  "enabled": true,
  "bot_token": "${SLACK_BOT_TOKEN}",        // xoxb-..., chat:write scope
  "signing_secret": "${SLACK_SIGNING_SECRET}",
  "channel_id": "C0123456789",
  "allowed_user_ids": ["U0123456789"],
  // "api_base_url": "https://slack.com/api",  // override for tests only
},
```

- **Default-off (T4).** `enabled` defaults false. When disabled the
  channel never registers on the push registry, `POST
  /api/v1/slack/interactions` does not exist on the HTTP server, and no
  goroutine runs. An absent block is identical to `enabled: false`.
- **Fail closed on approvers.** `allowed_user_ids` empty allows NOBODY —
  deliberately unlike the Telegram precedent where empty means all.
- **Secrets.** `bot_token` / `signing_secret` support the `${VAR}` env
  indirection the JSON5 loader applies (`ExpandEnvVars`); the values are
  never logged.
- **Validation.** `enabled: true` with any of `bot_token`,
  `signing_secret`, `channel_id` empty fails the C2 load-time contract
  (`config.SlackConfig.ValidateEnabled` / `slack.SlackConfig.Validate`);
  daemon wiring degrades to disabled with a warning rather than aborting
  boot.

### Interaction callback

Slack posts interactions to `POST /api/v1/slack/interactions` on the
daemon's unified HTTP server (`[transport.http]` host/port; the Requests
URL in the Slack app must reach it). The route is exempt from the bearer
API-key middleware — Slack cannot send one. Its authentication, enforced
inside the handler (internal/comm/slack/interactions.go), is:

1. the v0 request signature: `HMAC-SHA256(signing_secret,
   "v0:" + X-Slack-Request-Timestamp + ":" + raw body)`, compared in
   constant time, with a 5-minute replay window on the timestamp; and
2. membership of the clicking user in `allowed_user_ids`.

The payload's `response_url` is never an authorization boundary. The HTTP
200 ack goes out within Slack's 3-second budget; the relay resolve runs
async on its own goroutine (no request context — a human answer must not
be cancellable by an HTTP client). The button action value grammar is
`perm:<requestID>:<verb>` with verb in {allow, deny, always}; anything
else is answered politely and ignored.

### Timeout authority

The relay's fail-closed `[acp] relay_timeout` (seconds, default 120) is
the ONLY timeout for a pending permission request. The Slack channel adds
no second timer: a late click finds no pending request and gets a "no
pending request" ack. When a request resolves — from Slack, the CLI, the
GUI, or a timeout — the channel flips the posted card to its terminal
state via `chat.update` (best effort, bounded card tracking).

## Gate parks (checkpointed human gates)

A human gate pause — a plan in `pending_approval`, or a goal waiting on
operator signoff — is checkpointed through the existing `TurnParker` with
the `ParkKindGate` scope (internal/agent/gate_park.go). SINGLE PARK PATH:
no second park machinery exists.

- **Parks ride `plan.submitting`** (daemon wiring
  internal/daemon/gate_park_wiring.go): when a plan enters
  `pending_approval`, a `GateParkData{gate_kind: "plan"|"goal", gate_id,
  session_id, turn_reentry}` row persists in the shared `parks.db`
  ParkStore, keyed by `(ParkKindGate, "<kind>|<id>")`. Re-raising the
  same gate overwrites its own row.
- **Resumes ride two pinned choke points:** the `plan.approved` bus
  event (every plan-approval surface — RPC `plan.approve`, HTTP
  `PlanService.Approve`, CLI `./bin/meept plans approve <id>`, manager
  auto-approve — funnels through `PlanManager.ApprovePlan`, which
  publishes it) and the `agents.goals.approve` RPC path. Resume is
  event-driven (`ResumeByKey`), never time-driven: gate parks schedule a
  ten-year horizon so the parker's drain can never auto-approve a gate.
- **Restart re-arm.** `TurnParker.Start` (run in components startup after
  goal-loop registration) re-arms surviving gate rows, so an approval
  that arrives after a daemon restart still resumes the parked turn
  exactly once — the row is deleted BEFORE the resume callback
  (at-most-once: a crash mid-resume can never double-deliver).

`Checkpoint` (retry_recovery.go) is crash-recovery only; gate parks never
touch it.

## Notification deep links

Desktop notifications (menubar) and the Flutter GUI resolve into the
session/activity surface through the `meept://session/<id>` scheme.
Grammar (C6, byte-identical in both halves):

```
meept://session/<session-id>     # scheme "meept", host "session",
                                 # id matches ^[A-Za-z0-9-]+$
```

- **Menubar** (menubar/MeeptMenuBar/Services/NotificationManager.swift):
  a notification tap builds the URL from the notification's session id
  (validated against the mirrored regex — a hostile id never reaches the
  URL) and opens it, handing the GUI the link.
- **Flutter** (ui/flutter_ui/lib/core/deep_link.dart): the Dart half owns
  all navigation. It feature-gates the `/activity` go_router route at
  RUNTIME against the live router configuration (never an import): route
  present → navigate in-app to `/activity?session=<id>`; route absent →
  open the daemon's web session URL `<web base>/#/activity?session=<id>`
  (served by internal/comm/web). The macOS runner delivers the link over
  the `com.caimlas.meept/deep_link` MethodChannel registered by
  MainFlutterWindow.

## Enabling the full loop

```bash
# 1. ACP relay in human mode
./bin/meept config set acp.enabled true
./bin/meept config set acp.permission_mode human

# 2. Slack channel (values via env indirection in ~/.meept/meept.json5,
#    or: ./bin/meept config set slack.enabled true ...)
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_SIGNING_SECRET=...

# 3. Slack app: Interactivity ON, Request URL =
#    https://<your daemon host>/api/v1/slack/interactions,
#    bot scopes: chat:write
./bin/meept config get slack   # template shows keys; see docs/configuration/

# 4. restart the daemon and confirm registration
./bin/meept-daemon -f   # log line: "slack approval channel enabled"
```

When the channel is disabled, none of the above surfaces exist — the
feature is opt-in end to end.

## Related docs

- `docs/workflows/acp.md` — the permission relay and `permission_mode`.
- `docs/workflows/intake.md` — sibling opt-in daemon wiring pattern.
- `docs/plans/20261031-hitl-notifications/` — plan tree + research notes.
