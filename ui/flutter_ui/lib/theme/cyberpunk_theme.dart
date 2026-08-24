import 'package:flutter/material.dart';

import 'app_palette.dart';
import 'colors.dart';
import 'typography.dart';
import 'effects.dart';

/// Complete cyberpunk theme configuration.
///
/// [build] constructs the full [ThemeData] from an [AppPalette]; the provider
/// layer calls it with the active palette while legacy callers keep using
/// [darkTheme], which delegates to the palette currently set on
/// [CyberpunkColors].
abstract class CyberpunkTheme {
  /// Legacy accessor: theme built from the active [AppPalette].
  static ThemeData get darkTheme => build(CyberpunkColors.active);

  /// Build the full app theme from a palette. Ports every field of the
  /// original hand-written darkTheme, mapping colors through frozen roles.
  static ThemeData build(AppPalette palette) => ThemeData(
    useMaterial3: false,
    brightness: Brightness.dark,
    scaffoldBackgroundColor: palette.background,
    primaryColor: palette.primary,
    colorScheme: ColorScheme.dark(
      primary: palette.primary,
      secondary: palette.primaryBright,
      tertiary: palette.primaryGlow,
      surface: palette.surface,
      error: palette.error,
      onPrimary: palette.background,
      onSecondary: palette.background,
      onTertiary: palette.background,
      onSurface: palette.primaryGlow,
      onError: palette.background,
    ),
    fontFamily: CyberpunkTypography.primaryFont,
    textTheme: _textTheme,
    appBarTheme: _appBarTheme(palette),
    cardTheme: _cardTheme(palette),
    elevatedButtonTheme: _elevatedButtonTheme(palette),
    outlinedButtonTheme: _outlinedButtonTheme(palette),
    inputDecorationTheme: _inputDecorationTheme(palette),
    dividerTheme: _dividerTheme(palette),
    iconTheme: _iconTheme(palette),
  );

  static TextTheme get _textTheme => const TextTheme(
    displayLarge: CyberpunkTypography.displayLarge,
    displayMedium: CyberpunkTypography.displayMedium,
    headlineLarge: CyberpunkTypography.headlineLarge,
    headlineMedium: CyberpunkTypography.headlineMedium,
    headlineSmall: CyberpunkTypography.headlineSmall,
    bodyLarge: CyberpunkTypography.bodyLarge,
    bodyMedium: CyberpunkTypography.bodyMedium,
    bodySmall: CyberpunkTypography.bodySmall,
    labelLarge: CyberpunkTypography.label,
  );

  static AppBarTheme _appBarTheme(AppPalette palette) => AppBarTheme(
    backgroundColor: palette.surface,
    foregroundColor: palette.primary,
    elevation: 0,
    centerTitle: false,
    titleTextStyle: CyberpunkTypography.headlineMedium,
    actionsIconTheme: IconThemeData(color: palette.primaryBright),
  );

  static CardThemeData _cardTheme(AppPalette palette) => CardThemeData(
    color: palette.surfaceAlt,
    elevation: 4,
    shadowColor: palette.primaryDark,
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
  );

  static ElevatedButtonThemeData _elevatedButtonTheme(AppPalette palette) =>
      ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: palette.primary,
          foregroundColor: palette.background,
          elevation: 2,
          shadowColor: palette.primaryGlow,
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(2)),
          textStyle: CyberpunkTypography.label,
        ),
      );

  static OutlinedButtonThemeData _outlinedButtonTheme(AppPalette palette) =>
      OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: palette.primary,
          side: BorderSide(color: palette.primary, width: 1.5),
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(2)),
          textStyle: CyberpunkTypography.label,
        ),
      );

  static InputDecorationTheme _inputDecorationTheme(AppPalette palette) =>
      InputDecorationTheme(
        filled: true,
        fillColor: palette.surface,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 12,
        ),
        border: OutlineInputBorder(
          borderSide: BorderSide(color: palette.surfaceAlt),
          borderRadius: const BorderRadius.all(Radius.circular(2)),
        ),
        enabledBorder: OutlineInputBorder(
          borderSide: BorderSide(color: palette.surfaceAlt),
          borderRadius: const BorderRadius.all(Radius.circular(2)),
        ),
        focusedBorder: OutlineInputBorder(
          borderSide: BorderSide(color: palette.primary, width: 2),
          borderRadius: const BorderRadius.all(Radius.circular(2)),
        ),
        errorBorder: OutlineInputBorder(
          borderSide: BorderSide(color: palette.error),
          borderRadius: const BorderRadius.all(Radius.circular(2)),
        ),
        labelStyle: CyberpunkTypography.bodyMedium,
        hintStyle: CyberpunkTypography.bodySmall,
      );

  static DividerThemeData _dividerTheme(AppPalette palette) =>
      DividerThemeData(color: palette.surfaceAlt, thickness: 1, space: 1);

  static IconThemeData _iconTheme(AppPalette palette) =>
      IconThemeData(color: palette.primary, size: 24);

  /// Common container decoration
  static BoxDecoration get panelDecoration => BoxDecoration(
    color: CyberpunkColors.darkGray,
    border: Border.all(
      color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
      width: 1,
    ),
    boxShadow: CyberpunkEffects.borderGlow(),
  );

  /// Angled panel decoration (cyberpunk style)
  static BoxDecoration get angledPanelDecoration => BoxDecoration(
    color: CyberpunkColors.darkGray,
    border: Border.all(
      color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
      width: 1.5,
    ),
    gradient: CyberpunkEffects.angularGradient,
  );
}
