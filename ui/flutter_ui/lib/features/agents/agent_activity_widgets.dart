import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../theme/colors.dart';

/// 8-bit token-activity histogram (issue #160 follow-up).
///
/// Eight bars, one per 7.5-minute cell over the last hour (oldest left).
/// Height = token share vs this agent's peak cell. Monochrome green on
/// black; empty cells render dim. The fill is quantized to 4 block levels
/// and dithered with a checker pattern so it reads as 8-bit graphics
/// rather than a smooth chart.
class TokenHistogram extends StatelessWidget {
  const TokenHistogram({super.key, required this.cells, this.height = 22});

  /// 8 token counts, oldest first. Fewer/more cells are tolerated.
  final List<int> cells;
  final double height;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: height,
      child: CustomPaint(
        painter: _HistogramPainter(cells: cells),
        size: Size.infinite,
      ),
    );
  }
}

class _HistogramPainter extends CustomPainter {
  _HistogramPainter({required this.cells});

  final List<int> cells;

  static const _barCount = 8;
  // Quantized block levels (in pixels of a 4-step ladder) give the bars a
  // chunky, stepped silhouette instead of smooth fractional heights.
  static const _levels = 4;

  @override
  void paint(Canvas canvas, Size size) {
    final green = CyberpunkColors.greenSuccess;
    final dim = green.withValues(alpha: 0.14);
    const gap = 2.0;
    final barWidth = (size.width - gap * (_barCount - 1)) / _barCount;
    if (barWidth <= 0) return;

    var peak = 0;
    for (final c in cells) {
      if (c > peak) peak = c;
    }

    for (var i = 0; i < _barCount; i++) {
      final x = i * (barWidth + gap);
      final value = i < cells.length ? cells[i] : 0;
      final rect = Rect.fromLTWH(x, 0, barWidth, size.height);

      // Empty cell: dim outline block so the grid reads even at zero.
      if (value <= 0 || peak <= 0) {
        canvas.drawRect(rect, Paint()..color = dim);
        continue;
      }

      // Quantize the share to the block ladder (never zero for a non-zero
      // cell: at least one block so activity is always visible).
      final share = value / peak;
      final level = math.max(1, (share * _levels).ceil());
      final barHeight = size.height * level / _levels;
      final barRect = Rect.fromLTWH(
        x,
        size.height - barHeight,
        barWidth,
        barHeight,
      );
      canvas.drawRect(barRect, Paint()..color = green);

      // Dither: knock out alternating 2px checker cells inside the fill so
      // the bar reads as a pixel pattern, not a solid rectangle.
      final knockout = Paint()..color = Colors.black.withValues(alpha: 0.55);
      const cell = 2.0;
      var row = 0;
      for (var y = barRect.top; y < barRect.bottom; y += cell) {
        var col = 0;
        for (var cx = barRect.left; cx < barRect.right; cx += cell) {
          if ((row + col) % 2 == 0) {
            canvas.drawRect(
              Rect.fromLTWH(
                cx,
                y,
                math.min(cell, barRect.right - cx),
                math.min(cell, barRect.bottom - y),
              ),
              knockout,
            );
          }
          col++;
        }
        row++;
      }
    }
  }

  @override
  bool shouldRepaint(covariant _HistogramPainter oldDelegate) {
    if (oldDelegate.cells.length != cells.length) return true;
    for (var i = 0; i < cells.length; i++) {
      if (oldDelegate.cells[i] != cells[i]) return true;
    }
    return false;
  }
}

/// Slow orange pulse border for currently-active agents (issue #160
/// follow-up). Each instance picks its own random period (1.6-3.4 s) and
/// phase so a grid of active agents never pulses in lockstep. Inactive
/// agents render the child unchanged (no animation cost).
class PulsingBorder extends StatefulWidget {
  const PulsingBorder({
    super.key,
    required this.active,
    required this.borderRadius,
    required this.child,
  });

  final bool active;
  final BorderRadius borderRadius;
  final Widget child;

  @override
  State<PulsingBorder> createState() => _PulsingBorderState();
}

class _PulsingBorderState extends State<PulsingBorder>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;
  late final Duration _period;
  Timer? _phaseTimer;
  static final _random = math.Random();

  @override
  void initState() {
    super.initState();
    // Random interval per card: 1600-3400 ms per full pulse cycle.
    _period = Duration(milliseconds: 1600 + _random.nextInt(1800));
    _controller = AnimationController(vsync: this, duration: _period);
    if (widget.active) {
      _startWithRandomPhase();
    }
  }

  void _startWithRandomPhase() {
    // Random initial phase so cards do not synchronize on mount.
    _controller.value = _random.nextDouble();
    _controller.repeat(reverse: true);
  }

  @override
  void didUpdateWidget(covariant PulsingBorder oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.active && !oldWidget.active) {
      _startWithRandomPhase();
    } else if (!widget.active && oldWidget.active) {
      _controller.stop();
      _controller.value = 0;
    }
  }

  @override
  void dispose() {
    _phaseTimer?.cancel();
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.active) return widget.child;
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, child) {
        // Ease the 0..1 triangle into a slow breathe (0.25..1.0 opacity).
        final t = Curves.easeInOut.transform(_controller.value);
        final opacity = 0.25 + 0.75 * t;
        return DecoratedBox(
          decoration: BoxDecoration(
            borderRadius: widget.borderRadius,
            border: Border.all(
              color: CyberpunkColors.orangePrimary.withValues(alpha: opacity),
              width: 2,
            ),
          ),
          child: child,
        );
      },
      child: widget.child,
    );
  }
}
