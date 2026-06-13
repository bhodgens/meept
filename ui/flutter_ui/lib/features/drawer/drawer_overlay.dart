import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../providers/providers.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'panels/status_panel.dart';
import 'panels/agent_activity_panel.dart';
import 'panels/tasks_panel.dart';
import 'panels/recent_memory_panel.dart';
import '../metrics/metrics_panel.dart';

/// Drawer panel tabs.
enum DrawerTab { status, agents, tasks, memory, metrics }

/// Full-height drawer overlay that slides from the left.
/// Dismissed by clicking outside or pressing esc.
class DrawerOverlay extends ConsumerStatefulWidget {
  const DrawerOverlay({super.key});

  @override
  ConsumerState<DrawerOverlay> createState() => _DrawerOverlayState();
}

class _DrawerOverlayState extends ConsumerState<DrawerOverlay>
    with SingleTickerProviderStateMixin {
  late AnimationController _controller;
  late Animation<Offset> _slideAnimation;
  DrawerTab _selectedTab = DrawerTab.status;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 250),
    );
    _slideAnimation = Tween<Offset>(
      begin: const Offset(-1.0, 0.0),
      end: Offset.zero,
    ).animate(CurvedAnimation(parent: _controller, curve: Curves.easeOut));
    _controller.forward();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _close() {
    _controller.reverse().then((_) {
      if (mounted) {
        ref.read(drawerOpenProvider.notifier).state = false;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: _close,
      behavior: HitTestBehavior.opaque,
      child: Stack(
        children: [
          // Semi-transparent backdrop
          Container(
            color: CyberpunkColors.blackTransparent(0.5),
          ),
          // Sliding drawer panel
          SlideTransition(
            position: _slideAnimation,
            child: GestureDetector(
              onTap: () {}, // Prevent taps from propagating to backdrop
              child: Container(
                width: 320,
                height: double.infinity,
                decoration: BoxDecoration(
                  color: CyberpunkColors.darkGray,
                  border: Border(
                    right: BorderSide(
                      color: CyberpunkColors.orangePrimary,
                      width: 1,
                    ),
                  ),
                ),
                child: Column(
                  children: [
                    _buildTabBar(),
                    const Divider(height: 1, color: CyberpunkColors.midGray),
                    Expanded(child: _buildPanel()),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTabBar() {
    final tabs = [
      (DrawerTab.status, 'status'),
      (DrawerTab.agents, 'agents'),
      (DrawerTab.tasks, 'tasks'),
      (DrawerTab.memory, 'memory'),
      (DrawerTab.metrics, 'metrics'),
    ];

    return Container(
      padding: const EdgeInsets.all(8),
      child: Row(
        children: [
          for (final (tab, label) in tabs)
            GestureDetector(
              onTap: () => setState(() => _selectedTab = tab),
              child: Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                margin: const EdgeInsets.only(right: 4),
                decoration: BoxDecoration(
                  color: _selectedTab == tab
                      ? CyberpunkColors.orangePrimary.withValues(alpha: 0.2)
                      : Colors.transparent,
                  border: Border.all(
                    color: _selectedTab == tab
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.midGray,
                    width: 1,
                  ),
                  borderRadius: BorderRadius.circular(3),
                ),
                child: Text(
                  label,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: _selectedTab == tab
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.lightGray,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ),
            ),
          const Spacer(),
          GestureDetector(
            onTap: _close,
            child: const Icon(
              Icons.close,
              size: 16,
              color: CyberpunkColors.orangePrimary,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildPanel() {
    switch (_selectedTab) {
      case DrawerTab.status:
        return const StatusPanel();
      case DrawerTab.agents:
        return const AgentActivityPanel();
      case DrawerTab.tasks:
        return const TasksPanel();
      case DrawerTab.memory:
        return const RecentMemoryPanel();
      case DrawerTab.metrics:
        return const MetricsPanel(compact: true);
    }
  }
}
