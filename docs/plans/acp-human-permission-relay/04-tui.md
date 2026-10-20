# Leaf 04 — TUI relay surface (Contract D)

Parent: `master.md` Contract D. Depends: leaf 03 (topics + RPC live).
Branch: tui.

## Precedents to read first

- internal/tui/tools.go — ConfirmationInterceptor, ConfirmationModel,
  runModal machinery (used today for destructive tool confirmations).
- internal/tui/confirmation_test.go — modal test shape.
- How the TUI consumes WS/bus events: grep existing agent_progress/
  escalation handling in internal/tui (the TUI talks to the daemon over
  the same WS — find its event pump, likely in handlers/ or app.go).

## Tasks

1. RED→GREEN in EXISTING tui test files: on WS frame
   `acp.permission.requested` (agent_progress class) the TUI raises a
   permission modal showing agent id + summary; `a` (allow) / `d` (deny) /
   Esc (leave pending → timeout later) call `acp.permissions.resolve`;
   modal auto-dismisses on `acp.permission.resolved` frame (another client
   or timeout answered it) with a toast line.
2. Multiple pending requests: queue — show first, resolve walks the queue;
   status line shows `acp perms: N pending` (status-bar precedent:
   references the acp:N provider — grep `acp:` in internal/tui).
3. Text lowercase; keybindings: reuse modal accept/decline keys from the
   confirmation precedent — document actual keys in docs/workflows/tui.md.
   If a NEW global keybinding is added (not recommended), update palette
   manifest gui_inventory.json + parity_test.go.

## Verify

`go test -p 2 ./internal/tui/ -count=1` green; live: scratch daemon
(+ fakeagent) with the real TUI against it — screenshot-free proof: tmux/
pipe the TUI? If impractical, prove via the WS event → modal model
transition test (programmatic Update() with the frame) and note it.
Do NOT commit.
