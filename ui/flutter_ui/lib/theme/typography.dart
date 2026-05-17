import 'package:flutter/material.dart';
import 'colors.dart';

/// Cyberpunk typography - monospace fonts for terminal aesthetic
abstract class CyberpunkTypography {
  // Font families (must be added to pubspec.yaml)
  static const String primaryFont = 'JetBrainsMono';
  static const String displayFont = 'ShareTechMono';

  // Base text style
  static const TextStyle baseStyle = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeGlow,
    fontSize: 14,
    fontWeight: FontWeight.w400,
    height: 1.4,
  );

  // Display/Hero text
  static const TextStyle displayLarge = TextStyle(
    fontFamily: displayFont,
    color: CyberpunkColors.orangePrimary,
    fontSize: 48,
    fontWeight: FontWeight.w700,
    letterSpacing: -1,
    shadows: [
      Shadow(
        color: CyberpunkColors.orangeGlow,
        blurRadius: 20,
      ),
    ],
  );

  static const TextStyle displayMedium = TextStyle(
    fontFamily: displayFont,
    color: CyberpunkColors.orangePrimary,
    fontSize: 36,
    fontWeight: FontWeight.w700,
    letterSpacing: -0.5,
    shadows: [
      Shadow(
        color: CyberpunkColors.orangeGlow,
        blurRadius: 15,
      ),
    ],
  );

  // Headlines
  static const TextStyle headlineLarge = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangePrimary,
    fontSize: 24,
    fontWeight: FontWeight.w700,
    letterSpacing: 0.5,
  );

  static const TextStyle headlineMedium = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeBright,
    fontSize: 20,
    fontWeight: FontWeight.w600,
  );

  static const TextStyle headlineSmall = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeGlow,
    fontSize: 16,
    fontWeight: FontWeight.w600,
  );

  // Body text
  static const TextStyle bodyLarge = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeGlow,
    fontSize: 16,
    height: 1.5,
  );

  static const TextStyle bodyMedium = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeGlow,
    fontSize: 14,
    height: 1.4,
  );

  static const TextStyle bodySmall = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.lightGray,
    fontSize: 12,
    height: 1.3,
  );

  // Code/Terminal
  static const TextStyle code = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.terminalGreen,
    fontSize: 13,
    backgroundColor: CyberpunkColors.black,
  );

  // Labels
  static const TextStyle label = TextStyle(
    fontFamily: primaryFont,
    color: CyberpunkColors.orangeBright,
    fontSize: 11,
    fontWeight: FontWeight.w600,
    letterSpacing: 1,
  );
}
