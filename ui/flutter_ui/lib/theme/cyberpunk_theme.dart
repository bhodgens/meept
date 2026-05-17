import 'package:flutter/material.dart';
import 'colors.dart';
import 'typography.dart';
import 'effects.dart';

/// Complete cyberpunk theme configuration
abstract class CyberpunkTheme {
  static ThemeData get darkTheme => ThemeData(
        useMaterial3: false,
        brightness: Brightness.dark,
        scaffoldBackgroundColor: CyberpunkColors.black,
        primaryColor: CyberpunkColors.orangePrimary,
        colorScheme: const ColorScheme.dark(
          primary: CyberpunkColors.orangePrimary,
          secondary: CyberpunkColors.orangeBright,
          tertiary: CyberpunkColors.orangeGlow,
          background: CyberpunkColors.black,
          surface: CyberpunkColors.darkGray,
          error: CyberpunkColors.redAlert,
          onPrimary: CyberpunkColors.black,
          onSecondary: CyberpunkColors.black,
          onTertiary: CyberpunkColors.black,
          onBackground: CyberpunkColors.orangeGlow,
          onSurface: CyberpunkColors.orangeGlow,
          onError: CyberpunkColors.black,
        ),
        fontFamily: CyberpunkTypography.primaryFont,
        textTheme: _textTheme,
        appBarTheme: _appBarTheme,
        cardTheme: _cardTheme,
        elevatedButtonTheme: _elevatedButtonTheme,
        outlinedButtonTheme: _outlinedButtonTheme,
        inputDecorationTheme: _inputDecorationTheme,
        dividerTheme: _dividerTheme,
        iconTheme: _iconTheme,
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

  static AppBarTheme get _appBarTheme => AppBarTheme(
        backgroundColor: CyberpunkColors.darkGray,
        foregroundColor: CyberpunkColors.orangePrimary,
        elevation: 0,
        centerTitle: false,
        titleTextStyle: CyberpunkTypography.headlineMedium,
        actionsIconTheme:
            const IconThemeData(color: CyberpunkColors.orangeBright),
      );

  static CardTheme get _cardTheme => CardTheme(
        color: CyberpunkColors.midGray,
        elevation: 4,
        shadowColor: CyberpunkColors.orangeDark,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(4),
        ),
      );

  static ElevatedButtonThemeData get _elevatedButtonTheme =>
      ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: CyberpunkColors.orangePrimary,
          foregroundColor: CyberpunkColors.black,
          elevation: 2,
          shadowColor: CyberpunkColors.orangeGlow,
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(2),
          ),
          textStyle: CyberpunkTypography.label,
        ),
      );

  static OutlinedButtonThemeData get _outlinedButtonTheme =>
      OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: CyberpunkColors.orangePrimary,
          side: const BorderSide(color: CyberpunkColors.orangePrimary, width: 1.5),
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(2),
          ),
          textStyle: CyberpunkTypography.label,
        ),
      );

  static InputDecorationTheme get _inputDecorationTheme =>
      InputDecorationTheme(
        filled: true,
        fillColor: CyberpunkColors.darkGray,
        contentPadding:
            const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        border: const OutlineInputBorder(
          borderSide: BorderSide(color: CyberpunkColors.midGray),
          borderRadius: BorderRadius.all(Radius.circular(2)),
        ),
        enabledBorder: const OutlineInputBorder(
          borderSide: BorderSide(color: CyberpunkColors.midGray),
          borderRadius: BorderRadius.all(Radius.circular(2)),
        ),
        focusedBorder: const OutlineInputBorder(
          borderSide: BorderSide(color: CyberpunkColors.orangePrimary, width: 2),
          borderRadius: BorderRadius.all(Radius.circular(2)),
        ),
        errorBorder: const OutlineInputBorder(
          borderSide: BorderSide(color: CyberpunkColors.redAlert),
          borderRadius: BorderRadius.all(Radius.circular(2)),
        ),
        labelStyle: CyberpunkTypography.bodyMedium,
        hintStyle: CyberpunkTypography.bodySmall,
      );

  static DividerThemeData get _dividerTheme => const DividerThemeData(
        color: CyberpunkColors.midGray,
        thickness: 1,
        space: 1,
      );

  static IconThemeData get _iconTheme => const IconThemeData(
        color: CyberpunkColors.orangePrimary,
        size: 24,
      );

  /// Common container decoration
  static BoxDecoration panelDecoration = BoxDecoration(
    color: CyberpunkColors.darkGray,
    border: Border.all(
      color: CyberpunkColors.orangeDark.withOpacity(0.3),
      width: 1,
    ),
    boxShadow: CyberpunkEffects.borderGlow(),
  );

  /// Angled panel decoration (cyberpunk style)
  static BoxDecoration angledPanelDecoration = BoxDecoration(
    color: CyberpunkColors.darkGray,
    border: Border.all(
      color: CyberpunkColors.orangePrimary.withOpacity(0.3),
      width: 1.5,
    ),
    gradient: CyberpunkEffects.angularGradient,
  );
}
