import 'package:flutter/material.dart';

import 'app_palette.dart';

/// ORANGE VOID color palette — forwarding shim over the active [AppPalette].
///
/// Member names are frozen: ~1,076 call-sites across lib/ reference these
/// getters directly and must keep compiling without edits. Values follow the
/// runtime palette set via [setActive]; the default is cyberpunk so a cold
/// start without initialization renders identically to the pre-palette code.
///
/// Legacy member → role mapping:
///   black→background, darkGray→surface, midGray→surfaceAlt,
///   lightGray→border, veryLightGray→textPrimary,
///   orangePrimary→primary, orangeBright→primaryBright,
///   orangeDark→primaryDark, orangeGlow→primaryGlow, orangeAccent→accent,
///   cyanAccent/blueInfo→info, greenSuccess→success, redAlert→error,
///   yellowWarning→warning, terminalGreen→terminalGreen,
///   terminalAmber→terminalAmber.
abstract class CyberpunkColors {
  /// The palette every getter forwards to. Never null; defaults to cyberpunk.
  static AppPalette _active = AppPalette.forName('cyberpunk');

  /// Swap the palette all [CyberpunkColors] getters read from.
  ///
  /// Call once at startup with the stored theme and again whenever the
  /// theme changes, so direct `CyberpunkColors.*` call-sites follow along.
  static void setActive(AppPalette palette) {
    _active = palette;
  }

  /// Currently active palette (exposed for tests/debug tooling).
  static AppPalette get active => _active;

  // Base colors
  static Color get black => _active.background;
  static Color get darkGray => _active.surface;
  static Color get midGray => _active.surfaceAlt;

  /// Direct role access (used by newer call-sites like find_bar).
  static Color get surfaceAlt => _active.surfaceAlt;
  static Color get lightGray => _active.border;
  static Color get veryLightGray => _active.textPrimary; // For verbosity text

  // Primary - Orange spectrum
  static Color get orangePrimary => _active.primary;
  static Color get orangeBright => _active.primaryBright;
  static Color get orangeDark => _active.primaryDark;
  static Color get orangeGlow => _active.primaryGlow;
  static Color get orangeAccent => _active.accent;

  // Secondary accents
  static Color get cyanAccent => _active.info;
  static Color get greenSuccess => _active.success;
  static Color get redAlert => _active.error;
  static Color get yellowWarning => _active.warning;
  static Color get blueInfo => _active.info;

  // Terminal colors
  static Color get terminalGreen => _active.terminalGreen;
  static Color get terminalAmber => _active.terminalAmber;

  // Transparent variants
  static Color orangeTransparent(double opacity) =>
      orangePrimary.withValues(alpha: opacity);
  static Color blackTransparent(double opacity) =>
      black.withValues(alpha: opacity);

  // Gradients
  static List<Color> get orangeGradient => [
    orangePrimary,
    orangeBright,
    orangeDark,
  ];

  static List<Color> get darkGradient => [black, darkGray, midGray];
}
