import 'package:flutter/material.dart';

/// Cyberpunk color palette - orange/black theme
abstract class CyberpunkColors {
  // Primary colors
  static const Color black = Color(0xFF0A0A0A);
  static const Color darkGray = Color(0xFF1A1A1A);
  static const Color midGray = Color(0xFF2A2A2A);
  static const Color lightGray = Color(0xFF3A3A3A);

  // Accent - Orange spectrum
  static const Color orangePrimary = Color(0xFFFF6600);
  static const Color orangeBright = Color(0xFFFF8533);
  static const Color orangeDark = Color(0xFFCC5200);
  static const Color orangeGlow = Color(0xFFFFA666);

  // Secondary accents
  static const Color redAlert = Color(0xFFFF3333);
  static const Color greenSuccess = Color(0xFF33FF66);
  static const Color blueInfo = Color(0xFF3399FF);
  static const Color yellowWarning = Color(0xFFFFCC00);

  // Terminal colors
  static const Color terminalGreen = Color(0xFF33FF33);
  static const Color terminalAmber = Color(0xFFFFB000);

  // Gradients
  static const List<Color> orangeGradient = [
    Color(0xFFFF6600),
    Color(0xFFFF8533),
    Color(0xFFCC5200),
  ];

  static const List<Color> darkGradient = [
    Color(0xFF0A0A0A),
    Color(0xFF1A1A1A),
    Color(0xFF2A2A2A),
  ];

  // Opacity variants
  static Color orangeTransparent(double opacity) =>
      orangePrimary.withOpacity(opacity);
  static Color blackTransparent(double opacity) =>
      black.withOpacity(opacity);
}
