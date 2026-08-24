# Theming

Meept's TUI and GUI share one set of color tokens. `theme/tokens.json5` at
the repo root is the single source of truth.

## Variants

| Variant    | Look |
|------------|------|
| cyberpunk  | default — orange-on-black, matches the historical GUI palette exactly |
| midnight   | tokyo-night style blues/greens on deep indigo |
| solarized  | classic solarized dark tones |

## Roles

Every variant defines exactly these 18 roles (frozen; enforced by tests):

primary, primaryBright, primaryDark, primaryGlow, accent, secondary,
success, warning, error, info, background, surface, surfaceAlt, border,
textPrimary, textMuted, terminalGreen, terminalAmber

## Selecting a theme

### GUI (Flutter)

- Settings panel → "theme" dropdown. The swap applies immediately and is
  saved to the `ui_theme` key in local storage.
- The preference also mirrors to client config (`rendering.ui_theme`) when
  saved through settings.
- Unknown or stale values fall back to cyberpunk.

### TUI

- Client config (`~/.meept/client.json5`):

  ```json5
  {
    rendering: {
      ui_theme: "midnight", // cyberpunk | midnight | solarized
    }
  }
  ```

- Set it from the CLI:

  ```
  meept config set rendering.ui_theme midnight
  ```

- Applied at startup (restart to see a change). An invalid name logs a
  warning and keeps the built-in defaults.
- Note: `rendering.theme` is a different key — it selects the *syntax
  highlighting* theme (chroma), not the UI palette.

## How consumers read the tokens

- Go: package `theme` embeds tokens.json5 and exposes `Parse`; both
  `internal/tui` and `internal/tui/viz` resolve palettes through it.
- Flutter: `lib/theme/tokens_data.dart` holds a const copy of the same map;
  a parity test (`test/theme/`) fails if the copy drifts from tokens.json5.

## Adding a variant

1. Add the variant with all 18 roles to `theme/tokens.json5`.
2. Mirror it in `ui/flutter_ui/lib/theme/tokens_data.dart` (keep byte-exact).
3. Add the name to `theme.FrozenVariants` in `theme/tokens.go`.
4. Run: `go test ./theme/... && cd ui/flutter_ui && flutter test test/theme/`
5. The GUI dropdown lists variants from the token data automatically once
   steps 2–3 land.
