# Surfaces audit — API + TUI + Flutter GUI parity

- Scope: commit range 0a446a21..HEAD in /Users/caimlas/git/meept
- HEAD: 6b42213b fix(security,agent): restore voice_call permission rows; make rewake eviction test deterministic
- Timestamp: 2026-11-06T18:42:37Z
- Rules checked: TUI/GUI parity, lowercase UI text, WS filter (channel,session) scoping, session_id vs conversation_id, tableutil.Size for TUI tables
- Excluded (already closed): .hermes/audits/2026-10-30-bughunt-wave.md, PB-1/PB-2 (fixed 6b42213b)

## Findings

## F-1 [LOW] parity manifest not updated for the /intake GUI landing (violates gui_inventory.json's own same-commit rule)
- `internal/tui/testdata/gui_inventory.json` (palette_commands list) vs `ui/flutter_ui/lib/widgets/command_palette.dart:84-91`
- Evidence: command_palette.dart carries `label: 'intake status' ... route: '/intake'` (added 7b8c62fd) and `ui/flutter_ui/test/widgets/command_palette_test.dart:29` asserts it; but gui_inventory.json's palette_commands ends at `f find...` — no `intake` entry at all, and `deviations` has no `palette:intake*` key. The manifest's `_comment` states: "GUI sources of truth: ui/flutter_ui/lib/widgets/command_palette.dart ... When a surface changes legitimately, update BOTH the code and this manifest in the same commit."
- Why it's a bug: the manifest is the shared TUI/GUI contract consumed by `internal/tui/parity_test.go` (TestParityPaletteCommands) and the Dart palette test's "identical keys" comment; a GUI palette item that exists in code but not in the manifest is untracked parity drift — the exact class of gap the manifest exists to prevent. The TUI half (/intake slash, 9a19d0be) is also unlisted.
- Fix shape: add an `intake` palette entry (or a `deviations` row documenting slash-only-vs-palette) in gui_inventory.json in the same wave as any follow-up; alternatively extend the parity test to diff command_palette.dart labels against the manifest.

## F-2 [LOW] GUI intake report renders rows the TUI twin doesn't (claims to mirror it)
- `ui/flutter_ui/lib/features/intake/intake_status_view.dart:184-186` vs `internal/tui/intake_view.go:36-77`
- Evidence: the doc comment says "Row-by-row report mirroring renderIntakeStatus ... so the two surfaces agree", but (a) in the ENABLED state the GUI adds a `note` row when non-empty — and `internal/rpc/intake_status.go:88` guarantees note="intake enabled" is always non-empty when enabled — while the TUI's enabled branch never renders Note at all; (b) empty-string fields render as `'-'` in the GUI (`_dash`, intake_status_view.dart:191) but as blank in the TUI (`%s` with no fallback, intake_view.go:41).
- Why it's a bug: parity rule in AGENTS.md — "when a feature is added or changed in one surface, the other surface gets the same capability", and these two files explicitly claim row-for-row agreement. Divergent rendering of the same frozen contract misleads users comparing surfaces and defeats the manifest-based parity tests' intent.
- Fix shape: drop the note row in the GUI enabled branch (or add it to the TUI), and pick one empty-value convention (suggest '-' in both — TUI would need a small helper).

## Audited-clean

