# TUI Full e2e Coverage Plan

`internal/tui` is the largest untested-at-depth package in the repo (~43.5k
lines across ~20k lines of code plus ~20k of unit tests) and maps to the
thin `smoke` suite only in `e2e/manifest.json`. It currently has **zero
behavioral e2e coverage**: nothing drives the real `App` model through a
user flow against a real daemon. This document surveys the surfaces, the
existing coverage, the feasible testing mechanics, and lays out a phased
plan to close the gap.

Conventions referenced throughout: root `AGENTS.md` → "UI Conventions"
(lowercase text, bubblezone positioning, clickable context switching, TUI
table sizing via `tableutil.Size`/`tableutil.SetRows`, and the **TUI/GUI
parity rule** — status bar elements, command palette items, keyboard
shortcuts, and session/agent/tab semantics must match `ui/flutter_ui`,
with deviations documented and justified). Also
`docs/workflows/tui.md` (TUI architecture) and `docs/workflows/e2e-testing.md`
(tiers, manifest format, coverage policy).

## 1. Current state

### 1.1 What exists today

| Layer | Reality |
|---|---|
| Unit tests | ~40 `*_test.go` files across `internal/tui` and subpackages. They drive sub-models directly with synthetic messages and assert `View()` output or state (e.g. `internal/tui/models/table_render_regression_test.go:21` constructs `NewTasksModel(mock)`, sends `TasksUpdateMsg`/`tea.KeyPressMsg`, asserts rendered rows). Logic is well covered; the interactive loop is not. |
| Program-level tests | Exactly one file: `internal/tui/app_test.go:491` and `:553` run the real `App` under `tea.NewProgram` with `tea.WithInput(&in)`, `tea.WithOutput(&buf)`, `tea.WithContext(ctx)` — no TTY. `TestApp_Program_CommandPalette` (`internal/tui/app_test.go:553`) sends `Ctrl+X` / `Escape` via `p.Send` and asserts the final model state (palette closed). This proves headless program driving works in this repo, on bubbletea v2. |
| e2e tier | `e2e/manifest.json` maps `"internal/tui/"` → `["smoke"]`, explicitly listed among the "deliberately thin coverage" packages. No scenario under `e2e/suites/` exercises the TUI. |
| Harness | `e2e/harness/` provides `Stack` (fake LLM + scratch daemon + sandboxed `HOME`/`MEEPT_HOME`, `e2e/harness/daemon.go:41`) with `Stack.SocketPath`, scripted responses (`e2e/harness/fakellm.go:195` `Script`, `TextResponse`, `QuotaResponse`, SSE streaming via `writeSSE`, `e2e/harness/fakellm.go:778`), and RPC/WS/SSE clients (`e2e/harness/clients.go:299` `DialRPC`, `:48` `DialWS`). |

### 1.2 Why nothing breaks today (and why that is dangerous)

The TUI's failure modes are exactly the ones unit tests can't see: a key
binding shadowed by modal focus order, a table sized on one axis only (the
zero-width-viewport bug class that `tableutil` exists to prevent — see the
regression note in `internal/tui/models/table_render_regression_test.go:11`),
a streaming update mutating chat state while the user is in a modal, or a
palette item drifting from its GUI twin. None of these produce a failing
unit test; all of them produce a broken operator experience.

## 2. Surface survey (`internal/tui`)

Major surfaces, with the entry points an automated test would drive:

