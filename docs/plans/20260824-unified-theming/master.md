---
name: master.md
description: Root orchestrator for the unified theming plan (issue #24)
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [theming, tokens, flutter, tui, generator, issue-24]
---

# Unified Theming Plan — Root Orchestrator

Source: GitHub issue #24. One shared color-token set drives both frontends.
Users switch themes per client from config; shipped variants actually work.

## Verified ground truth (2026-08-24 recon — overrides issue text where they differ)

| Fact | Value |
|------|-------|
| Flutter `CyberpunkColors.*` refs | 1,076 across 49 files (18 distinct members) |
| Flutter const-context refs | ~27 lines use `CyberpunkColors` inside `const` expressions |
| Flutter raw `Color(0x…)` outside lib/theme | exactly 1 (`find_bar.dart:73`) |
| TUI `lipgloss.Color("#…")` literals outside styles.go | 378 across 17 files |
| TUI pkg-level style vars frozen at init | selfimprove.go si* styles, models/sessions.go archivedRowStyle |
| TUI hex-string consts | `constants.go` + `models/constants.go`: ColorAmber/Green/Red/Gray |
| viz package duplicates palette | `internal/tui/viz/colors.go` (8 lipgloss.Color vars) |
| styles.go var block | 10 `Color*` lipgloss.Color vars + `DefaultStyles()` (354 lines) |
| DefaultStyles() callers | app.go:245 only (+ bubbles table.DefaultStyles which is unrelated) |
| GUI theme storage | SharedPreferences key `theme` via StorageService.getTheme()/setTheme() |
| GUI MaterialApp | main.dart:132 `theme: CyberpunkTheme.darkTheme`, StatelessWidget |
| GUI settings dropdown | settings_panel.dart:764 — single 'cyberpunk' entry, comment explains why |
| RenderingPrefs pattern to copy | lib/providers/rendering_prefs_provider.dart (StateNotifier + load()) |
| TUI config | ClientConfig.Rendering (markdown/syntax_highlighting/theme=monokai/word_wrap…) in internal/tui/config.go |
| Client config PATCH API | PATCH /api/v1/config/client exists (RFC 7396 merge) — GUI can persist theme there too |
| Go JSON5 parsing dep | github.com/tailscale/hujson (used by internal/tui/config.go) |
| chroma syntax theme | rendering.theme ("monokai") is the *syntax highlighter* theme — SEPARATE from UI palette |

Key architectural decision (differs from issue sketch): **no code generation**.
A hand-written `AppPalette`/`Palette` layer reading a checked-in
`theme/tokens.json5` gives runtime selection with zero build-step risk.
Generators add build_runner/chroma wiring for no benefit over embedding the
same file both sides read at runtime.

## Interface contracts (frozen)

1. `theme/tokens.json5` — map of variant → role → hex string. Roles frozen list
   (see leaf W0). Both consumers parse this exact file (Flutter: asset bundle;
   Go: go:embed).
2. Flutter: `lib/theme/app_palette.dart` defines `class AppPalette` (final
   fields per role) and `const palettes = <String, AppPalette>{…}` parsed from
   the embedded token file at load. `lib/theme/palette_provider.dart` exposes
   Riverpod `paletteProvider` + `themeNameProvider`.
   `CyberpunkColors` keeps its exact member names but becomes non-const
   getters forwarding to the active palette. `CyberpunkTheme.darkTheme` builds
   from the active palette.
3. Go: `internal/tui/palette.go` defines `type Palette struct`, `Current()`,
   `SetPalette(name string) error`, `Names() []string`. `styles.go` var block
   becomes `var colorPrimary = …` derived after SetPalette; exported aliases
   preserved so 403 existing call-sites compile untouched.
4. Config keys: GUI reads/writes client config `rendering.ui_theme` via
   PATCH /api/v1/config/client (falls back to local SharedPreferences key
   `ui_theme` offline). TUI adds `RenderingConfig.UITheme string
   json:"ui_theme"` default "cyberpunk". (Existing `rendering.theme` stays =
   chroma syntax theme.)

## Child index

| Doc | Type | Depends on | Scope |
|-----|------|-----------|-------|
| W0-tokens-and-generator.md | wave0 (in-session) | — | tokens.json5, role census, consistency test skeleton |
| L1-flutter-palette.md | leaf | W0 | AppPalette + provider + CyberpunkColors forwarding + const fixes + find_bar literal |
| L2-tui-runtime.md | leaf | W0 | Go Palette type + styles derivation + config key + NewApp wiring + viz/colors.go |
| L3-flutter-settings.md | leaf | L1 | dropdown entries midnight+solarized, live swap test, persistence |
| L4-tui-stragglers.md | leaf | L2 | migrate 17 files' raw literals to palette; unfreeze init-time style vars |
| L5-consistency-docs.md | leaf | W0 | cross-surface parity test + docs/configuration updates + AGENTS.md |

Dispatch order: W0 → {L1 ∥ L2} → {L3 ∥ L4} → L5 → gates.

File ownership (no overlap):
- L1 owns ui/flutter_ui/lib/theme/** except nothing in L2/L4/L5 scope.
- L2 owns internal/tui/{palette.go,styles.go,config.go,app.go,viz/colors.go}.
- L3 owns settings_panel.dart, providers, its tests.
- L4 owns the 17 straggler files listed inside it.
- L5 owns docs/, tools/, AGENTS.md, and the cross-parity test file.

## Conventions for all dispatches

- All UI text lowercase (repo rule).
- No `git add -A`; orchestrator commits explicit paths only.
- Go: handle errors, no `_ = err`. Two-value type asserts on map[string]any.
- Flutter: no top-level dart:io in shared code; kIsWeb guards where needed.
- Do not rename public identifiers beyond the contract above.
- Non-goal (from issue): light themes; TUI hot-reload of theme (restart-applied OK).

## Coding Conventions

- Go: in-package tests, table-driven where natural, errors wrapped with
  %w, no panics in libs; run `gofmt` before reporting.
- Dart: follow `flutter analyze` clean as the gate; no `print`, use the
  app logger; no top-level `dart:io` in shared code (kIsWeb guards).
- Naming: match each surface's existing idiom (Go exported PascalCase;
  Dart lowerCamel); no cross-surface renames beyond the contracts.
- Comments explain WHY; no TODO/placeholder/debug artifacts in landed work.

## Dispatch Protocol

> This tree is COMPLETE (all items COMPLETE in Completion tracking, below).
> The protocol is retained as the historical record; no further dispatches.

1. Read this master + the assigned leaf + the frozen interface contracts.
2. Dispatch the leaf via delegate_task with: full leaf text, contracts,
   conventions, "Do NOT commit. Do NOT run git add. Write code, run tests,
   report results only."
3. Review in-session (main model); re-dispatch with feedback on gaps
   (max 3 cycles); commit explicit paths on pass; update tracking.

## Review Checklist

- [ ] Leaf scope implemented exactly; files owned per the ownership table
- [ ] Frozen interface contracts (above) satisfied — names, paths, keys
- [ ] Tests present and passing (`go test ./theme/... ./internal/tui/...`,
      `flutter analyze && flutter test`)
- [ ] All UI text lowercase; no new deps; no public renames beyond contract
- [ ] No debug artifacts, no line-number corruption

## Integration Test Plan

1. `go build ./...` and full `go test ./...` green (gates row: COMPLETE).
2. `cd ui/flutter_ui && flutter analyze && flutter test` (322 pass at
   completion).
3. Cross-surface parity: switch rendering.ui_theme per client (TUI + GUI);
   both surfaces render identical palettes; restart-applied on TUI.
4. Stray-literal guard: no raw `Color(0x…)` outside lib/theme; no
   `lipgloss.Color("#…")` literals outside styles.go (L4 guard test).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| W0 tokens foundation | COMPLETE | 1 | see historical tracking below |
| L1 flutter palette layer | COMPLETE | 1 | commit 2d6124d1 |
| L2 tui runtime palette | COMPLETE | 1 | commit 660227b4 |
| L3 flutter settings + tests | COMPLETE | 1 | commit 78db4f98 |
| L4 tui stragglers | COMPLETE | 2 | commit 86af507c |
| L5 consistency + docs | COMPLETE | 1 | commit c1fd91d5 |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Completion tracking

| Item | Status | Iterations | Timestamp | Complete | Notes |
|------|--------|------------|-----------|----------|-------|
| W0 tokens foundation | COMPLETE | 1 | 2026-08-24T23:58Z | 100% | tokens.json5 + theme pkg parse/validate + parity pins; go test ./theme green. Note: JSON5 unquoted keys unsupported by hujson Standardize→quoted keys used. |
| L1 flutter palette layer | COMPLETE | 1 | 2026-08-25T00:45Z | 100% | commit 2d6124d1 | |
| L2 tui runtime palette | COMPLETE | 1 | 2026-08-24T23:59Z | 100% | commit 660227b4; viz wiring fixed in-session | |
| L3 flutter settings + tests | COMPLETE | 1 | 2026-08-25T01:31Z | 100% | commit 78db4f98; +2 latent panel bugs fixed | |
| L4 tui stragglers | COMPLETE | 2 | 2026-08-25T11:55Z | 100% | landed in commit 86af507c (swept by concurrent session's commit — message mismatch noted); stray-literal guard test added | |
| L5 consistency + docs | COMPLETE | 1 | 2026-08-25T00:50Z | 100% | commit c1fd91d5 | |
| Gates (lint/build/tests/report) | COMPLETE | 1 | 2026-08-25T11:58Z | 100% | go build+full go test green; flutter 322 pass, analyze 0 errors | |
