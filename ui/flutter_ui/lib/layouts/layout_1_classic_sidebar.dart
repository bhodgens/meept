/// Layout 1: Classic Sidebar Navigation
///
/// Vertical sidebar on the left with nav items, main content area on the right.
/// Chat input stays at bottom of content area.
///
/// Structure:
/// +----------------+------------------------------------------+
/// |  LOGO          |  [Header: Session Title]                 |
/// |                +------------------------------------------+
/// |  chat          |                                          |
/// |  sessions      |           Message List                   |
/// |  plans         |                                          |
/// |  tasks         |                                          |
/// |  agents        |                                          |
/// |                +------------------------------------------+
/// |                |  [Chat Input Area]                       |
/// +----------------+------------------------------------------+
/// |  [Status Bar]                                            |
/// +----------------------------------------------------------+
library;

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

class Layout1ClassicSidebar extends StatelessWidget {
  const Layout1ClassicSidebar({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          // Left Sidebar Navigation
          _Sidebar(),
          // Main Content Area
          Expanded(
            child: Column(
              children: [
                _Header(),
                Expanded(child: _MessageListPlaceholder()),
                _ChatInput(),
                _StatusBar(),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 180,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          // Logo area
          Container(
            height: 50,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(color: CyberpunkColors.orangeDark, width: 2),
              ),
            ),
            child: Text(
              'meept',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
                fontWeight: FontWeight.bold,
              ),
            ),
          ),
          // Navigation items
          const Expanded(
            child: Column(
              children: [
                _NavItem('chat', true),
                _NavItem('sessions', false),
                _NavItem('plans', false),
                _NavItem('tasks', false),
                _NavItem('agents', false),
              ],
            ),
          ),
          // Bottom spacer
          Container(height: 20),
        ],
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  final String label;
  final bool isActive;

  const _NavItem(this.label, this.isActive);

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(vertical: 12),
      decoration: BoxDecoration(
        color: isActive
            ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
            : Colors.transparent,
        border: Border(
          left: BorderSide(
            color: isActive
                ? CyberpunkColors.orangePrimary
                : Colors.transparent,
            width: 3,
          ),
        ),
      ),
      child: Text(
        label.toLowerCase(),
        textAlign: TextAlign.center,
        style: CyberpunkTypography.label.copyWith(
          color: isActive
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.lightGray,
        ),
      ),
    );
  }
}

class _Header extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      color: CyberpunkColors.orangePrimary,
      child: Row(
        children: [
          Text(
            'session name │ description text here...',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.black,
              fontFamily: 'SourceCodePro',
              fontWeight: FontWeight.bold,
            ),
          ),
        ],
      ),
    );
  }
}

class _MessageListPlaceholder extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black.withValues(alpha: 0.5),
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.chat_bubble_outline,
              size: 48,
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.5),
            ),
            const SizedBox(height: 16),
            Text(
              '[message list area]',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ChatInput extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: TextField(
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.veryLightGray,
        ),
        decoration: InputDecoration(
          hintText: 'type a message...',
          hintStyle: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
          filled: true,
          fillColor: CyberpunkColors.midGray,
          border: OutlineInputBorder(
            borderSide: BorderSide(color: CyberpunkColors.orangeDark),
            borderRadius: BorderRadius.circular(4),
          ),
          suffixIcon: Icon(Icons.send, color: CyberpunkColors.orangePrimary),
        ),
        maxLines: 2,
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
            'session: test-session',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontFamily: 'SourceCodePro',
            ),
          ),
          const Spacer(),
          Flexible(
            child: Text(
              '^k focus · / cmd · ^f find',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.veryLightGray,
                fontFamily: 'SourceCodePro',
              ),
              overflow: TextOverflow.ellipsis,
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
