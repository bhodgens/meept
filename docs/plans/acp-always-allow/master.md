# ACP Always-Allow Option — Master Plan

Tree: `docs/plans/acp-always-allow/` (master.md + leaves 01-core, 02-surfaces).

## Origin

`docs/plans/acp-human-permission-relay/master.md` (user decisions): "v1
decision verbs: allow and deny. No 'always' option in v1 (optionId
passthrough deferred; pinned here so nobody adds it silently)." This tree
is that pinned decision, revisited on user request (2026-10-19).

## Verified current state (drift-audited, do not re-derive)

- Allow replies ALREADY echo the agent's offered optionId:
  `resolveAllowOptionID` (internal/acp/session.go:386) parses the
  requestPermission `options` array, prefers kind `allow_once`, falls back
  to `defaultAllowOptionID = "allow-once"`. So generic optionId passthrough
  is NOT the gap; the gap is the allow_always SCOPE.
- Relay registry: `internal/acp/relay.go` — PendingPermission value
  snapshot, `Resolve(id, decision)` normalizes anything != "allow" to deny
  (decision.go/relay.go). relayDecision{text, allow}. SessionID accessed
  via `s.getSessionID()`; waits run on `Transport.dispatchPermission`
  goroutines (post-head-of-line-fix architecture, commit b4323721).
- RPC boundary: internal/rpc/acp.go validates decision ==
  "allow"|"deny" (acp.go:138 region), error strings pinned in tests +
  docs/reference/rpc.md.
- Notifier payload: internal/bus/acp_topics.go ACPPermissionEvent —
  frozen keys {id, agent_id, session_id, summary, created_at, decision,
  reason}.
- Surfaces: TUI internal/tui/acp_permission.go (y/n/esc), GUI
  ui/flutter_ui dialog (allow/deny buttons), CLI cmd/meept/acp_cmd.go
  (verb validated offline), HTTP POST resolve.
- fakeagent: internal/acp/testdata/fakeagent/main.go permission mode
  sends requestPermission — CHECK whether its params include an options
  array; leaf 01 adds one (with allow_always) if missing, keeping older
  legs' expectations.

## Design decisions (normative)

1. Semantics: meept is a TRANSPORT for "always", not a memory. When a
   human answers "always", the reply echoes the agent's OWN
   `allow_always`-kind optionId (ACP spec: the agent persists the grant
   and stops re-asking in that session/scope). Meept persists NOTHING —
   no policy store, no cross-session grants (that would be a different,
   larger product; recorded as future work in docs).
2. Agents that offer no allow_always option: "always" resolves like
   "allow" (allow_once echo + default fallback). Never error, never
   silently deny.
3. Backward compat: RPC/HTTP/CLI `decision` param gains "always" as a
   third valid value; every existing allow|deny behavior byte-identical
   (pins stay green). Offline verb validators updated with it.
4. Event stream: relay resolved text gains "always"; relayPermissionWait
   reply uses resolveAlwaysOptionID. Session-level decision-lane
   escalation unaffected (the model cannot vote "always" — options stay
   allow/deny there; ONLY human answers gain always).

## Contracts

### Contract A — core + wire (leaf 01)

