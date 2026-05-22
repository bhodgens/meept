import 'package:flutter/material.dart';
import 'colors.dart';

/// Cyberpunk visual effects - glows, glitches, scanlines
abstract class CyberpunkEffects {
  /// Orange glow shadow for text and widgets
  static List<BoxShadow> glowShadow({double intensity = 1.0}) => [
        BoxShadow(
          color: CyberpunkColors.orangePrimary.withValues(alpha: 0.5 * intensity),
          blurRadius: 10 * intensity,
          spreadRadius: 2 * intensity,
        ),
        BoxShadow(
          color: CyberpunkColors.orangeGlow.withValues(alpha: 0.3 * intensity),
          blurRadius: 20 * intensity,
          spreadRadius: 5 * intensity,
        ),
      ];

  /// Subtle border glow for containers
  static List<BoxShadow> borderGlow({double intensity = 1.0}) => [
        BoxShadow(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3 * intensity),
          blurRadius: 5 * intensity,
          spreadRadius: 1 * intensity,
        ),
      ];

  /// Angular gradient for backgrounds
  static const LinearGradient angularGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: CyberpunkColors.darkGradient,
    stops: [0.0, 0.5, 1.0],
  );

  /// Orange accent gradient
  static const LinearGradient orangeGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: CyberpunkColors.orangeGradient,
    stops: [0.0, 0.7, 1.0],
  );

  /// Scanline effect overlay
  static Decoration scanlineOverlay({double opacity = 0.1}) =>
      BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [
            Colors.black.withValues(alpha: 0),
            Colors.black.withValues(alpha: opacity),
            Colors.black.withValues(alpha: 0),
          ],
          stops: const [0.0, 0.5, 1.0],
        ),
      );

  /// Angled corner decoration (cyberpunk style cut corners)
  static ClipPath angledClip({double cutSize = 8.0}) =>
      ClipPath(clipper: AngledCornerClipper(cutSize: cutSize));

  /// Glitch effect animation (simulated with transforms)
  static Matrix4 glitchTransform({required double offset}) =>
      Matrix4.translationValues(offset, 0, 0);
}

/// Clipper for angled corners
class AngledCornerClipper extends CustomClipper<Path> {
  final double cutSize;

  AngledCornerClipper({this.cutSize = 8.0});

  @override
  Path getClip(Size size) {
    final path = Path();
    path.moveTo(0, 0);
    path.lineTo(size.width - cutSize, 0);
    path.lineTo(size.width, cutSize);
    path.lineTo(size.width, size.height);
    path.lineTo(cutSize, size.height);
    path.lineTo(0, size.height - cutSize);
    path.close();
    return path;
  }

  @override
  bool shouldReclip(covariant AngledCornerClipper oldClipper) =>
      oldClipper.cutSize != cutSize;
}
