---
name: L1-flutter-palette.md
description: Flutter runtime palette layer — AppPalette, provider, CyberpunkColors forwarding
version: 1.0.0
---

# L1 — Flutter palette layer

## Files owned

- `ui/flutter_ui/lib/theme/app_palette.dart` (new)
- `ui/flutter_ui/lib/theme/palette_provider.dart` (new)
- `ui/flutter_ui/lib/theme/tokens_data.dart` (new — const map copy per W0)
- `ui/flutter_ui/lib/theme/colors.dart` (rewrite as forwarding shim)
- `ui/flutter_ui/lib/theme/cyberpunk_theme.dart` (build from active palette)
- `ui/flutter_ui/lib/theme/effects.dart`, `markdown_style.dart`,
  `syntax_highlighter.dart` (de-const where they break)
- `ui/flutter_ui/lib/features/chat/find_bar.dart:73` (raw literal → role)
- `ui/flutter_ui/pubspec.yaml` only if a dep is required (avoid)

## Tasks

1. `AppPalette`: immutable class with final Color fields for all 18 roles +
   `name`. Provide `AppPalette forName(String)` returning cyberpunk on unknown.
   Build from `tokensData` const map in tokens_data.dart (same shape as
   theme/tokens.json5; guarded by L5 test).
2. `palette_provider.dart`: Riverpod
   - `themeNameProvider = StateProvider<String>('cyberpunk')`
   - `paletteProvider = Provider<AppPalette>((ref) => AppPalette.forName(ref.watch(themeNameProvider)))`
   - `appThemeProvider = Provider<ThemeData>` building the full ThemeData from
     palette (port every field of current CyberpunkTheme.darkTheme: colorScheme,
     appBar/card/button/input/divider/icon themes, textTheme unchanged).
3. colors.dart rewrite: keep `abstract class CyberpunkColors` and ALL member
   names. Convert `static const Color X` → `static Color get X => …`
   reading from a settable static `_active` palette (default cyberpunk), with
   `static void setActive(AppPalette p)`. Keep orangeTransparent/blackTransparent,
   keep orangeGradient/darkGradient but as getters returning List<Color>.
4. Wire main.dart: CyberpunkApp becomes ConsumerWidget;
   MaterialApp.router theme: ref.watch(appThemeProvider); on startup read
   stored theme name ('ui_theme' SharedPreferences key first — StorageService
   gains getUiTheme/setUiTheme mirroring getTheme; fallback 'theme' key value
   if it names a known variant) and set themeNameProvider + call
   CyberpunkColors.setActive so 1,076 direct refs follow the palette too.
5. Fix ~27 const-context usages across 11 files (list from census:
   client_prefs_editor 6, settings_panel 10, orchestrator_config_editor 2,
   skill_panel 2, home_screen/tools_dropdown/chat_input/memory_panel/layouts 4):
   remove `const` keyword where it now fails. Do NOT convert widgets to watch
   providers — the static-forwarding design keeps call-sites untouched.
6. find_bar.dart:73 `Color(0xFF1F2937)` → CyberpunkColors.surfaceAlt.

## Tests

- `test/theme/app_palette_test.dart`: forName('midnight') returns midnight
  hexes; unknown name falls back to cyberpunk; setActive swaps CyberpunkColors.
- `test/theme/cyberpunk_theme_test.dart`: appThemeProvider output changes when
  themeNameProvider changes (assert primaryColor differs).

## Verify

cd ui/flutter_ui && flutter analyze && flutter test

## Constraints

Do NOT touch settings_panel dropdown entries (L3 owns). No new deps.
