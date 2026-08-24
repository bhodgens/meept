import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app_palette.dart';
import 'colors.dart';
import 'cyberpunk_theme.dart';

/// Name of the active ui theme variant ('cyberpunk' | 'midnight' |
/// 'solarized').
///
/// Initial state comes from [initialThemeName], populated at startup from
/// storage before runApp; defaults to cyberpunk when nothing was stored.
final themeNameProvider = StateProvider<String>((ref) => initialThemeName);

/// Palette resolved from [themeNameProvider]; unknown names fall back to
/// cyberpunk via [AppPalette.forName].
final paletteProvider = Provider<AppPalette>(
  (ref) => AppPalette.forName(ref.watch(themeNameProvider)),
);

/// Full app ThemeData derived from the active palette. Watching this in
/// MaterialApp rebuilds the whole widget tree on a theme swap.
final appThemeProvider = Provider<ThemeData>(
  (ref) => CyberpunkTheme.build(ref.watch(paletteProvider)),
);

/// Theme name resolved before runApp by [initStoredTheme] (main.dart).
/// Defaults to cyberpunk so tests and cold starts are deterministic.
String initialThemeName = 'cyberpunk';

/// Apply [name] everywhere: static forwarding layer + provider state.
///
/// Call-sites that read CyberpunkColors.* directly (no Riverpod watch) only
/// follow a swap if setActive runs, and widgets watching appThemeProvider
/// only follow it through the StateProvider — keep both in sync here.
void applyTheme(WidgetRef ref, String name) {
  final palette = AppPalette.forName(name);
  CyberpunkColors.setActive(palette);
  ref.read(themeNameProvider.notifier).state = palette.name;
}
