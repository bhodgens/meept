/// Layout 4: Top Tabs with Side Panels
///
/// Traditional top tabs, but with collapsible side panels for tools.
/// Chat input integrated into main content area.
///
/// Structure:
/// +----------------------------------------------------------+
/// |  [Logo] meept     chat | sessions | plans | tasks | agents|
/// +----------------------------------------------------------+
/// |  [Tools]    |                                | [Project]  |
/// |             |                                |            |
/// |  - search   |                                | - branches |
/// |  - memory   |     Main Content Area          | - files    |
/// |  - prompts  |                                | - terminal |
/// |  - settings |                                |            |
/// |             |                                |            |
/// +-------------+--------------------------------+------------+
/// |  [Status Bar]                                            |
/// +----------------------------------------------------------+

import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../theme/effects.dart';

class Layout4TopTabsPanels extends StatefulWidget {
  const Layout4TopTabsPanels({super.key});

  @override
  State<Layout4TopTabsPanels> createState() => _Layout4TopTabsPanelsState();
}

class _Layout4TopTabsPanelsState extends State<Layout4TopTabsPanels> {
  bool _leftPanelExpanded = true;
  bool _rightPanelExpanded = true;
  int _selectedTab = 0;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          _HeaderWithTabs(),
          Expanded(
            child: Row(
              children: [
                if (_leftPanelExpanded) _LeftPanel(),
                _ToggleButtons(),
                Expanded(
                  child: _MainContentArea(tabIndex: _selectedTab),
                ),
                if (_rightPanelExpanded) _RightPanel(),
              ],
            ),
          ),
          _StatusBar(),
        ],
      ),
    );
  }

  Widget _ToggleButtons() {
    return Column(
      children: [
        _ToggleButton(
          icon: Icons.chevron_left,
          isActive: _leftPanelExpanded,
          onTap: () => setState(() => _leftPanelExpanded = !_leftPanelExpanded),
        ),
        const SizedBox(height: 8),
        _ToggleButton(
          icon: Icons.chevron_right,
          isActive: _rightPanelExpanded,
          onTap: () => setState(() => _rightPanelExpanded = !_rightPanelExpanded),
        ),
      ],
    );
  }
}

class _HeaderWithTabs extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Top bar with logo
        Container(
          height: 40,
          padding: const EdgeInsets.symmetric(horizontal: 16),
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            border: Border(
              bottom: BorderSide(
                color: CyberpunkColors.orangeDark,
                width: 1,
              ),
            ),
          ),
          child: Row(
            children: [
              Text(
                'meept',
                style: CyberpunkTypography.headlineSmall.copyWith(
                  color: CyberpunkColors.orangePrimary,
                  fontWeight: FontWeight.bold,
                  letterSpacing: 2,
                ),
              ),
              const Spacer(),
              _ConnectionIndicator(),
              const SizedBox(width: 16),
            ],
          ),
        ),
        // Tab bar
        Container(
          decoration: BoxDecoration(
            color: CyberpunkColors.black,
            border: Border(
              bottom: BorderSide(
                color: CyberpunkColors.orangePrimary,
                width: 2,
              ),
            ),
          ),
          child: Row(
            children: [
              _TabButton('chat', 0),
              _TabButton('sessions', 1),
              _TabButton('plans', 2),
              _TabButton('tasks', 3),
              _TabButton('agents', 4),
            ],
          ),
        ),
      ],
    );
  }
}

class _TabButton extends StatelessWidget {
  final String label;
  final int index;

  const _TabButton(this.label, this.index);

  @override
  Widget build(BuildContext context) {
    final selectedTab = (context.findAncestorStateOfType<_Layout4TopTabsPanelsState>())?._selectedTab ?? 0;
    final isSelected = selectedTab == index;

    return Expanded(
      child: GestureDetector(
        onTap: () {
          final state = context.findAncestorStateOfType<_Layout4TopTabsPanelsState>();
          state?.setState(() => state._selectedTab = index);
        },
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 12),
          decoration: BoxDecoration(
            color: isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                : Colors.transparent,
            border: Border(
              bottom: BorderSide(
                color: isSelected ? CyberpunkColors.orangePrimary : Colors.transparent,
                width: 2,
              ),
            ),
          ),
          child: Text(
            label.toLowerCase(),
            textAlign: TextAlign.center,
            style: CyberpunkTypography.label.copyWith(
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.lightGray,
              letterSpacing: 1.5,
            ),
          ),
        ),
      ),
    );
  }
}

