/// Layout 2: Bottom Navigation Bar
///
/// Horizontal nav bar at the bottom, content fills the rest.
/// Chat input appears only on chat tab, hidden on others.
///
/// Structure:
/// +----------------------------------------------------------+
/// |  [Header: Session Title + Tool Actions]                  |
/// +----------------------------------------------------------+
/// |                                                          |
/// |                  Main Content Area                       |
/// |            (chat messages / sessions list /              |
/// |             plans / tasks / agents based on tab)         |
/// |                                                          |
/// +----------------------------------------------------------+
/// |        |         |         |         |                   |
/// |  chat  |sessions |  plans  |  tasks  |     agents        |
/// |--------+---------+---------+---------+-------------------|
/// |  [Status Bar]                                            |
/// +----------------------------------------------------------+
library;

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../theme/effects.dart';

class Layout2BottomNav extends StatelessWidget {
  const Layout2BottomNav({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          _Header(),
          Expanded(child: _ContentArea()),
          _BottomNavigation(),
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
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      decoration: BoxDecoration(
        gradient: CyberpunkEffects.angularGradient,
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.orangePrimary, width: 2),
        ),
      ),
      child: Row(
        children: [
          Text(
            'meept',
            style: CyberpunkTypography.headlineSmall.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(width: 24),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
            decoration: BoxDecoration(
              color: CyberpunkColors.midGray,
              border: Border.all(
                color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
              ),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.folder,
                  size: 14,
                  color: CyberpunkColors.orangePrimary,
                ),
                const SizedBox(width: 8),
                Text(
                  '~/projects/meept',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ],
            ),
          ),
          const Spacer(),
          const _HeaderAction(Icons.search, 'find'),
          const _HeaderAction(Icons.call_split, 'branches'),
          const _HeaderAction(Icons.menu, 'menu'),
        ],
      ),
    );
  }
}

class _HeaderAction extends StatelessWidget {
  final IconData icon;
  final String label;

  const _HeaderAction(this.icon, this.label);

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(left: 8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
        ),
      ),
      child: Icon(icon, size: 18, color: CyberpunkColors.orangePrimary),
    );
  }
}

class _ContentArea extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        image: DecorationImage(
          image: AssetImage('assets/images/gui-bg.png'),
          fit: BoxFit.cover,
          opacity: 0.1,
        ),
      ),
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Mock chat bubbles to show layout
            const _ChatBubble(isUser: false, text: 'welcome to meept'),
            const SizedBox(height: 8),
            const _ChatBubble(isUser: true, text: 'create a new plan'),
            const SizedBox(height: 8),
            const _ChatBubble(
              isUser: false,
              text: 'processing your request...',
            ),
            const SizedBox(height: 24),
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: CyberpunkColors.midGray.withValues(alpha: 0.3),
                border: Border.all(
                  color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
                  width: 1,
                ),
              ),
              child: Text(
                '[tab content: chat / sessions / plans / tasks / agents]',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.midGray,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChatBubble extends StatelessWidget {
  final bool isUser;
  final String text;

  const _ChatBubble({required this.isUser, required this.text});

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 400),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        decoration: BoxDecoration(
          color: isUser
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.3)
              : CyberpunkColors.darkGray.withValues(alpha: 0.5),
          border: Border.all(
            color: isUser
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.lightGray,
            width: 1,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Text(
          text,
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: isUser
                ? CyberpunkColors.black
                : CyberpunkColors.veryLightGray,
          ),
        ),
      ),
    );
  }
}

class _BottomNavigation extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(color: CyberpunkColors.orangePrimary, width: 2),
        ),
      ),
      child: const Row(
        children: [
          _NavButton('chat', Icons.chat, true),
          _NavButton('sessions', Icons.folder, false),
          _NavButton('plans', Icons.document_scanner, false),
          _NavButton('tasks', Icons.task, false),
          _NavButton('agents', Icons.smart_toy, false),
        ],
      ),
    );
  }
}

class _NavButton extends StatelessWidget {
  final String label;
  final IconData icon;
  final bool isActive;

  const _NavButton(this.label, this.icon, this.isActive);

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: InkWell(
        onTap: () {},
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 16),
          decoration: BoxDecoration(
            gradient: isActive
                ? LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: [
                      CyberpunkColors.orangePrimary.withValues(alpha: 0.2),
                      Colors.transparent,
                    ],
                  )
                : null,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                icon,
                color: isActive
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.lightGray,
              ),
              const SizedBox(height: 4),
              Text(
                label.toLowerCase(),
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: isActive
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.lightGray,
                  letterSpacing: 1,
                ),
              ),
            ],
          ),
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
          const _StatusDot(connected: true),
          const SizedBox(width: 6),
          Text(
            'connected',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.greenSuccess,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const SizedBox(width: 16),
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

class _StatusDot extends StatelessWidget {
  final bool connected;
  const _StatusDot({required this.connected});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(
        color: connected
            ? CyberpunkColors.greenSuccess
            : CyberpunkColors.redAlert,
        shape: BoxShape.circle,
      ),
    );
  }
}
