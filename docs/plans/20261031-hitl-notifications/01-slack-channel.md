# Slack Approval Channel - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A new internal/comm/slack package: a services.PushChannel
  implementation that publishes pending permission requests as interactive
  Slack messages, verifies Slack request signatures, and resolves them
  through the existing ACP relay with a new "slack" Approver kind.
- **Dependencies:** R0-research.md (signing constants; dispatch context
  carries the verified values from research-notes.md)
- **Estimated Context:** 90K
- **Concurrency Group:** B
- **Audit references:** contract C1-C4 in ../master.md

## Goal

Today a permission request can only be answered from the TUI or the GUI.
This leaf adds a third surface: Slack. A pending permission is published as
an interactive Slack message (buttons: allow / deny / always); the button
action hits a daemon HTTP callback; the daemon verifies the Slack signature,
maps the action to a relay Resolve with Approver{Kind: "slack", Detail:
"<slack uid>"}, and the EXISTING fail-closed relay timeout (120 s default,
[acp] relay_timeout) stays the only timeout authority - a late click finds
no pending request and gets a polite ephemeral response.

## Context

Meept is a Go agent daemon (module github.com/caimlas/meept). The pieces
you build on:
- internal/acp/relay.go - human permission relay. PermissionNotifier
  (:94-107) is the daemon-side publish seam (NotifyPermissionRequested /
  NotifyPermissionResolved; implementations must not block - calls run on
  a permission goroutine with NO session lock held, mutexio rule). Verbs
  allow/deny/always (:82-86). Unanswered requests cancel fail-closed after
  [acp] relay_timeout (default 120 s; internal/config/schema.go:2662-2714).
- internal/acp/approver.go - Approver{Kind, Detail} with Kind values and
  String() rendering "kind:detail". You ADD the "slack" kind here
  (constants + String case + a doc comment). Nothing in internal/acp
  imports your new package (the kind is a plain string).
- internal/services/push_channels.go:34-56 - PushChannel interface
  (CanReceive, Push) + ChannelRegistry; wired at
  internal/daemon/components.go:2114 (pushRegistry). Telegram precedent:
  internal/comm/telegram/channel.go (imports internal/agent + internal/
  session - yours may import internal/services and internal/config).
- internal/services/push_service.go:22-24 - PushType enum
  (notification|alert|summary). Do NOT add values.
- internal/services/notification_prefs.go - per-type pref keys already
  anticipate "requires_approval"; read it for opt-in semantics, do not
  redefine push types.
- internal/daemon/components.go - where you register the channel and the
  HTTP route (pattern: other component wiring in that file).
