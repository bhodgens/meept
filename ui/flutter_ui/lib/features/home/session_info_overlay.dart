/// Session Info Overlay - Tabbed panel showing session-scoped data
///
/// Opens when clicking the [i] icon next to a session in the sidebar.
/// Shows four tabs: sessions (2-pane), plans, tasks, agents. The tab bodies
/// are the SAME canonical widgets used by the main window (see TabContent);
/// the popup is only a shell (header + lowercase tab bar + IndexedStack).
///
/// ```
/// ┌─────────────────────────────────────────────────────────────────┐
/// │  Session: session4                          [X]                 │
/// ├─────────────────────────────────────────────────────────────────┤
/// │  sessions │ plans │ tasks │ agents                              │
/// ├───────────┴───────┴───────┴─────────────────────────────────────┤
/// │                                                                   │
/// │  [Tab content]                                                  │
/// │                                                                   │
/// └─────────────────────────────────────────────────────────────────┘
/// ```
library;

import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../sessions/sessions_overview_tab.dart';
import '../plans/plans_tab.dart';
import '../tasks/tasks_tab.dart';
import '../agents/agents_tab.dart';

/// Session info overlay dialog
class SessionInfoOverlay extends StatefulWidget {
  final Session session;

  const SessionInfoOverlay({super.key, required this.session});

  @override
  State<SessionInfoOverlay> createState() => _SessionInfoOverlayState();
}

class _SessionInfoOverlayState extends State<SessionInfoOverlay>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;
  int _selectedTabIndex = 0;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 4, vsync: this);
    _tabController.addListener(() {
      setState(() => _selectedTabIndex = _tabController.index);
    });
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      backgroundColor: Colors.transparent,
      insetPadding: EdgeInsets.zero,
      child: SizedBox.expand(
        child: Container(
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            border: Border.all(
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
              width: 1,
            ),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Column(
            children: [
              // Header with session name and close button
              _Header(
                session: widget.session,
                onClose: () => Navigator.pop(context),
              ),
              // Tab bar
              _TabBar(
                tabs: const ['sessions', 'plans', 'tasks', 'agents'],
                selectedIndex: _selectedTabIndex,
                onTap: (index) => _tabController.animateTo(index),
              ),
              // Tab content: canonical widgets shared with the main window.
              // Each loads ALL rows in initState, so session-scoped filtering
              // does not silently hide cross-session rows (parity with TUI).
              Expanded(
                // The canonical tabs contain ListTiles (e.g. _PlanTile).
                // Popup's dark-gray Container is a DecoratedBox with a
                // background, so give tab content its own Material ancestor
                // below it — otherwise Flutter asserts that ListTile ink /
                // selectedTileColor is hidden by that DecoratedBox.
                child: Material(
                  color: Colors.transparent,
                  child: IndexedStack(
                    index: _selectedTabIndex,
                    children: const [
                      SessionsOverviewTab(),
                      PlansTab(),
                      TasksTab(),
                      AgentsTab(),
                    ],
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

/// Header with session name and close button
class _Header extends StatelessWidget {
  final Session session;
  final VoidCallback onClose;

  const _Header({required this.session, required this.onClose});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.orangePrimary, width: 2),
        ),
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              'session: ${session.title.isEmpty ? 'unnamed' : session.title}',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
          GestureDetector(
            onTap: onClose,
            child: Container(
              padding: const EdgeInsets.all(4),
              decoration: BoxDecoration(
                border: Border.all(
                  color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
                ),
              ),
              child: Icon(
                Icons.close,
                size: 16,
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Custom tab bar
class _TabBar extends StatelessWidget {
  final List<String> tabs;
  final int selectedIndex;
  final ValueChanged<int> onTap;

  const _TabBar({
    required this.tabs,
    required this.selectedIndex,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: tabs.asMap().entries.map((entry) {
          final index = entry.key;
          final label = entry.value;
          final isSelected = index == selectedIndex;

          return Expanded(
            child: GestureDetector(
              onTap: () => onTap(index),
              child: Container(
                padding: const EdgeInsets.symmetric(vertical: 10),
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
                child: Text(
                  label.toLowerCase(),
                  textAlign: TextAlign.center,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: isSelected
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.lightGray,
                    fontFamily: 'SourceCodePro',
                    letterSpacing: 1,
                  ),
                ),
              ),
            ),
          );
        }).toList(),
      ),
    );
  }
}
