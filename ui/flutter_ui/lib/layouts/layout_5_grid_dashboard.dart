/// Layout 5: Grid Dashboard
///
/// All 5 main sections visible as cards in a grid dashboard.
/// Clicking a card expands to full_content view.
/// Chat input appears as a floating action button or bottom bar.
///
/// Structure:
/// +----------------------------------------------------------+
/// |  [Logo] meept                           [Status Pill]    |
/// +----------------------------------------------------------+
/// |                                                          |
/// |   +--------+  +--------+  +--------+                     |
/// |   | chat   |  |sessions|  | plans  |                     |
/// |   | [icon] |  | [icon] |  | [icon] |                     |
/// |   | preview|  | preview|  | preview|                     |
/// |   +--------+  +--------+  +--------+                     |
/// |                                                          |
/// |   +--------+  +--------+                                 |
/// |   | tasks  |  | agents |                                 |
/// |   | [icon] |  | [icon] |                                 |
/// |   | preview|  | preview|                                 |
/// |   +--------+  +--------+                                 |
/// |                                                          |
/// +----------------------------------------------------------+
/// |  [Floating Chat Bar]                                     |
/// +----------------------------------------------------------+
/// |  [Status Bar]                                            |
/// +----------------------------------------------------------+
library;

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../theme/effects.dart';

class Layout5GridDashboard extends StatelessWidget {
  const Layout5GridDashboard({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          _Header(),
          Expanded(child: _DashboardGrid()),
          _FloatingChatBar(),
          _StatusBar(),
        ],
      ),
    );
  }
}

class _Header extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      height: 60,
      padding: const EdgeInsets.symmetric(horizontal: 24),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.topRight,
          colors: [
            CyberpunkColors.darkGray,
            CyberpunkColors.darkGray.withValues(alpha: 0.5),
          ],
        ),
        border: const Border(
          bottom: BorderSide(color: CyberpunkColors.orangePrimary, width: 3),
        ),
      ),
      child: Row(
        children: [
          // Logo with angled accent
          Stack(
            children: [
              CustomPaint(
                size: const Size(40, 60),
                painter: _LogoAccentPainter(),
              ),
              Padding(
                padding: const EdgeInsets.only(left: 12),
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'meept',
                      style: CyberpunkTypography.headlineMedium.copyWith(
                        color: CyberpunkColors.orangePrimary,
                        fontWeight: FontWeight.bold,
                        letterSpacing: 3,
                      ),
                    ),
                    Text(
                      'agent os',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.lightGray,
                        fontSize: 9,
                        letterSpacing: 2,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const Spacer(),
          // Status indicators
          _StatusPill(),
          const SizedBox(width: 16),
          // User avatar
          CircleAvatar(
            radius: 18,
            backgroundColor: CyberpunkColors.orangePrimary.withValues(
              alpha: 0.2,
            ),
            child: const Icon(
              Icons.person,
              size: 20,
              color: CyberpunkColors.orangePrimary,
            ),
          ),
        ],
      ),
    );
  }
}

class _LogoAccentPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = CyberpunkColors.orangePrimary.withValues(alpha: 0.3)
      ..style = PaintingStyle.fill;

    final path = Path();
    path.moveTo(0, 0);
    path.lineTo(size.width, 0);
    path.lineTo(size.width, size.height);
    path.lineTo(10, size.height);
    path.close();

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}