class _ConnectionIndicator extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(
            color: CyberpunkColors.greenSuccess,
            shape: BoxShape.circle,
            boxShadow: [
              BoxShadow(
                color: CyberpunkColors.greenSuccess.withValues(alpha: 0.5),
                blurRadius: 8,
              ),
            ],
          ),
        ),
        const SizedBox(width: 6),
        Text(
          'online',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.greenSuccess,
            fontFamily: 'SourceCodePro',
          ),
        ),
      ],
    );
  }
}

class _LeftPanel extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 160,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _PanelHeader('tools'),
          _ToolItem(Icons.search, 'search'),
          _ToolItem(Icons.memory, 'memory'),
          _ToolItem(Icons.lightbulb, 'prompts'),
          _ToolItem(Icons.psychology, 'reflection'),
          const Divider(color: CyberpunkColors.midGray),
          _ToolItem(Icons.settings, 'settings'),
          _ToolItem(Icons.extension, 'skills'),
        ],
      ),
    );
  }
}

class _RightPanel extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 180,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border(
          left: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _PanelHeader('project'),
          _ProjectInfo(),
          const Divider(color: CyberpunkColors.midGray),
          _PanelHeader('quick access'),
          _ToolItem(Icons.call_split, 'branches'),
          _ToolItem(Icons.folder, 'files'),
          _ToolItem(Icons.terminal, 'terminal'),
        ],
      ),
    );
  }
}

class _PanelHeader extends StatelessWidget {
  final String title;

  const _PanelHeader(this.title);

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            width: 1,
          ),
        ),
      ),
      child: Text(
        title.toUpperCase(),
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.orangePrimary,
          fontWeight: FontWeight.bold,
          letterSpacing: 1,
          fontFamily: 'SourceCodePro',
        ),
      ),
    );
  }
}

class _ToolItem extends StatelessWidget {
  final IconData icon;
  final String label;

  const _ToolItem(this.icon, this.label);

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(
        icon,
        size: 18,
        color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
      ),
      title: Text(
        label.toLowerCase(),
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.lightGray,
          fontFamily: 'SourceCodePro',
        ),
      ),
      onTap: () {},
    );
  }
}

class _ProjectInfo extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                Icons.folder,
                size: 16,
                color: CyberpunkColors.orangePrimary,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'meept',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontWeight: FontWeight.bold,
                    fontFamily: 'SourceCodePro',
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            'branch: main',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
              fontSize: 10,
            ),
          ),
        ],
      ),
    );
  }
}

class _ToggleButton extends StatelessWidget {
  final IconData icon;
  final bool isActive;
  final VoidCallback onTap;

  const _ToggleButton({
    required this.icon,
    required this.isActive,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        width: 24,
        height: 24,
        decoration: BoxDecoration(
          color: isActive
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.3)
              : CyberpunkColors.darkGray,
          border: Border.all(
            color: isActive
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.midGray,
            width: 1,
          ),
        ),
        child: Icon(
          icon,
          size: 16,
          color: isActive
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.lightGray,
        ),
      ),
    );
  }
}

class _MainContentArea extends StatelessWidget {
  final int tabIndex;

  const _MainContentArea({required this.tabIndex});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        image: DecorationImage(
          image: AssetImage('assets/images/gui-bg.png'),
          fit: BoxFit.cover,
          opacity: 0.05,
        ),
      ),
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              '[${_tabName(tabIndex)} content]',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 16),
            _ChatInputPlaceholder(),
          ],
        ),
      ),
    );
  }

  String _tabName(int index) {
    const names = ['chat', 'sessions', 'plans', 'tasks', 'agents'];
    return index >= 0 && index < names.length ? names[index] : 'unknown';
  }
}

class _ChatInputPlaceholder extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Container(
      width: 500,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.8),
        border: Border.all(
          color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        children: [
          Text(
            '[chat input area - type messages here]',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ],
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
            '[panel layout: left/right collapsible]',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
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