- **intake.status RPC (internal/rpc/intake_status.go)** — nil-handler disabled envelope answers, never method-not-found; setters nil-guarded per repo convention; no token material; field contract identical across TUI struct (`internal/tui/intake_view.go:9-20`), Dart model (`intake_models.dart`), CLI (`cmd/meept/intake.go`). PollerScheduled forced false when disabled — matches doc.
- **HTTP bridge (internal/comm/http/intake_handlers.go)** — 503 when rpcCall seam absent, lowercased error text (UI-lowercase convention honored), registered behind auth middleware like other /api/v1 routes; server.go route registration present (`GET /api/v1/intake/status`).
- **GUI intake wiring** — provider goes through `sdkClientProvider` (persisted host/key client) as its comment requires; `getIntakeStatus()` in sdk_client.dart:2062 matches the route; router + slash_commands + command_palette all carry /intake (feature-level parity with the TUI's /intake is present; only the rendering deviations F-2 and the manifest gap F-1 found).
- **TUI /intake command path** — routed in command_handler.go:194 (`case "intake"`), output fully lowercase, error results marked IsError:false for renderable messages, not-connected guard present.
- **Activity view table sizing (internal/tui/models/activity.go)** — created via `table.New` and re-sized through `tableutil.Size(&m.table, width, max(height-6,3))` (line 235) + `tableutil.SetRows` (line 525); `activityColumns` clamps the summary column with `max(width-72, 8)` — width-0 viewport rule satisfied. App wires SetSize on WindowSizeMsg and View/Update/initCurrentView cases; tail poller is torn down at the single choke point (app.go:2722-2730 HideActivityView).
- **Activity GUI parity** — TUI activity view (palette `a`, gui_inventory `gui:true`) has the Flutter twin: `/activity` route, palette item, activity_list/recent/findings/drift panels polling the same 4 HTTP endpoints registered in server.go (`GET /api/v1/activity*`, `/api/v1/agents/{id}/review`); sdk_client carries getActivity/Recent/Overview/Findings matching handler param names (employee_id, severity, resolved...).
- **Modal negative-width fixes (5a2f050e)** — `strings.Repeat("─", max(m.width-4,1))` in acp_permission.go + pending_changes.go (x2), and chat.go updateViewport `max(m.width-6,1)` — closes the pre-first-WindowsizeMsg panic family; no remaining bare `Repeat(..., m.width-N)` found in the touched files.
- **WS filter / session vs conversation ids** — no new WS-relay or event-routing code in this range touches `ShouldSend`/`wsEventChannel` semantics (server.go diff adds only HTTP route wiring + auth exemptions); intake/activity RPC and HTTP surfaces are session-less read snapshots, so the (channel,session) rule and dual-id rule are not engaged. Groups RPC (internal/rpc/groups.go) carries no session/conversation id handling at all (membership mutation via explicit ids).
- **Auth exemptions (server.go middleware)** — slack interactions + voice routes are short-circuited BEFORE the auth chain but their comments claim rate-limiting "still applies (the handlers run under the ordinary chain)"; reading the code, these branches call the handler directly from the outer wrapper — the rate limiter lives inside the wrapped `handler` chain, so the exemption path bypasses it. FLAGGED AS OBSERVATION, not filed as a finding: signature verification is enforced inside the handlers (fail-close 503 without key), matching the pairing precedent which is also rate-limit-exempt and documented ("no bearer auth, no rate limit"). The slack/voice comments saying "Rate limiting still applies" are inaccurate against the pairing pattern — a doc/comment mismatch worth a follow-up read of where the limiter is installed.
- **cmd/meept (doctor/groups additions)** — build + vet clean (`go build ./internal/tui/... ./internal/comm/... ./internal/rpc/... ./cmd/meept/` rc=0; `go vet` rc=0); report-only doctor checks, no UI-text uppercase violations in scope files.
- **Dart lowercase UI text sweep** — grep of all added `Text(` / `label:` / `description:` literals in the 0a446a21..HEAD ui/flutter_ui diff found zero capitalized UI strings.

## Wave closeout (parent, 2026-11-06)
- F-1 CLOSED: gui_inventory.json carries the intake palette row AND the TUI
  now has a live 'intake status' palette item (modal.go row + app.go 'i'
  case dispatching the existing /intake slash path). TestParityPalette +
  full internal/tui ok; golden regenerated; command_palette_test.dart ok.
- F-2 CLOSED: GUI enabled-note row dropped (TUI parity); the remaining
  empty-field '-' vs blank rendering is documented as deviation
  'intake:view-empty-dash'. flutter analyze clean.