- `PendingPermission` += `Options []PermissionOption` (exported type move
  or alias from session.go's permissionOption — ONE definition),
  populated from raw params at register time (bounded: keep max e.g. 16
  options).
- `Relay.Resolve(id, decision)`: accept "always"; relayDecision gains
  allow=true + scope=always. Unknown-decision normalization unchanged
  (anything not allow/always -> deny).
- New `resolveAlwaysOptionID(rawParams) string`: kind == "allow_always"
  first, then kind contains "always"&&"allow", else "" (empty = caller
  falls back to resolveAllowOptionID path).
- `handlePermissionHuman` / `relayPermissionWait` reply shape: on always
  use `optionId: resolveAlwaysOptionID(...) or fallback allow-once`.
- bus `ACPPermissionEvent` += `options` key (array of
  {optionId,name,kind} — raw agent labels pass through) — additive;
  decision key may now be "always".
- RPC/HTTP/CLI decision validators accept "always" (CLI offline validator
  too). RPC resolve reply reason stays "answered".
- fakeagent: permission-mode params gain an options array incl.
  allow_always entry (verify no existing leg asserts absence).
- Tests: extend internal/acp session/relay tests + rpc/http/bus/daemon
  wiring tests (existing files only) + one e2e acp-wire scenario: human
  mode, resolve "always" -> fakeagent receives selected/allow_always
  optionId echo.

### Contract B — surfaces (leaf 02)

- TUI modal: third key `a` = always (y=allow, n=deny, esc unchanged);
  footer shows "y allow / a always / n deny / esc dismiss" ONLY when the
  head carries an allow_always-kind option (else current 3-key footer —
  graceful, agent-declared capability).
- GUI dialog: third button "always" rendered iff event.options includes
  an allow_always-kind entry; provider resolve call passes "always".
  Status badge unchanged. dart format + widget tests in existing families.
- Docs: workflows/acp.md surfaces table + escalation/decision text
  updates (relay "always"), reference rpc/http/cli decision values gain
  "always", features.md ACP row clause, parent relay master.md non-goal
  note marked SUPERSEDED (never deleted) pointing at this tree.

## Non-goals

- No persistent always-policy in meept (per decision 1).
- No agent-side scope naming UI (we echo ids, we do not localize).
- Model votes stay allow/deny only (decision 4).

## Leaves + waves

- 01 `01-core-wire.md` — Contract A. Branch: acp+bus+rpc+http+cli+e2e.
- 02 `02-surfaces.md` — Contract B. Branch: tui + flutter + docs. Depends
  01.

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| 01 | COMPLETE 2026-10-20 | 6bcd5cd6 | PermissionOption exported + parsePermissionOptions + resolveAlwaysOptionID (allow_always > allow&always > ""->allow-once chain) + alwaysOutcomeResult; Relay.Resolve accepts "always"; PendingPermission.Options cap 16 at register AND published snapshot; bus ACPPermissionEvent gains additive options key (mirrored struct, no bus->acp import); rpc/http/cli validators + 400 text + help gain always; fakeagent NEW permission-always mode (old mode params byte-identical — subprocess legs untouched); 8 acp + 1 bus + rpc + http + daemon + e2e acp-wire-05 HumanModeAlwaysEcho tests; 1 sanctioned pin edit: rpc/acp_test.go invalid-decision list dropped "ALWAYS" (superseded v1 pin, boundary still refuses unknown verbs); gates green x2; sibling WIP (chat.go, components.go, teardown, hooks) excluded |
| 02 | COMPLETE 2026-10-20 | da511d98 | TUI a/A key + conditional footer gated on allow_always option (mirrors resolveAlwaysOptionID chain) + options decode + toast handles always; GUI AcpPermissionOption/hasAlwaysOption + third button key acp-permission-always iff offered; SURFACE GAP CLOSED by leaf: GET /api/v1/acp/permissions was dropping options (GUI catch-up could never render the button) -> ACPPendingPermissionStatus.Options additive + passthrough test; docs x9 pages incl. SUPERSEDED append to relay master.md; tui 22s/rpc green, flutter 28 acp tests pass, dart format clean; sibling /dnd WIP (chat_input, slash_commands, chat_input_test, command_handler) excluded |
| Integration | COMPLETE 2026-10-20 | — | acp-wire e2e ok 27.6s; acp/bus/daemon/comm-http sweeps ok; graphs+reference-docs freshness at clean-HEAD worktree = ZERO diff; sibling-WIP files never staged |

Tree COMPLETE 2026-10-20 — 2/2 leaves + integration, 100%.

## Integration gates

builds; go test -p 2 acp/bus/rpc/comm/http/daemon/tui; e2e acp-wire x2;
flutter dialog/provider tests; dart format clean; docs freshness at clean
HEAD worktree; sibling-WIP exclusion rule (explicit pathspecs).