| Surface | Code | Test entry points |
|---|---|---|
| Main loop / view router | `internal/tui/app.go:710` `Update`, `:2529` `View` — routes `tea.WindowSizeMsg` (`:714`), `tea.KeyPressMsg` (`:759`), dispatches per `currentView` (`ViewChat/Sessions/Tasks/Queue/Memory/Plans/Search/Agents`) | Send `tea.WindowSizeMsg{Width,H}` then key messages; assert `View()` string + final model state |
| Header / status bar / tabs | `internal/tui/app.go:2649` `renderHeader`, `:2757` `renderTabs`, `:2797` `renderStatusBar`, `:2975` `renderProjectIndicator` | `View()` output after fixed `WindowSizeMsg`; these are the parity-critical renders |
| Command palette | `internal/tui/palette.go`, `internal/tui/components/palette/`, opened via `ModalCommandPalette` (`app.go:2617` `renderModalOverlay`) | `Ctrl+X` (per `DefaultKeyMap`, `app.go:214`), type to filter, Enter to run |
| Session tabs / picker | `internal/tui/app.go:2408` `switchToSessionByID`, `:2472` `deleteSession`, `:2484` `renameSession`; `internal/tui/models/sessions.go` | Key messages through `handleModalKey` (`app.go:2204`); session RPC via mockable client |
| Chat view + streaming | `internal/tui/models/chat.go` (`NewChatModel` `:324`), turn lifecycle `internal/tui/models/chat_turn.go`, `internal/tui/models/turn.go`; progress rendering `ProgressState.Render` (`chat.go:716`) | Feed progress/chat msgs from a scripted daemon; assert rendered transcript |
| Agent views | `internal/tui/agents_panel.go`, `internal/tui/models/tasks.go`, queue/memory/plans models under `internal/tui/models/` | Same synthetic-msg pattern already used by their unit tests |
| Modals | `internal/tui/modals/` (pending changes, project prompt), `confirmation.go`, `users_modal.go`, `modal.go` | Drive via keys; assert overlay render through `renderModalOverlay` (`app.go:2614`) |
| Keybindings | `DefaultKeyMap` (`app.go:214`), `internal/tui/client.json5` overrides (`internal/tui/config.go` persistence tests exist) | Bindings are data — enumerate and assert in a parity test |
| Tables | `internal/tui/tableutil/` — `Size`/`SetRows` normalization | Covered by unit tests; e2e only needs to catch sizing regressions in situ |
| Sidebar / vim mode / viz / prompts | `internal/tui/sidebar.go`, `vim/`, `viz/`, `prompts/`, `render/` | Unit-covered; defer behavioral e2e (Phase 3) |

Daemon boundary: the TUI talks to the daemon exclusively through
`RPCClient` (`internal/tui/rpc.go:26`), and the chat model depends on the
`models.RPCClient` **interface** (`internal/tui/models/chat.go:252`), which
is why every unit test can inject mocks. The real program entry is
`cmd/meept/chat.go:429` (`tea.NewProgram(app)`).

## 3. Feasibility of headless testing (verified against the code)

The central question: **can the TUI be driven headlessly — synthetic keys,
no terminal — and does `View()` render deterministically?** Answers:

1. **Yes, and it is already proven in-repo.** `internal/tui/app_test.go:491`
   runs the full `App` under `tea.NewProgram(tea.WithInput, tea.WithOutput,
   tea.WithContext)` with `bytes.Buffer` I/O — no TTY, no pty. The test
   comments document bubbletea v2's ordering guarantee: `p.Send` blocks on
   an unbuffered channel until the single-threaded event loop receives the
   message, so sequential sends from one goroutine are ordered and each
   `Update` completes before the next key is handled
   (`internal/tui/app_test.go:546-551`). That is exactly the teatest-style
   choreography without the dependency.
2. **`Update` handles synthetic messages directly.** `tea.WindowSizeMsg`
   (`internal/tui/app.go:714`) and `tea.KeyPressMsg` (`:759`) are ordinary
   cases in the switch; every unit test in `internal/tui/models/` already
   drives sub-models this way (`table_render_regression_test.go:27,33`).
3. **`View()` is deterministic given fixed width/height — with two known
   hazards.** `View()` (`internal/tui/app.go:2529`) is a pure function of
   model state: it returns `"loading..."` at zero size, composes header +
   main view + status bar + notifications from state, and returns
   `tea.NewView(b.String())` with `AltScreen` set. Hazards for golden
   tests:
   - Status messages expire via wall-clock comparisons on
     `statusMessageTime` (set with `time.Now()` throughout `Update`,
     e.g. `internal/tui/app.go:789,807`) and `tea.Tick` auto-clears
     (`app.go:790,807,871`). Golden assertions must either avoid the
     transient status-message line or deliver the clear tick before
     snapshotting.
   - `App.Init` fires RPC-loading cmds (`loadSession`, `fetchTemplateNames`,
     `fetchCurrentProject` — `app.go:553-560`); against a scripted harness
     these resolve deterministically, against a dead socket they surface as
     error views. Tests must run against a scripted client or daemon, not a
     closed socket, whenever the assertion depends on loaded data.
4. **teatest (`github.com/charmbracelet/x/exp/teatest/v2`) is NOT vendored**
   (`go.mod` carries only `charm.land/bubbletea/v2 v2.0.6`,
   `charm.land/bubbles/v2 v2.1.0`, lipgloss). Given (1), adding it is
   optional: its value-add is output-buffer polling and animation draining,
   which the existing `WithInput/WithOutput/Send` pattern already covers.
   Recommendation: do not add the dependency; extend the in-repo pattern.
   Revisit only if flaky-render debugging demands teatest's final-frame
   capture helpers.