class _StatusPill extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(
          color: CyberpunkColors.greenSuccess.withValues(alpha: 0.5),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(20),
        boxShadow: [
          BoxShadow(
            color: CyberpunkColors.greenSuccess.withValues(alpha: 0.2),
            blurRadius: 8,
          ),
        ],
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: CyberpunkColors.greenSuccess,
              shape: BoxShape.circle,
              boxShadow: CyberpunkEffects.glowShadow(intensity: 0.8),
            ),
          ),
          const SizedBox(width: 8),
          Text(
            'daemon connected',
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

class _DashboardGrid extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final cards = [
      const _DashboardCard(
        title: 'chat',
        icon: Icons.chat_bubble_outline,
        preview: 'active conversation',
        accentColor: CyberpunkColors.orangePrimary,
      ),
      const _DashboardCard(
        title: 'sessions',
        icon: Icons.folder_open,
        preview: '12 archived',
        accentColor: CyberpunkColors.blueInfo,
      ),
      const _DashboardCard(
        title: 'plans',
        icon: Icons.document_scanner,
        preview: '3 in progress',
        accentColor: CyberpunkColors.cyanAccent,
      ),
      const _DashboardCard(
        title: 'tasks',
        icon: Icons.task_alt,
        preview: '24 pending',
        accentColor: CyberpunkColors.yellowWarning,
      ),
      const _DashboardCard(
        title: 'agents',
        icon: Icons.smart_toy,
        preview: '5 active',
        accentColor: CyberpunkColors.greenSuccess,
      ),
    ];

    return Padding(
      padding: const EdgeInsets.all(24),
      child: GridView.count(
        crossAxisCount: 3,
        mainAxisSpacing: 16,
        crossAxisSpacing: 16,
        childAspectRatio: 1.2,
        children: cards,
      ),
    );
  }
}

class _DashboardCard extends StatelessWidget {
  final String title;
  final IconData icon;
  final String preview;
  final Color accentColor;

  const _DashboardCard({
    required this.title,
    required this.icon,
    required this.preview,
    required this.accentColor,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () {},
      child: Container(
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
          border: Border.all(
            color: accentColor.withValues(alpha: 0.3),
            width: 1,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Stack(
          children: [
            // Angled corner accent
            Positioned(
              top: 0,
              right: 0,
              child: CustomPaint(
                size: const Size(30, 30),
                painter: _CornerAccentPainter(color: accentColor),
              ),
            ),
            // Content
            Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // Icon with glow
                  Container(
                    padding: const EdgeInsets.all(12),
                    decoration: BoxDecoration(
                      color: accentColor.withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(8),
                      boxShadow: [
                        BoxShadow(
                          color: accentColor.withValues(alpha: 0.3),
                          blurRadius: 8,
                        ),
                      ],
                    ),
                    child: Icon(icon, size: 32, color: accentColor),
                  ),
                  const Spacer(),
                  Text(
                    title.toUpperCase(),
                    style: CyberpunkTypography.label.copyWith(
                      color: accentColor,
                      letterSpacing: 2,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    preview,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.lightGray,
                      fontFamily: 'SourceCodePro',
                    ),
                  ),
                ],
              ),
            ),
            // Hover/active border glow
            Positioned.fill(
              child: Container(
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: accentColor.withValues(alpha: 0.0),
                    width: 2,
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _CornerAccentPainter extends CustomPainter {
  final Color color;

  _CornerAccentPainter({required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color.withValues(alpha: 0.5)
      ..style = PaintingStyle.fill;

    final path = Path();
    path.moveTo(size.width, 0);
    path.lineTo(size.width, 10);
    path.lineTo(10, size.height);
    path.lineTo(size.width, size.height);
    path.close();

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant _CornerAccentPainter oldDelegate) => false;
}

class _FloatingChatBar extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
      child: Container(
        height: 60,
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray.withValues(alpha: 0.9),
          border: Border.all(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.5),
            width: 2,
          ),
          borderRadius: BorderRadius.circular(8),
          boxShadow: CyberpunkEffects.glowShadow(intensity: 0.5),
        ),
        child: Row(
          children: [
            Expanded(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Text(
                  'quick message...',
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.midGray,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ),
            ),
            Container(width: 1, height: 40, color: CyberpunkColors.orangeDark),
            IconButton(
              icon: Icon(
                Icons.attach_file,
                color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
              ),
              onPressed: () {},
            ),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: CyberpunkColors.orangePrimary,
                borderRadius: BorderRadius.circular(4),
              ),
              child: const Icon(
                Icons.send,
                color: CyberpunkColors.black,
                size: 20,
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
        border: const Border(
          top: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          Text(
            '[dashboard view]',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const SizedBox(width: 16),
          Text(
            '|',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangeDark,
            ),
          ),
          const SizedBox(width: 16),
          Text(
            'project: meept [main]',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const Spacer(),
          Text(
            '^k · ^f · ^v',
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
