import 'package:flutter/material.dart';

import 'tokens_data.dart';

/// Immutable runtime palette: one entry per variant in `theme/tokens.json5`.
///
/// Fields map 1:1 to the 18 frozen roles. Built from the const [kTokensData]
/// copy of `theme/tokens.json5`; see [palettes] for all known variants and
/// [forName] for the unknown-name fallback.
class AppPalette {
  final String name;

  final Color primary;
  final Color primaryBright;
  final Color primaryDark;
  final Color primaryGlow;
  final Color accent;
  final Color secondary;
  final Color success;
  final Color warning;
  final Color error;
  final Color info;
  final Color background;
  final Color surface;
  final Color surfaceAlt;
  final Color border;
  final Color textPrimary;
  final Color textMuted;
  final Color terminalGreen;
  final Color terminalAmber;

  const AppPalette({
    required this.name,
    required this.primary,
    required this.primaryBright,
    required this.primaryDark,
    required this.primaryGlow,
    required this.accent,
    required this.secondary,
    required this.success,
    required this.warning,
    required this.error,
    required this.info,
    required this.background,
    required this.surface,
    required this.surfaceAlt,
    required this.border,
    required this.textPrimary,
    required this.textMuted,
    required this.terminalGreen,
    required this.terminalAmber,
  });

  /// Parse a `#RRGGBB` hex string into an opaque [Color].
  static Color _hex(String hex) =>
      Color(int.parse(hex.substring(1), radix: 16) | 0xFF000000);

  /// Build a palette from one variant of [kTokensData]. Throws if the
  /// variant is missing (the tokens file is guarded by tests).
  factory AppPalette.fromTokens(String name) {
    final roles = kTokensData[name]!;
    return AppPalette(
      name: name,
      primary: AppPalette._hex(roles['primary']!),
      primaryBright: AppPalette._hex(roles['primaryBright']!),
      primaryDark: AppPalette._hex(roles['primaryDark']!),
      primaryGlow: AppPalette._hex(roles['primaryGlow']!),
      accent: AppPalette._hex(roles['accent']!),
      secondary: AppPalette._hex(roles['secondary']!),
      success: AppPalette._hex(roles['success']!),
      warning: AppPalette._hex(roles['warning']!),
      error: AppPalette._hex(roles['error']!),
      info: AppPalette._hex(roles['info']!),
      background: AppPalette._hex(roles['background']!),
      surface: AppPalette._hex(roles['surface']!),
      surfaceAlt: AppPalette._hex(roles['surfaceAlt']!),
      border: AppPalette._hex(roles['border']!),
      textPrimary: AppPalette._hex(roles['textPrimary']!),
      textMuted: AppPalette._hex(roles['textMuted']!),
      terminalGreen: AppPalette._hex(roles['terminalGreen']!),
      terminalAmber: AppPalette._hex(roles['terminalAmber']!),
    );
  }

  /// All known palettes, keyed by variant name.
  static Map<String, AppPalette> get palettes => {
        for (final name in kTokensData.keys) name: AppPalette.fromTokens(name),
      };

  /// Resolve a stored theme name; unknown names fall back to cyberpunk
  /// so a stale preference can never produce an invalid palette.
  static AppPalette forName(String name) {
    if (kTokensData.containsKey(name)) return AppPalette.fromTokens(name);
    return AppPalette.fromTokens('cyberpunk');
  }
}