5. **Real-daemon e2e is a composition problem, not a new capability.** The
   `App` is constructed with a socket path (`NewApp`, `internal/tui/app.go:248`);
   the e2e harness already yields a scratch daemon with a known socket
   (`Stack.SocketPath`) and a fake LLM with scripted/SSE responses
   (`e2e/harness/fakellm.go`). A TUI e2e test = `NewApp(stack.SocketPath, tmpdir)`
   + `tea.NewProgram` headless + `p.Send` keys + assert on `View()` output
   and final model state. No product code changes required.

**What can never be automated (and what we do instead):**

- True terminal fidelity: alt-screen repaint, mouse event delivery, color
  profile negotiation, and resize-triggered relayout under a real
  terminal emulator. Mitigation: the headless tests fix `WindowSizeMsg`
  explicitly, and `agent-tui ./bin/meept chat` (already referenced in
  AGENTS.md) stays the manual smoke path.
- Visual/layout judgment: whether a wrapped message *looks* right, sidebar
  join aesthetics. Golden files catch content and structure, not taste.
- Interactive STT/TTS paths (`chat.go:460,492`) and clipboard (`doCopy`,
  `app.go:3037`): OS-integration surfaces. Assert state transitions
  (toggle flipped, command issued), not audio/clipboard effects.
- Cross-surface human judgment of parity: automated tests compare
  inventories of commands/keys/status elements; whether a deviation is
  *justified* is a review decision (see §7).

## 4. Chosen testing approach

Three complementary layers, all TTY-free:

**(a) Component-level headless interaction tests** — `internal/tui/**`
(unit tier, `go test`). Drive `App` (or a sub-model) with synthetic
`WindowSizeMsg`/`KeyPressMsg`/domain msgs using the proven
`app_test.go` pattern where a full program loop matters, direct `Update`
calls where it doesn't. Assert final model state and `View()` substrings.
Home for: keyboard flows, modal open/close/focus, view switching, keymap
overrides from `client.json5`.

**(b) Golden-file view-render suite** — `internal/tui/golden/` (or
`render/`): for each `currentView` and each modal, render at fixed sizes
(80×24, 120×40, narrow 50×20) against a stubbed RPC client (the
`models.RPCClient` interface at `chat.go:252` makes this trivial) and
compare against committed `.golden` files (`go test -update` to regenerate,
same convention as `make graphs-check` staleness philosophy). Determinism
rules from §3.3 apply: no transient status lines in goldens. Home for:
layout regressions, table sizing on both axes, lowercase-text enforcement
(scan rendered output for uppercase — automates the AGENTS.md UI rule).

**(c) Thin behavioral e2e suite against the hermetic daemon** —
`e2e/suites/tui-flows/` (`//go:build e2e`), manifest-registered, running
the real `App` model against `harness.Stack` with a fake LLM. Top operator
flows only (see Phase 2). This is the suite that replaces `internal/tui/`'s
thin `smoke` mapping in `path_map`.

The split respects the repo's e2e policy (AGENTS.md "e2e Testing Policy"):
(a) and (b) are test-infrastructure for an existing package (its unit tier
already exists and may be extended — this is not a "new feature"), (c) is
the new-feature-tier suite that satisfies the "every internal/ package maps
to at least one suite" rule behaviorally.

## 5. Phased rollout

**Phase 1 — foundations (land first).**
- Extract the ad-hoc `App`-construction boilerplate from
  `app_test.go:470-510` into a test helper (e.g. `newHeadlessApp(t, rpc,
  width, height)`) so all three layers share one constructor.
- Golden harness (b) with the first batch of goldens: chat (empty + loaded),
  sessions, tasks, palette open, status bar. Lowercase-text check as a
  golden post-processor.
- Parity inventory test (§7) — pure data, cheap, immediately useful.

**Phase 2 — core flows (the behavioral e2e suite).**
`e2e/suites/tui-flows/`, hermetic, against `harness.Stack`:
1. `tui-palette-01` — open palette (Ctrl+X), filter, execute a command;
   assert the RPC the command issues and the resulting view change.
2. `tui-session-switch-01` — switch sessions via picker/keys; assert
   `switchToSessionByID` effect and transcript reload in `View()`.
