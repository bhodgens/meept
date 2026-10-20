# Leaf 03 — bus topics + RPC + HTTP + WS relay (Contract C)

Parent: `master.md` Contract C. Depends: leaves 01+02 committed (read
`internal/acp/relay.go` Manager API on disk: SetPermissionRelay shape,
PendingPermissions()). Branch: daemon+rpc+http. Owns: bus topic names,
WS-proxy registration, RPC/HTTP handlers.

## VERIFIED precedents (use these, do not invent)

- Typed topics: `bus.NewTopic[T]` / `PublishT` / `SubscribeT`
  (internal/bus/topic.go:14-80). Payload is stable → USE TYPED topics.
  Define in `internal/bus/topic.go` if that is where shared Topic vars
  live (grep `NewTopic[` there and follow suit).
- WS relay: raw-name subscribe pattern at
  `internal/comm/http/server.go:767` (`msgBus.Subscribe("http-ws-...
  ", "agent.progress.synthesized")`) + the topic-routing filter block
  around :922-927. ACP permission events MUST be added to the relayed set
  so they arrive as `type: "agent_progress"` WS frames. Read the filter
  carefully — there is an EXCLUSION branch for synthesized; do not let
  acp topics fall through it.
- RPC: `internal/rpc/acp.go` (acp.list, RegisterACPMethods) — extend, do
  not replace; nil-manager envelope pattern is pinned there.
- HTTP: `internal/comm/http/acp_status.go` GET /api/v1/acp/agents via
  rpcCall — copy shape for the two new routes.

## Tasks

1. RED tests in EXISTING files: internal/rpc/acp handler tests (grep test
   file owning acp.list), internal/comm/http acp_status_test.go, e2e
   acp-wire suite gets the WS-frame assertion (the hermetic RPC tests are
   NOT sufficient — master's wiring-defect note; the e2e must open the WS
   and assert `acp.permission.requested` frame arrives while a relay
   request is pending, then POST resolve and assert frame `...resolved`).
   Payload keys (frozen): id, agent_id, session_id, summary, created_at;
   resolve reply echo decision + reason.
2. Topic defs: `TopicACPPermissionRequested` = "acp.permission.requested",
   `...Resolved` = "acp.permission.resolved", payload struct (exported
   fields, json tags snake_case) — put payload in internal/bus or a shared
   spot importable by daemon+http WITHOUT importing internal/acp (check
   import cycles: internal/acp must not gain a bus dep; daemon does the
   adaptation).
3. Daemon notifier: new file internal/daemon/acp_notifier.go implementing
   acp.PermissionNotifier over *bus.MessageBus (PublishT; Notify returns
   true iff subscriber count > 0 — PublishT returns int receivers). Wire in
   acp_register.go/components.go ONLY when mode human or
   decision_escalate_to_human (disabled/non-relay configs: zero hunks
   behavior, zero logs — existing contract test pins).
4. RPC: `acp.permissions.list`, `acp.permissions.resolve` (params {id,
   decision}; decision must be "allow"|"deny" else invalid-params error
   envelope). Registered even when manager nil (empty list / not-found
   error) per acp.list precedent. cmd/meept: extend the existing acp CLI
   command (grep `acp` in cmd/meept/) with `permissions` subcommand (list/
   resolve <id> allow|deny) — CLI surface required by repo wiring rule.
5. HTTP: GET /api/v1/acp/permissions; POST /api/v1/acp/permissions/resolve
   {id, decision}.
6. WS: server.go relay registration (point 1's e2e proves it).
7. Regenerate graphs FROM CLEAN WORKTREE for the post-leaf-03 tree state —
   coordinate with orchestrator (sibling WIP: orchestrator runs
   gen-connectivity-graph.py + docs-generate in a /tmp worktree at the
   leaf's commit and copies deltas). Expected deltas: new routes, new
   topics.

## Verify

`go test -p 2 ./internal/rpc/... ./internal/comm/... ./internal/daemon/
-count=1`; e2e acp-wire with WS assertion green; CLI smoke:
`go run ./cmd/meept acp permissions list` against scratch daemon (or the
e2e harness RPC) — record. Do NOT commit.
