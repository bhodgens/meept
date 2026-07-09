/// Layout 3: Radial/Hub Navigation
///
/// Central content area with radial nav buttons positioned around.
/// Futuristic cyberpunk aesthetic with angular elements.
///
/// Structure:
/// +----------------------------------------------------------+
/// |  [Header Bar]                                            |
/// +----------------------------------------------------------+
/// |                                                          |
/// |    [nav]                        [nav]                    |
/// |                                                          |
/// |              Main Content Area                           |
/// |                                                          |
/// |    [nav]    [center hub]    [nav]                        |
/// |                                                          |
/// |         [nav]         [nav]                              |
/// |                                                          |
/// +----------------------------------------------------------+
/// |  [Status Bar]                                            |
/// +----------------------------------------------------------+

import 'dart:math' as math;
import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../theme/effects.dart';

class Layout3RadialHub extends StatelessWidget {
  const Layout3RadialHub({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          _HeaderBar(),
          Expanded(
            child: Stack(
              children: [
                // Background with scanline effect
                DecoratedBox(
                  decoration: CyberpunkEffects.scanlineOverlay(opacity: 0.05),
                ),
                // Radial navigation layout
                _RadialNavigation(),
              ],
            ),
          ),
          _StatusBar(),
        ],
      ),
    );
  }
}

class _HeaderBar extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      height: 50,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          bottom: BorderSide(
            color: CyberpunkColors.orangePrimary,
            width: 2,
          ),
        ),
      ),
      child: Row(
        children: [
          // Angled corner decoration
          CustomPaint(
            size: const Size(30, 50),
            painter: _AngledCornerPainter(),
          ),
          Text(
            'meept',
            style: CyberpunkTypography.headlineMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontWeight: FontWeight.bold,
              letterSpacing: 3,
            ),
          ),
          const SizedBox(width: 24),
          // Connection indicator
          _ConnectionPill(),
          const Spacer(),
          // Header actions
          _ActionButton(Icons.settings, 'settings'),
          _ActionButton(Icons.info_outline, 'info'),
          Container(
            width: 2,
            height: 30,
            color: CyberpunkColors.orangeDark,
            margin: const EdgeInsets.symmetric(horizontal: 8),
          ),
          _ActionButton(Icons.close, 'exit'),
        ],
      ),
    );
  }
}

class _ConnectionPill extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(
          color: CyberpunkColors.greenSuccess.withValues(alpha: 0.5),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              color: CyberpunkColors.greenSuccess,
              shape: BoxShape.circle,
              boxShadow: CyberpunkEffects.glowShadow(intensity: 0.5),
            ),
          ),
          const SizedBox(width: 8),
          Text(
            'connected',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.greenSuccess,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
      ),
    );
  }
}

class _ActionButton extends StatelessWidget {
  final IconData icon;
  final String label;

  const _ActionButton(this.icon, this.label);

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.all(8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
        ),
      ),
      child: Icon(
        icon,
        size: 18,
        color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
      ),
    );
  }
}

class _AngledCornerPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = CyberpunkColors.orangePrimary
      ..style = PaintingStyle.fill;

    final path = Path();
    path.moveTo(0, 0);
    path.lineTo(size.width - 10, 0);
    path.lineTo(size.width, size.height);
    path.lineTo(0, size.height);
    path.close();

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}

class _RadialNavigation extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final navItems = [
      {'label': 'chat', 'icon': Icons.chat, 'active': true},
      {'label': 'sessions', 'icon': Icons.folder, 'active': false},
      {'label': 'plans', 'icon': Icons.document_scanner, 'active': false},
      {'label': 'tasks', 'icon': Icons.task_alt, 'active': false},
      {'label': 'agents', 'icon': Icons.smart_toy, 'active': false},
    ];

    return Center(
      child: Stack(
        alignment: Alignment.center,
        children: [
          // Central hub
          _CentralHub(),
          // Radial buttons positioned around center
          ...navItems.asMap().entries.map((entry) {
            final index = entry.key;
            final item = entry.value;
            final angle = (2 * math.pi / navItems.length) * index - math.pi / 2;
            final radius = 140.0;
            return _PositionedRadialButton(
              angle: angle,
              radius: radius,
              label: item['label'] as String,
              icon: item['icon'] as IconData,
              isActive: item['active'] as bool,
            );
          }),
        ],
      ),
    );
  }
}

class _CentralHub extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 120,
      height: 120,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        shape: BoxShape.circle,
        border: Border.all(
          color: CyberpunkColors.orangePrimary,
          width: 2,
        ),
        boxShadow: CyberpunkEffects.glowShadow(intensity: 0.8),
      ),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.center_focus_strong,
            size: 32,
            color: CyberpunkColors.orangePrimary,
          ),
          const SizedBox(height: 4),
          Text(
            'hub',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
      ),
    );
  }
}

class _PositionedRadialButton extends StatelessWidget {
  final double angle;
  final double radius;
  final String label;
  final IconData icon;
  final bool isActive;

  const _PositionedRadialButton({
    required this.angle,
    required this.radius,
    required this.label,
    required this.icon,
    required this.isActive,
  });

  @override
  Widget build(BuildContext context) {
    final x = radius * math.cos(angle);
    final y = radius * math.sin(angle);

    return Positioned(
      left: 200 + x, // Center offset
      top: 200 + y,
      child: Transform.translate(
        offset: Offset(-x, -y),
        child: _RadialNavItem(
          label: label,
          icon: icon,
          isActive: isActive,
        ),
      ),
    );
  }
}

class _RadialNavItem extends StatelessWidget {
  final String label;
  final IconData icon;
  final bool isActive;

  const _RadialNavItem({
    required this.label,
    required this.icon,
    required this.isActive,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () {},
      child: Container(
        width: 70,
        height: 70,
        decoration: BoxDecoration(
          color: isActive
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.2)
              : CyberpunkColors.darkGray.withValues(alpha: 0.8),
          border: Border.all(
            color: isActive
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.lightGray,
            width: isActive ? 2 : 1,
          ),
          shape: BoxShape.circle,
          boxShadow: isActive ? CyberpunkEffects.glowShadow(intensity: 0.5) : null,
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              icon,
              size: 24,
              color: isActive
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.lightGray,
            ),
            const SizedBox(height: 2),
            Text(
              label.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: isActive
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.lightGray,
                fontSize: 9,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _StatusBar extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      height: 28,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: CyberpunkColors.blackTransparent(0.7),
        border: Border(
          top: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          Text(
            '[radial navigation layout]',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const Spacer(),
          Text(
            'verbosity: normal',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.veryLightGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
      ),
    );
  }
}
