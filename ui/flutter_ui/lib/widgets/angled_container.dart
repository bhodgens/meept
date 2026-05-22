import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';

class AngledContainer extends StatelessWidget {
  final Widget child;
  final double cutSize;
  final Color? color;
  final Color? borderColor;
  final bool useGradient;
  final VoidCallback? onTap;

  const AngledContainer({
    super.key,
    required this.child,
    this.cutSize = 8.0,
    this.color,
    this.borderColor,
    this.useGradient = true,
    this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return ClipPath(
      clipper: AngledCornerClipper(cutSize: cutSize),
      child: GestureDetector(
        onTap: onTap,
        child: Container(
          decoration: BoxDecoration(
            color: color ?? CyberpunkColors.darkGray,
            gradient: useGradient ? CyberpunkEffects.angularGradient : null,
            border: Border.all(
              color: borderColor ?? CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
              width: 1.5,
            ),
            boxShadow: CyberpunkEffects.borderGlow(),
          ),
          child: child,
        ),
      ),
    );
  }
}
