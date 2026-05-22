import 'package:flutter/material.dart';

class ScanlineOverlay extends StatelessWidget {
  final double opacity;
  final Color? color;

  const ScanlineOverlay({
    super.key,
    this.opacity = 0.1,
    this.color,
  });

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [
              (color ?? Colors.black).withValues(alpha: 0),
              (color ?? Colors.black).withValues(alpha: opacity),
              (color ?? Colors.black).withValues(alpha: 0),
            ],
            stops: const [0.0, 0.5, 1.0],
          ),
        ),
      ),
    );
  }
}