3. `tui-agent-tab-01` — switch to agents/tasks view; table renders rows
   (the zero-width-viewport regression class, in situ).
4. `tui-quota-01` — script `QuotaResponse` (`e2e/harness/fakellm.go:143`)
   from the fake LLM; assert quota status surfaces in the status bar
   (`internal/tui/quota_status.go`) instead of an error.
5. `tui-stream-01` — scripted SSE streaming turn; assert incremental
   progress rendering and final transcript content in chat view.
Flip `e2e/manifest.json`: `"internal/tui/"` → `["tui-flows"]` (keep
`smoke` too if smoke still touches the package).

**Phase 3 — breadth (defer until 1-2 are stable).**
- Remaining views (queue, memory, plans, search) as goldens + one flow each.
- Modals: confirmation, rename, pending changes, project prompt — key-flow
  tests in layer (a).
- Keybinding overrides via `client.json5` fixtures (`internal/tui/config.go`).
- Sidebar, vim mode, viz/prompts: goldens only; behavioral flows stay manual.

Each phase lands with its manifest/scenario entries in the same commit
(pre-commit check 18/18 requirement).

## 6. CI integration

| Layer | Runs where | Gate |
|---|---|---|
| (a) interaction tests | `go test -p 2 ./internal/tui/...` — part of `make test`, so every CI run and pre-commit | Standard unit gate |
| (b) goldens | Same package, same run; stale-golden failure message points at `-update` | Standard unit gate; golden diffs reviewed in PRs like generated artifacts |
| (c) `tui-flows` | `make e2e-fast` (CI job `e2e-fast`) and pre-commit `pre-commit-e2e` via `scripts/e2e-affected.sh` — any diff under `internal/tui/` maps to the suite in `path_map` | Blocking; emergency bypass stays `MEEPT_SKIP_E2E=1` |

Daemon-slot discipline: `tui-flows` goes through `harness.Stack`, which
already caps concurrent daemons (`e2e/harness/daemon.go:26` `daemonSlots`)
— consistent with the macOS `-p 2` rule. Target: seconds per test, keeping
the fast tier's contract.

## 7. TUI/GUI parity enforcement

The AGENTS.md parity rule is enforced by a **parity inventory test**
(lands in Phase 1, `internal/tui/parity_test.go`):

- **Commands**: the TUI palette item list (`internal/tui/palette.go`,
  `internal/tui/components/palette/`) is compared against the Flutter
  command palette inventory. The GUI side is parsed from the Dart source
  (palette/command registry file, path fixed in the test) or from a
  checked-in manifest both surfaces update — pick whichever survives GUI
  refactors; parsing is preferred so the GUI cannot silently drift.
- **Keyboard shortcuts**: `DefaultKeyMap` (`internal/tui/app.go:214`) vs
  the GUI's shortcut map; the AGENTS.md rule (identical keys across
  surfaces, e.g. `Ctrl+V` for verbosity everywhere) is asserted literally.
- **Status bar elements**: the set of elements `renderStatusBar`
  (`app.go:2797`) can emit vs the GUI's status bar widget inventory.
- **Session/agent/tab semantics**: the verbs TUI offers per session
  (archive vs delete, rename, switch) must exist in the GUI's session
  action set, and vice versa.

Any mismatch fails the test with a pointer to the AGENTS.md parity rule;
an intentional deviation lands as a documented justification next to the
assertion (mirroring the "document surface-specific deviations" clause).
This test is data-driven and cheap — it runs in the unit gate, not e2e,
so parity breaks surface on every commit rather than only when a suite is
affected.

## 8. Direct answers

- **Automate first**: the parity inventory test and goldens for the status
  bar/palette/chat (Phase 1) — highest defect-catch per line of test code,
  zero harness dependencies. Then the five `tui-flows` scenarios (Phase 2),
  because they are the first tests in the repo that can catch "the feature
  works at the RPC layer but is broken in the UI".
- **Never automatable**: real-terminal repaint/resize fidelity, visual
  quality of layout, OS audio/clipboard effects, and the human judgment
  call on whether a TUI/GUI deviation is justified. Covered by manual
  `agent-tui` smoke, golden review, and the parity-test justification
  comment respectively.
- **CI gate**: `path_map["internal/tui/"]` → `tui-flows` in
  `e2e/manifest.json`, so `pre-commit-e2e` and the CI `e2e-fast` job block
  any `internal/tui` change that breaks a top operator flow; goldens and
  parity ride the standard unit gate.