- The unified HTTP server (internal/comm/http) - where POST
  /api/v1/slack/interactions is registered (pattern: existing
  /api/v1/* handlers in that package).
- Slack message publishing uses chat.postMessage with a bot token. The
  API base URL is configurable (C2 APIBaseURL) so hermetic tests run
  against an httptest server instead of slack.com.

## Verified Anchors

Anchors grep-confirmed 2026-10-31. Re-grep before editing - siblings move lines.

- internal/acp/relay.go:82-86 - escalated* consts sit beside the verb
  handling; relay verbs allow/deny/always.
- internal/acp/relay.go:94-107 - PermissionNotifier interface (2 methods,
  non-blocking contract, returned bool on Requested).
- internal/acp/approver.go:14-39 - Approver struct + Kind switch +
  String() rendering "kind:detail".
- internal/config/schema.go:2662-2714 - [acp] relay_timeout + its
  validation pattern (validate > 0 only when reachable) to mirror for
  [comm.slack].
- internal/services/push_channels.go:34-56 - PushChannel + ChannelRegistry.
- internal/services/push_service.go:22-24 - PushType enum.
- internal/daemon/components.go:2114 - pushRegistry := NewChannelRegistry(...).
- internal/comm/telegram/channel.go - sole PushChannel implementation
  precedent (constructor + CanReceive + Push shape).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C1 SlackChannel: `NewSlackChannel(cfg config.SlackConfig, logger
  *slog.Logger) *SlackChannel` with `CanReceive(ctx, sessionID, msg) bool`
  and `Push(ctx, msg *services.PushMessage) error` (services.PushChannel).
  Package: internal/comm/slack. Also: `RegisterHTTP(mux pattern per the
  internal/comm/http registration convention)` publishing POST
  /api/v1/slack/interactions (C3), and a `Notifier() acp.PermissionNotifier`
  adapter or equivalent wiring seam the daemon uses to connect the relay.
- C2 SlackConfig: struct in internal/config/schema.go exactly as pinned in
  ../master.md (Enabled/BotToken/SigningSecret/ChannelID/AllowedUserIDs/
  APIBaseURL), validated at config load.
- C3 callback behavior: signature header check (v0 HMAC-SHA256 over
  "v0:<timestamp>:<raw body>"), 5-minute replay window, action grammar
  "perm:<requestID>:<verb>", verbs allow|deny|always; unknown/expired
  requestID -> HTTP 200 + ephemeral "no pending request".
- C4 Approver kind "slack" in internal/acp/approver.go, String() renders
  "slack:<uid>".

### What This Leaf Consumes

- [acp relay] - Resolve / pending-permission registry APIs as they exist
  in internal/acp (grep Resolve( in internal/acp/relay.go before wiring;
  do not modify relay semantics).
- [C5 GateParkData] - NOT consumed by this leaf (leaf 02 owns it); your
  resolve path must NOT park or resume anything.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | SlackConfig struct + validation + config template entry | Create: internal/config (schema.go addition + defaults per existing pattern); Modify: config/meept.json5 (commented example); Test: internal/config/slack_config_test.go | `go test -p 2 ./internal/config/ -run TestSlackConfig` | struct absent | enabled=true missing token/secret/channel -> validation error at load; default enabled=false validates clean | Given [comm.slack] with enabled=true and empty bot_token; When config loads; Then a validation error names the missing key |
| T2 | SlackChannel skeleton: PushChannel methods + message rendering + relay Notifier adapter (NotifyPermissionRequested -> interactive message via APIBaseURL; NotifyPermissionResolved -> message update) | Create: internal/comm/slack/channel.go, internal/comm/slack/message.go; Test: internal/comm/slack/channel_test.go (httptest API server) | `go test -p 2 ./internal/comm/slack/` | package absent | Push posts chat.postMessage JSON to the test server with rendered buttons; Notifier methods never block (return within ms, no mutex held across HTTP - spawn goroutine or buffered queue per the seam's non-blocking rule) | Given a PendingPermission; When NotifyPermissionRequested fires; Then the test server received a chat.postMessage whose block payload carries allow/deny/always actions with values "perm:<requestID>:<verb>" |
| T3 | Interaction callback: signature verification + replay window + action parse + resolve round trip | Create: internal/comm/slack/interactions.go; Test: internal/comm/slack/interactions_test.go | `go test -p 2 ./internal/comm/slack/ -run TestInteractions` | handler absent | bad signature -> 401; timestamp older than 5 min -> 401; valid "perm:r1:allow" -> relay Resolve called with verb allow and Approver{Kind:"slack", Detail:"<uid>"}; uid not in AllowedUserIDs -> rejected; unknown requestID -> 200 + "no pending request"; signing secret never logged | Given a form-encoded block_actions body signed with the test secret; When POSTed; Then the relay stub recorded the resolve and the response is 200 |
| T4 | Approver kind "slack" + daemon wiring (register channel on pushRegistry, register route, connect Notifier to relay) | Modify: internal/acp/approver.go (kind constant + String case + doc comment); Modify: internal/daemon/components.go (wiring behind cfg.Enabled); Test: internal/acp/approver_slack_kind_test.go + internal/daemon/slack_wiring_test.go | `go test -p 2 ./internal/acp/ ./internal/daemon/ -run 'Slack'` | kind absent | String() renders "slack:U123ABC"; with slack disabled, no channel registered and no route served (default-off invariant); enabled wiring registers exactly one channel | Given Approver{Kind:"slack", Detail:"U123ABC"}; When String(); Then "slack:U123ABC"; Given config without [comm.slack]; When daemon starts; Then pushRegistry has no slack channel |

Worked example (canonical row expansion, T3):

1. Write failing test:

```go
func TestInteractionsRejectsBadSignature(t *testing.T) {
    h := newTestHarness(t) // httptest API server + relay stub + signing secret
    req := h.signedFormRequest(t, "payload=...", "v0deadbeef")
    rec := httptest.NewRecorder()
    h.handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusUnauthorized { t.Fatalf("got %d", rec.Code) }
}
```

2. Verify failure: `go test -p 2 ./internal/comm/slack/ -run TestInteractions`
   Expected: FAIL - interactions.go not defined.
3. Minimal implementation: constant-time HMAC compare
   (crypto/hmac + crypto/sha256, hmac.Equal), per the R0 constants inlined
   in your dispatch context (base string "v0:<timestamp>:<raw body>",
   5-minute window).
4. Verify pass: same command, PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C1-C4)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Security self-check: constant-time signature compare; replay window
      enforced; secret never in logs or errors; AllowedUserIDs enforced on
      EVERY action path; relay timeout semantics untouched (no new timer)
- [ ] gofmt clean; go vet clean on touched packages

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C2 struct byte-identical to master contract; C3 grammar exact
- [ ] Signature verification matches the R0-verified constants (check
      against research-notes.md section A)
- [ ] No blocking call inside PermissionNotifier implementations (mutexio)
- [ ] Default-off wiring proven by a test (disabled = no registration)
- [ ] internal/acp does NOT import internal/comm/slack (grep)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The relay's fail-closed timeout is the ONLY timeout authority. Your code
  must not add a second timer that auto-denies; lateness is handled by the
  pending registry answering "not found" on Resolve.
- Hermetic tests only: never hit slack.com. APIBaseURL + httptest covers
  both message publishing and (if used) response_url posting.
- If relay Resolve's signature differs from what C3 assumes, STOP and
  report the drift - do not adapt relay.go's semantics to fit the contract.
- Block Kit exact JSON shape is yours to choose; the pinned element is the
  action VALUE grammar, not the visual layout.
