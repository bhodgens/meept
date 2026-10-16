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
library;

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/providers.dart';
import '../services/websocket_service.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Tab flash state for the top tab bar (GUI parity with the TUI's
/// tabFlash: a '●' marks a tab whose task/plan event stream delivered an
/// event while it was not the active tab; the dot clears when the tab is
/// activated). ValueNotifier-based so the layout needs no provider
/// plumbing; one notifier instance per [TabFlashController].
class TabFlashController {
  /// Per-tab unread/event dots. Index matches the tab bar index.
  final List<ValueNotifier<bool>> tabs;

  TabFlashController({int tabCount = 5})
    : tabs = List.generate(tabCount, (_) => ValueNotifier<bool>(false));

  /// Map a tab label to its notifier index (null when unknown).
  int? indexOf(String label) {
    const names = ['chat', 'sessions', 'plans', 'tasks', 'agents'];
    final i = names.indexOf(label);
    return i >= 0 ? i : null;
  }

  /// Mark [index] as having unseen events (only meaningful when it is not
  /// the active tab; the caller decides).
  void flash(int index) {
    if (index >= 0 && index < tabs.length) tabs[index].value = true;
  }

  /// Clear the dot on [index] (tab activation).
  void clear(int index) {
    if (index >= 0 && index < tabs.length) tabs[index].value = false;
  }

  void dispose() {
    for (final t in tabs) {
      t.dispose();
    }
  }
}

/// Feeds a [TabFlashController] from the websocket message stream: any
/// task/plan event ('job_update', 'plan_update') arriving while its tab is
/// not active sets that tab's dot. Mirrors the TUI app.go tabFlash wiring
/// (task events flash ViewTasks, plan events flash ViewPlans).
///
/// Returns a subscription the caller must cancel on dispose.
StreamSubscription<Map<String, dynamic>> bindTabFlash({
  required WebSocketService websocket,
  required TabFlashController controller,
  required int Function() activeTabIndex,
}) {
  return websocket.messageStream.listen((msg) {
    final type = msg['type'] as String?;
    final target = switch (type) {
      WsEventTypes.jobUpdate => 3, // tasks tab
      WsEventTypes.planUpdate => 2, // plans tab
      _ => null,
    };
    if (target == null) return;
    if (activeTabIndex() == target) return;
    controller.flash(target);
  });
}

class Layout4TopTabsPanels extends ConsumerStatefulWidget {
  const Layout4TopTabsPanels({super.key});

  @override
  ConsumerState<Layout4TopTabsPanels> createState() =>
      _Layout4TopTabsPanelsState();
}

class _Layout4TopTabsPanelsState extends ConsumerState<Layout4TopTabsPanels> {
  bool _leftPanelExpanded = true;
  bool _rightPanelExpanded = true;
  int _selectedTab = 0;
  late final TabFlashController _tabFlash;
  StreamSubscription<Map<String, dynamic>>? _flashSub;

  @override
  void initState() {
    super.initState();
    _tabFlash = TabFlashController();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Bind the flash feed to the app's websocket once a provider scope is
    // above this widget. Tolerates preview/test trees without a
    // ProviderScope (the layout still renders; only the dot feed is off).
    if (_flashSub == null) {
      try {
        final ws = ref.read(websocketProvider);
        _flashSub = bindTabFlash(
          websocket: ws,
          controller: _tabFlash,
          activeTabIndex: () => _selectedTab,
        );
      } catch (_) {
        // No ProviderScope above (layout preview, smoke tests): the tab
        // bar renders without the event-dot feed.
      }
    }
  }

  @override
  void dispose() {
    _flashSub?.cancel();
    _tabFlash.dispose();
    super.dispose();
  }

  void selectTab(int index) {
    setState(() => _selectedTab = index);
    // Clear the event dot on activation (TUI tabFlash semantics).
    _tabFlash.clear(index);
  }

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
                _buildToggleButtons(),
                Expanded(child: _MainContentArea(tabIndex: _selectedTab)),
                if (_rightPanelExpanded) _RightPanel(),
              ],
            ),
          ),
          _StatusBar(),
        ],
      ),
    );
  }

  Widget _buildToggleButtons() {
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
          onTap: () =>
              setState(() => _rightPanelExpanded = !_rightPanelExpanded),
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
              bottom: BorderSide(color: CyberpunkColors.orangeDark, width: 1),
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
          child: Builder(
            builder: (context) {
              final flash = context
                  .findAncestorStateOfType<_Layout4TopTabsPanelsState>()
                  ?._tabFlash;
              return Row(
                children: [
                  _TabButton('chat', 0, flash: flash),
                  _TabButton('sessions', 1, flash: flash),
                  _TabButton('plans', 2, flash: flash),
                  _TabButton('tasks', 3, flash: flash),
                  _TabButton('agents', 4, flash: flash),
                ],
              );
            },
          ),
        ),
      ],
    );
  }
}

class _TabButton extends StatelessWidget {
  final String label;
  final int index;
  final TabFlashController? flash;

  const _TabButton(this.label, this.index, {this.flash});

  @override
  Widget build(BuildContext context) {
    final selectedTab =
        (context.findAncestorStateOfType<_Layout4TopTabsPanelsState>())
            ?._selectedTab ??
        0;
    final isSelected = selectedTab == index;
    final dot = flash?.tabs[index];

    return Expanded(
      child: GestureDetector(
        onTap: () {
          context
              .findAncestorStateOfType<_Layout4TopTabsPanelsState>()
              ?.selectTab(index);
        },
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 12),
          decoration: BoxDecoration(
            color: isSelected
                ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                : Colors.transparent,
            border: Border(
              bottom: BorderSide(
                color: isSelected
                    ? CyberpunkColors.orangePrimary
                    : Colors.transparent,
                width: 2,
              ),
            ),
          ),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                label.toLowerCase(),
                textAlign: TextAlign.center,
                style: CyberpunkTypography.label.copyWith(
                  color: isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.lightGray,
                  letterSpacing: 1.5,
                ),
              ),
              // Unread-event dot (TUI tabFlash '●' parity): set by
              // bindTabFlash when task/plan events arrive on this tab
              // while inactive; cleared on activation (selectTab).
              if (dot != null)
                ValueListenableBuilder<bool>(
                  valueListenable: dot,
                  builder: (context, hasDot, _) => Visibility(
                    visible: hasDot,
                    maintainSize: true,
                    maintainAnimation: true,
                    maintainState: true,
                    child: Padding(
                      padding: const EdgeInsets.only(left: 4),
                      child: Icon(
                        Icons.circle,
                        size: 6,
                        color: CyberpunkColors.orangePrimary,
                      ),
                    ),
                  ),
                ),
            ],
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
          const _PanelHeader('tools'),
          const _ToolItem(Icons.search, 'search'),
          const _ToolItem(Icons.memory, 'memory'),
          const _ToolItem(Icons.lightbulb, 'prompts'),
          const _ToolItem(Icons.psychology, 'reflection'),
          Divider(color: CyberpunkColors.midGray),
          const _ToolItem(Icons.settings, 'settings'),
          const _ToolItem(Icons.extension, 'skills'),
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
          const _PanelHeader('project'),
          _ProjectInfo(),
          Divider(color: CyberpunkColors.midGray),
          const _PanelHeader('quick access'),
          const _ToolItem(Icons.call_split, 'branches'),
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
    // Own Material: the parent panel's DecoratedBox paints over the
    // nearest Material, which hides ListTile ink/background.
    return Material(
      color: Colors.transparent,
      child: ListTile(
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
      ),
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
      decoration: const BoxDecoration(
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
