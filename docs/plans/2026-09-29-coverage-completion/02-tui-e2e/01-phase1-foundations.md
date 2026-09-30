# Leaf 01 — TUI Phase 1: headless foundations + goldens + parity inventory

**Objective:** Extract the headless `App` constructor, build the golden
render harness, capture the first golden batch, and land the TUI/GUI
parity inventory test. Everything later phases need.

**Reference:** docs/workflows/tui-e2e-plan.md sections 2 (mechanics),
4 (approach a+b), 6 (parity). Existing proof: internal/tui/app_test.go:491
(tea.NewProgram with WithInput/WithOutput/WithContext over buffers).

**Files:**
- Create: `internal/tui/headless_test.go` — `newHeadlessApp(t, rpc, w, h)`
  helper: builds the real App with a stub/mock RPCClient, sends a fixed
  WindowSizeMsg(w,h) via the program, returns app + program + a Send
  helper. Extract from app_test.go:470-510 boilerplate.
- Create: `internal/tui/golden_test.go` — golden harness: render View()
  after a scripted msg sequence, compare against
  `internal/tui/testdata/golden/<name>.txt`; `-update-golden` flag
  rewrites. Hazard handling: avoid sending msgs that start tea.Tick
  timers (statusMessageTime); if unavoidable, strip ANSI cursor moves and
  time-dependent lines before compare (document the strip list).
- Create: first goldens — chat empty, chat loaded (one assistant msg),
  sessions view, tasks view, palette open, status bar.
- Create: `internal/tui/parity_test.go` — data-driven inventory:
  TUI palette command names (internal/tui/palette.go +
  internal/tui/components/palette/), DefaultKeyMap entries, status-bar
  elements compared against a checked-in
  `internal/tui/testdata/gui_inventory.json` (Flutter GUI side parsed
  from ui/flutter_ui source OR maintained as a shared manifest both
  surfaces update — pick the checked-in manifest; simpler, and the GUI
  has no Go test runner). Mismatch = fail with a diff of expected vs
  actual inventory.

**Invariants:**
- View() output for fixed input is deterministic (no wall-clock lines in
  the captured goldens; document any strip rules).
- All golden text is lowercase (AGENTS.md rule) — assert in the golden
  post-processor.

**Verify:**
```
go test ./internal/tui/ -run TestGolden -v          # goldens compare clean
go test ./internal/tui/ -run TestParity -v          # inventory matches
go test ./internal/tui/...                          # full package green
```

**Commit:** `test(tui): headless app helper, golden render suite, GUI parity inventory`

**Self-check:** `-update-golden` twice in a row produces zero diff
(deterministic).
