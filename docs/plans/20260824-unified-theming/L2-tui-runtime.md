---
name: L2-tui-runtime.md
description: Go TUI runtime palette — Palette type, styles derivation, config key, wiring
version: 1.0.0
---

# L2 — TUI runtime palette

## Files owned

- `internal/tui/palette.go` (new)
- `internal/tui/palette_test.go` (new)
- `internal/tui/styles.go` (var block → palette-derived)
- `internal/tui/config.go` (add RenderingConfig.UITheme + default)
- `internal/tui/app.go` (call SetPalette after LoadClientConfigPath, before
  DefaultStyles())
- `theme/embed.go`, `theme/tokens_test.go` (created in W0; L2 may extend
  tokens_test only if compile requires)

## Tasks

1. `theme` package (W0 file): `//go:embed tokens.json5`; expose
   `func Parse(data []byte) (map[string]map[string]string, error)` using
   tailscale/hujson → encoding/json (hujson.Standardize then json.Unmarshal).
2. `internal/tui/palette.go`:
   - `type Palette struct` with lipgloss.Color fields for all 18 roles.
   - `SetPalette(name string) error` — parse embedded tokens (cache once via
     sync.Once), unknown name → error listing valid names.
   - `Current() *Palette`, `Names() []string`.
3. styles.go: replace the exported `Color*` var block with unexported vars
   assigned by an `initPalette()` called from SetPalette; keep exported names
   as aliases (`ColorPrimary = …`) so the ~403 existing references compile —
   they become palette-backed at startup instead of compile-time constants.
   `DefaultStyles()` unchanged otherwise. NOTE: exported Color* are currently
   used by cmd/meept-lite and others via package import; verify with
   `go build ./...` that no external caller depends on them being consts.
4. config.go: add `UITheme string json:"ui_theme"` to RenderingConfig,
   default "cyberpunk" in both default-construction sites (~line 178 and the
   missing-field fallback ~line 287).
5. app.go NewApp: after LoadClientConfigPath(), call
   `_ = tui.SetPalette(clientConfig.Rendering.UITheme)`-equivalent (package-
   internal: plain call) with slog.Warn on error, BEFORE DefaultStyles().
6. viz/colors.go: convert its 8 hardcoded vars to derive from
   tui.Current() (import parent pkg or duplicate tiny parse; prefer importing
   `github.com/bhodgens/meept/internal/tui`? — NO: viz is imported BY tui;
   instead viz gains `viz.SetPalette(*tui.Palette)`-free approach: define its
   own Current() reading theme.Parse directly). Keep it simple: viz parses the
   same embedded tokens itself via the theme package.

## Tests

- palette_test.go: parse ok for cyberpunk/midnight/solarized; error on unknown;
  SetPalette('midnight') changes Current().Primary hex string.
- styles_test.go additions: DefaultStyles() after SetPalette reflects palette
  (construct style, render a probe string, assert ANSI contains new hex).

## Verify

go build ./... && go test ./internal/tui/... ./theme/... && gofmt -l internal/tui theme

## Constraints

Restart-applied theme is acceptable (issue non-goal: no hot reload).
Do not touch models/*.go stragglers (L4 owns).
