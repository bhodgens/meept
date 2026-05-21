import 'package:flutter/material.dart';

/// ORANGE VOID cyberpunk color palette
abstract class CyberpunkColors {
  // Base colors
  static const Color black = Color(0xFF000000);
  static const Color darkGray = Color(0xFF1A1A1A);
  static const Color midGray = Color(0xFF2A2A2A);
  static const Color lightGray = Color(0xFF333333);

  // Primary - Orange spectrum
  static const Color orangePrimary = Color(0xFFFF6600);
  static const Color orangeBright = Color(0xFFFF8800);
  static const Color orangeDark = Color(0xFFCC5500);
  static const Color orangeGlow = Color(0xFFFFAA33);
  static const Color orangeAccent = Color(0xFFFF9933);

  // Secondary accents
  static const Color cyanAccent = Color(0xFF00FFFF);
  static const Color greenSuccess = Color(0xFF00FFAA);
  static const Color redAlert = Color(0xFFFF3366);
  static const Color yellowWarning = Color(0xFFFFCC00);
  static const Color blueInfo = Color(0xFF3399FF);

  // Terminal colors
  static const Color terminalGreen = Color(0xFF33FF33);
  static const Color terminalAmber = Color(0xFFFFB000);

  // Transparent variants
  static Color orangeTransparent(double opacity) =>
      orangePrimary.withOpacity(opacity);
  static Color blackTransparent(double opacity) =>
      black.withOpacity(opacity);

  // Gradients
  static const List<Color> orangeGradient = [
    Color(0xFFFF6600),
    Color(0xFFFF8800),
    Color(0xFFCC5500),
  ];

  static const List<Color> darkGradient = [
    Color(0xFF000000),
    Color(0xFF1A1A1A),
    Color(0xFF2A2A2A),
  ];
}
