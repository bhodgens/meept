import 'dart:math' as math;
import 'package:flutter/material.dart';
import '../../theme/colors.dart';

class GlitchText extends StatefulWidget {
  final String text;
  final TextStyle? style;
  final double glitchIntensity;

  const GlitchText({
    super.key,
    required this.text,
    this.style,
    this.glitchIntensity = 0.3,
  });

  @override
  State<GlitchText> createState() => _GlitchTextState();
}

class _GlitchTextState extends State<GlitchText>
    with SingleTickerProviderStateMixin {
  late AnimationController _controller;
  double _offsetX = 0;
  double _offsetY = 0;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      duration: const Duration(milliseconds: 100),
      vsync: this,
    )..addListener(() {
        setState(() {
          _offsetX = (math.Random().nextDouble() - 0.5) * widget.glitchIntensity * 4;
          _offsetY = (math.Random().nextDouble() - 0.5) * widget.glitchIntensity * 2;
        });
      });
    _controller.repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        Transform.translate(
          offset: Offset(_offsetX - 1, _offsetY),
          child: Text(
            widget.text,
            style: widget.style?.copyWith(
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
            ),
          ),
        ),
        Transform.translate(
          offset: Offset(_offsetX + 1, _offsetY),
          child: Text(
            widget.text,
            style: widget.style?.copyWith(
              color: CyberpunkColors.blueInfo.withValues(alpha: 0.7),
            ),
          ),
        ),
        Transform.translate(
          offset: Offset(_offsetX, _offsetY),
          child: Text(
            widget.text,
            style: widget.style,
          ),
        ),
      ],
    );
  }
}
