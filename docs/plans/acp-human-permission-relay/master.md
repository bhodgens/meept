# ACP Human Permission Relay — Master Plan

Tree: `docs/plans/acp-human-permission-relay/` (master.md + leaves 01–06).
Parent tree: `docs/plans/acp-permission-decision/` (COMPLETE 2026-10-19,
commits 57093292 / 3d8128e3 / a1eef0f3). That tree's non-goal "no
human-in-the-loop relay" is THIS tree's goal.

## Problem statement

ACP `session/requestPermission` is answered by policy only: permissive
(grant all), deny (cancel all), decision (model vote, fail-closed). A human
who wants to gate external-agent side effects personally has no surface.
This tree adds:

1. `permission_mode: "human"` — each request pauses, a pending-permission
   record surfaces through bus → WS → TUI modal + GUI dialog; the human
   answers via RPC/HTTP; timeout cancels (fail-closed, matching master.md
   decision 3 of the parent tree).
2. `[acp] decision_escalate_to_human` (default false) — in decision mode,
   every NON-allow outcome routes to the same human relay instead of
   cancelling outright; human timeout or absent relay → cancel (parent
   fail-closed polarity preserved).

## User decisions (normative, 2026-10-19)

- Relay is requested by the user directly ("do the follow-up now: don't
  defer"). Full TUI + GUI parity is mandatory (root AGENTS.md invariant).
- v1 decision verbs: `allow` (maps to ACP selected/allow-once) and `deny`
  (maps to cancelled). No "always" option in v1 (optionId passthrough
  deferred; pinned here so nobody adds it silently).
- Timeout default 120 s, knob `[acp] relay_timeout`. Fail-closed: expired or
  unroutable requests cancel the permission, log warn, emit resolved event.
- One pending registry lives in `internal/acp` (session-adjacent, the only
  package that knows ACP request ids). Bus/RPC/HTTP/TUI/GUI are consumers.

## Frozen contracts

### Contract A — config (leaf 01)

ACPConfig gains:
```go
RelayTimeout            int  `json:"relay_timeout"             toml:"relay_timeout"` // seconds, default 120, Validate >0
DecisionEscalateToHuman bool `json:"decision_escalate_to_human" toml:"decision_escalate_to_human"` // default false
```
`Validate()`: PermissionMode switch adds `"human"` (so cases are
permissive/deny/decision/human; error message updated verbatim:
`permission_mode must be "permissive", "deny", "decision", or "human"`).
`relay_timeout > 0` enforced ONLY when mode == "human" or
decision_escalate_to_human (unused elsewhere may stay any value;
DefaultConfig sets 120 so the normal case is fine). config/meept.json5
[acp] comment block gains the fourth mode + two knobs. docs/configuration
acp knob table updated. `make docs-generate` after schema change.

### Contract B — relay seam + pending registry (leaf 02, internal/acp)

```go
// internal/acp/relay.go (new)
type PermissionNotifier interface {
    // NotifyPermissionRequested publishes the pending record; returns false
    // if no consumer is listening (caller still waits for Resolve until
    // timeout — a GUI that reconnects can still answer).
    NotifyPermissionRequested(p PendingPermission) bool
    // NotifyPermissionResolved publishes the terminal shape.
    NotifyPermissionResolved(p PendingPermission, decision, reason string)
}
type PendingPermission struct {
    ID        string // pkg/id.Generate("perm")
    AgentID   string
    SessionID string
    RequestID any    // JSON-RPC id echoed for resolve calls
    Summary   string // bounded 300-rune render of raw params (reuse
                   // renderDecisionState budget — call a shared helper)
    CreatedAt time.Time
    // unexported: resolve chan chan relayDecision, once sync.Once
}
func (r *Relay) Register(p PendingPermission) ...   // manager-owned registry
func (r *Relay) Resolve(id, decision string) error  // ErrRelayNotFound, ErrRelayAnswered
func (r *Relay) Pending() []PendingPermission
```
Manager: `SetPermissionRelay(...)` nil-safe setter; relay + notifier +
timeout flow into SessionConfig like the decision seam (stamp ONLY for
mode=="human"; decision-lane escalation ALSO stamps when
DecisionEscalateToHuman && mode=="decision").
Session: `handlePermissionHuman(n)` — registry insert, notifier call, wait
`select` on resolve chan vs `time.After(relayTimeout)`; allow→selected/
allow-once, deny/timeout→cancelled. NEVER holds s.mu; runs on Serve
goroutine (same shape as handlePermissionDecision).
Decision-lane branch: in `handlePermissionDecision`, when outcome is NOT
allow and escalation is armed, run the human relay wait INSTEAD of the
immediate cancel; event texts gain reason token prefix `escalated_human_*`
(allow→`escalated_human_allow`, deny→`escalated_human_deny`,
timeout→`deny` unchanged semantics, `no_relay` → fall back to plain cancel).
Fail-closed at every layer: no relay wired → today's parent-tree behavior
byte-identical (pin with tests).
Events (SessionEvent.Kind strings, frozen): `permission_requested`,
`permission_resolved` (Text: decision), plus existing `permission_request`/
`permission_decision` stay as parent tree defined.

### Contract C — daemon bus + RPC + HTTP (leaf 03)

- `internal/bus/topic.go` (or existing topic registry — leaf 03 reads
  precedent `escalation.event` raw Publish and the typed `bus.Topic[T]`
  convention and CHOOSES typed topic `acp.permission.requested` +
  `.resolved` since payload is stable; record the choice + reason in docs).
- acp PermissionNotifier impl in internal/daemon publishes those topics
  (wire via applyACPFromConfig caller site, components.go minimal hunk;
  disabled ACP → nothing wired, zero log lines).
- RPC: `acp.permissions.list` (pending snapshot),
  `acp.permissions.resolve` (params {id, decision}). Register even when
  manager nil (acp.list precedent: nil manager → empty list, resolve →
  not-found error envelope). internal/rpc/acp.go extended.
- HTTP: `GET /api/v1/acp/permissions`, `POST /api/v1/acp/permissions/resolve`
  (acp_status.go pattern). WS: the two topics must be RELAYED to WS —
  register the topic in the http-ws proxy subscription list (precedent:
  `agent.progress.synthesized` in server.go) so it lands as
  `agent_progress`. THIS IS THE CLASSIC WIRING DEFECT — hermetic rpc-level
  tests do not prove WS delivery; leaf 03's e2e must assert the WS frame.
- docs/generated/* will change (new routes + topic): leaf 03 regenerates
  from ITS OWN committed tree state per orchestrator instruction.

### Contract D — TUI (leaf 04) + Contract E — GUI (leaf 05)

Surfaces per root AGENTS.md parity rule:
- TUI: listen for the WS/bus event through the existing event path the TUI
  already consumes (internal/tui reads WS frames; check
  `escalation`/confirmation precedent). Render modal reusing
  `ConfirmationModel`/`runModal` machinery from internal/tui/tools.go with
  allow/deny keys; resolve via RPC. status-bar: nothing new (deferred —
  record deviation if TUI cannot scope modal + resolve together).
- GUI: `destructive_confirmation_dialog.dart` precedent — new
  `AcpPermissionDialog` fed by the WS `agent_progress` frame with
  `acp.permission.requested` payload; allow/deny buttons POST the resolve
  route. chat_provider.dart listens. Web-safe (no dart:io).
- Both surfaces: all UI text lowercase.
- Parity manifest: `gui_inventory.json` + parity_test.go gate if the
  palette/keybinding surface changes (leaf reads
  references/tui-gui-parity-audit contract in the skill? NO — leaves only
  get file paths; master notes: if keybindings added, update both +
  inventory).

### Contract F — docs + live gate (leaf 06)

- docs/workflows/acp.md: human mode section + escalation knob + relay
  timeout semantics + event table. docs/workflows/<new pkg files> if a new
  package dir appears (there is none — relay lives in internal/acp).
- AGENTS.md maintenance pass (WS classification bullet: new topic class
  mapping; likely summary-only edit in internal/comm/AGENTS.md if wsclass
  touched).
- LIVE GATE (the tree's real proof): scratch daemon, permission_mode=human,
  fakeagent triggers requestPermission, assert (1) WS frame arrives to a
  raw websocket client, (2) POST resolve allow → fakeagent echoes turn
  completed, (3) second request denied → cancelled reply, (4) third request
  left unanswered → timeout cancel (relay_timeout lowered to 5 s in the
  scratch config). Then decision_escalate_to_human=true with stub decider
  answering deny → relay frame arrives → human allow → agent proceeds.

## Leaves

- 01 `01-config.md` — Contract A. Branch: config. Files: schema.go,
  config/meept.json5, docs/configuration/acp page, generated config.md.
- 02 `02-acp-relay.md` — Contract B. Files: internal/acp/relay.go (new),
  session.go, session_test.go, manager.go, manager_test.go, decision.go,
  testdata/fakeagent (only if a second permission trigger is needed —
  prefer existing -mode permission). e2e: extend acp-wire suite for
  relay allow/deny/timeout over RPC surface. Branch: acp.
- 03 `03-daemon-surface.md` — Contract C. Files: internal/bus topic
  registry, internal/daemon (acp_register/components/ new acp_notifier
  file + test), internal/rpc/acp.go, internal/comm/http (acp_status.go +
  server.go ws-proxy list), e2e acp-wire or http suite for WS frame.
  Branch: daemon+rpc+http. Depends 01+02.
- 04 `04-tui.md` — Contract D. Files: internal/tui (event handler + modal),
  tests in existing tui test files. Depends 03 (RPC/WS live).
- 05 `05-gui.md` — Contract E. Files: ui/flutter_ui (dialog, provider
  listener), parity tests. Depends 03. PARALLEL with 04 (disjoint trees).
- 06 `06-docs-live.md` — Contract F. Depends 01–05.

Wave order: 01 → 02 → 03 → (04 ∥ 05) → 06 → integration gates.

## Integration gates

- daemon + cli build; `go test -p 2 ./internal/config/... ./internal/acp/...
  ./internal/daemon/... ./internal/rpc/... ./internal/comm/... ./internal/tui/...`
- `make e2e-fast-area AREA=acp-wire` (+ any touched suites); dart format +
  flutter analyze on GUI files; `make graphs` + `make graphs-check` from a
  CLEAN worktree at final HEAD (docs/generated committed in leaf 03 already
  — re-verify freshness at integration).
- `make docs-generate` + `make docs-check`.
- Pre-commit setters gate should be green at HEAD now (sibling landed);
  do NOT --no-verify without re-checking the gate directly first.

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| 01 | COMPLETE 2026-10-19 | 08e55aa0 | human mode + relay_timeout(120) + decision_escalate_to_human(false); conditional bounds; four-mode verbatim error pinned; setters gate green at HEAD directly |
| 02 | COMPLETE 2026-10-19 | 93547738 | relay.go registry (cap 32 reject-new, sentinels, buffered once-guard) + human lane + escalation lane (shared relayPermissionWait); 20 session + manager matrix tests; e2e HumanModeAllowDenyTimeout; parent pins untouched |
| 03 | COMPLETE 2026-10-19 | 7d079c52 + 9996bf53 + 9e4b6355 | bus typed topics + daemon notifier + rpc acp.permissions.list/resolve + http GET/POST routes + meept acp CLI + "acp.*.*" WS-proxy fix (latent gap: 3-seg topics matched no wildcard) + wsclass marker decode; DEFECT FOUND+FIXED: Start bound long-lived agent subprocess to turn ctx (context.WithoutCancel); full-stack e2e proof (real WS frame agent_progress, HTTP resolve, CLI, timeout fail-closed). errors hook blocked on 4 _ = tw.Flush sites — fixed to return/propagate (9996bf53 wiring tests were left unstaged by leaf, committed separately). graphs+docs regenerated from clean worktree (9e4b6355) |
| 04 | COMPLETE 2026-10-19 | c54f00a0 | ACPPermissionModal (FIFO, dedupe, y/n/esc per ConfirmationModel vocab — no new global keys so inventory untouched); event-pump branches for both acp topics + "acp.*.*" added to TUI EventStream list (same 3-seg gap as WS proxy); status token acp perms: N pending; failure keeps modal + retry dispatch; resolved-from-any-surface dismiss+toast. 5 flow tests via headlessProgram + real RPC stub; tui suite + e2e tui-flows green; sibling command_handler WIP excluded from commit |
| 05 | COMPLETE 2026-10-19 | 407f58e4 | model/provider/dialog triple (race-safe snapshot merge, optimistic clear + error retention), subscribeToAcpPermissions, router rootNavigatorKey + host in MaterialApp.builder, status badge; 23 new tests, flutter 634 pass, analyze 0 errors, dart format clean; sibling chat_input.dart WIP excluded. NOTE: label parity with TUI ("acp perms: N pending" vs "N acp permissions pending") reconciled in leaf 06 docs |
| 06 | PENDING | | |
| Integration | PENDING | | |
