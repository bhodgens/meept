# Leaf 02 — acp relay registry + human lane (Contract B)

Parent: `master.md` Contract B. Depends: leaf 01 committed (read schema.go
fields on disk). Branch: acp.

## Tasks

1. RED tests — extend `internal/acp/session_test.go` + `manager_test.go`
   only (no new *_test.go for the new relay.go? the repo rule is NO NEW
   UNIT TEST FILES for new FEATURES — extend existing files; relay tests
   go in session_test.go/manager_test.go):
   - human mode + injected fake notifier + relay: request →
     `permission_requested` event + notifier called → Resolve(id,"allow")
     → wire reply selected/allow-once + `permission_resolved` event.
   - Resolve "deny" → cancelled.
   - timeout (config relayTimeout small, e.g. 50ms in test) with no resolve
     → cancelled + `permission_resolved` text "timeout"; WARN log.
   - notifier returns false (no listener) → STILL waits until timeout then
     cancels (reconnect contract).
   - nil relay/notifier with mode human → cancel immediately + WARN (fail
     closed; pin as deliberate like parent tree's no_decisioner test).
   - decision-lane escalation: mode decision + escalate=true + stub
     decider answers "deny" → relay wait runs (allow via relay proceeds;
     event text `escalated_human_allow`); escalate=true + decider allow →
     NO relay wait.
   - escalate=false decision behavior byte-identical to parent tree pins.
   - relay Resolve on unknown id → ErrRelayNotFound; double resolve →
     ErrRelayAnswered.
2. Impl `internal/acp/relay.go` per Contract B (PendingPermission, Relay,
   PermissionNotifier, sentinels via errors.New — sentinel style per
   manager.go; pkg/id.Generate for IDs).
3. `session.go`: SessionConfig += Relay *Relay, Notifier PermissionNotifier,
   RelayTimeout time.Duration, DecisionEscalateToHuman bool;
   handlePermission dispatch: mode "human" → handlePermissionHuman;
   decision lane non-allow + escalate → escalation wait (shared helper in
   relay.go so both paths use ONE code); events per master.
4. `manager.go`: relay/notifier/timeout/escalate fields + nil-safe
   `SetPermissionRelay(...)` (single setter taking relay+notifier+timeout
   OR separate setters — pick ONE shape, keep HasDecisionClient pattern);
   stamp into SessionConfig for mode human (always) and decision+escalate.
   Add `PendingPermissions() []PendingPermission` on Manager (delegating to
   relay; nil-safe empty) — leaf 03's RPC surface needs it.
5. Extend `internal/acp/testdata/fakeagent/main.go` ONLY if needed to fire
   requestPermission on a SECOND prompt turn (timeout test wants two
   requests over one session; if the io.Pipe harness can re-notify without
   the subprocess, prefer harness-only — read existing tests first).
6. e2e acp-wire suite: allow + deny + timeout scenarios over the subprocess
   path (extend existing decision-mode test style).

## Verify

`GOTOOLCHAIN=go1.26.5 go test -p 2 ./internal/acp/ -count=1`;
`go test -p 2 -tags e2e ./e2e/suites/acp-wire/ -count=1`; mutexio: never
hold s.mu or relay mutex across notifier/transport I/O — relay registry
locks guard map ops ONLY.
Do NOT commit.
