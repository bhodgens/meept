# Leaf 02 — TUI Phase 2: tui-flows hermetic e2e suite

**Objective:** Drive the REAL TUI App model against the hermetic
harness daemon (e2e/harness Stack + FakeLLM) through the five top
operator flows; flip the manifest so internal/tui maps to tui-flows.

**Files:**
- Create: `e2e/suites/tui-flows/tui_flows_test.go` (e2e build tag)
- Modify: `e2e/manifest.json` — new suite entry + path_map
  `internal/tui/` → `['tui-flows']` (replacing the thin smoke mapping)

**Mechanics:** compose `harness.Stack` (fake LLM + scratch daemon,
SocketPath) with the Phase 1 `newHeadlessApp` pointed at
`stack.SocketPath` — no real terminal. The fake LLM scripts chat
responses (see harness/fakellm.go Script/TextResponse, SSE via
writeSSE). Follow e2e/suites/naive-user-chat/ for Stack setup patterns.

**Flows (each a test):**
1. `tui-palette-01` — send Ctrl+X, assert palette view renders; filter
   to a command; execute; assert the RPC the command issues (capture via
   a harness RPC spy or the fake LLM request log) and the view change.
2. `tui-session-switch-01` — two sessions exist; switch via keys/picker;
   assert switchToSessionByID effect fires and View() shows the other
   transcript.
3. `tui-agent-tab-01` — switch to the agents/tasks view; assert the
   table renders rows (non-empty, tableutil-sized).
4. `tui-quota-01` — fake LLM returns a QuotaResponse; assert the quota
   display state (agent.quota_wait path) renders the wait indicator.
5. `tui-streaming-01` — scripted SSE streaming response; assert the chat
   view accumulates streamed deltas and finalizes.

**Invariants:**
- No real terminal; all input via program.Send (bubbletea v2 ordered
  delivery per app_test.go:491 pattern).
- Daemon teardown: defer stack.Stop(); TUI program cancellation before
  daemon stop (order matters — see tui-e2e-plan.md hazards).
- No wall-clock assertions.

**Verify:**
```
make e2e-fast-area AREA=tui-flows      # green
go build ./...
python3 -c "import json; json.load(open('e2e/manifest.json'))"
```

**Commit:** `test(e2e): tui-flows suite — real TUI model against hermetic daemon (5 flows)`

**Self-check:** run the suite twice; second run must also pass (no
state leakage via MEEPT_HOME — Stack sandboxes it).
