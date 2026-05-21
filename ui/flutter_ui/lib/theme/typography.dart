import 'package:flutter/material.dart';

/// ORANGE VOID typography configuration - all lowercase per project convention
abstract class CyberpunkTypography {
  static const String primaryFont = 'SourceCodePro';
  static const String displayFont = 'SourceCodePro';

  // Text styles - all lowercase per project convention
  static const TextStyle displayLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 32,
    fontWeight: FontWeight.bold,
    letterSpacing: 3,
  );

  static const TextStyle displayMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 24,
    fontWeight: FontWeight.bold,
    letterSpacing: 2,
  );

  static const TextStyle headlineLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 20,
    fontWeight: FontWeight.bold,
    letterSpacing: 2,
  );

  static const TextStyle headlineMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 18,
    fontWeight: FontWeight.w600,
    letterSpacing: 1,
  );

  static const TextStyle headlineSmall = TextStyle(
    fontFamily: primaryFont,
    fontSize: 16,
    fontWeight: FontWeight.w600,
  );

  static const TextStyle bodyLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 14,
    letterSpacing: 0.5,
  );

  static const TextStyle bodyMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 13,
  );

  static const TextStyle bodySmall = TextStyle(
    fontFamily: primaryFont,
    fontSize: 11,
    color: Colors.grey,
  );

  static const TextStyle label = TextStyle(
    fontFamily: primaryFont,
    fontSize: 12,
    fontWeight: FontWeight.w600,
    letterSpacing: 1,
  );

  static const TextStyle button = TextStyle(
    fontFamily: primaryFont,
    fontSize: 12,
    fontWeight: FontWeight.bold,
    letterSpacing: 1,
  );

  static const TextStyle code = TextStyle(
    fontFamily: 'monospace',
    fontSize: 12,
  );
}
