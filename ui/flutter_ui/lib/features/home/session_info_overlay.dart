/// Session Info Overlay - Tabbed panel showing session-scoped data
///
/// Opens when clicking the [i] icon next to a session in the sidebar.
/// Shows three tabs: plans, tasks, agents - all scoped to the selected session.
///
/// ```
/// ┌─────────────────────────────────────────────────────────────────┐
/// │  Session: session4                          [X]                 │
/// ├─────────────────────────────────────────────────────────────────┤
/// │  plans  │  tasks  │  agents                                     │
/// ├─────────┴─────────┴─────────────────────────────────────────────┤
/// │                                                                   │
/// │  [Tab content - session-scoped lists]                          │
/// │                                                                   │
/// └─────────────────────────────────────────────────────────────────┘
/// ```

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../../widgets/background_image.dart';
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
    _tabController = TabController(length: 3, vsync: this);
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
      insetPadding: const EdgeInsets.all(24),
      child: Container(
        width: 600,
        height: 500,
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
            _Header(session: widget.session, onClose: () => Navigator.pop(context)),
            // Tab bar
            _TabBar(
              tabs: const ['plans', 'tasks', 'agents'],
              selectedIndex: _selectedTabIndex,
              onTap: (index) => _tabController.animateTo(index),
            ),
            // Tab content
            Expanded(
              child: IndexedStack(
                index: _selectedTabIndex,
                children: [
                  _PlansTabContent(sessionId: widget.session.id),
                  _TasksTabContent(sessionId: widget.session.id),
                  _AgentsTabContent(sessionId: widget.session.id),
                ],
              ),
            ),
          ],
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
          bottom: BorderSide(
            color: CyberpunkColors.orangePrimary,
            width: 2,
          ),
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
          bottom: BorderSide(
            color: CyberpunkColors.midGray,
            width: 1,
          ),
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

/// Plans tab content - session scoped
class _PlansTabContent extends ConsumerWidget {
  final String sessionId;

  const _PlansTabContent({required this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // TODO Phase 5: Filter plans by session ID
    final plansState = ref.watch(planProvider);

    if (plansState.plans.isEmpty) {
      return const _EmptyState(
        icon: Icons.document_scanner,
        message: 'no plans for this session',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: plansState.plans.length,
      itemBuilder: (context, index) {
        final plan = plansState.plans[index];
        return _PlanItem(plan: plan);
      },
    );
  }
}

/// Single plan item in the list
class _PlanItem extends StatelessWidget {
  final dynamic plan; // TODO: Use proper Plan type

  const _PlanItem({required this.plan});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            plan.title ?? 'Untitled Plan',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontFamily: 'SourceCodePro',
            ),
          ),
          if (plan.description != null && plan.description!.isNotEmpty)
            const SizedBox(height: 4),
          if (plan.description != null && plan.description!.isNotEmpty)
            Text(
              plan.description!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
        ],
      ),
    );
  }
}

/// Tasks tab content - session scoped
class _TasksTabContent extends ConsumerWidget {
  final String sessionId;

  const _TasksTabContent({required this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // TODO Phase 5: Filter tasks by session ID
    final tasksState = ref.watch(taskProvider);

    if (tasksState.tasks.isEmpty) {
      return const _EmptyState(
        icon: Icons.task_alt,
        message: 'no tasks for this session',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: tasksState.tasks.length,
      itemBuilder: (context, index) {
        final task = tasksState.tasks[index];
        return _TaskItem(task: task);
      },
    );
  }
}

/// Single task item in the list
class _TaskItem extends StatelessWidget {
  final dynamic task; // TODO: Use proper Task type

  const _TaskItem({required this.task});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Icon(
            _taskStatusIcon(task.status),
            size: 16,
            color: _taskStatusColor(task.status),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              task.title ?? 'Untitled Task',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.lightGray,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ),
        ],
      ),
    );
  }

  IconData _taskStatusIcon(String? status) {
    switch (status) {
      case 'completed':
        return Icons.check_circle;
      case 'failed':
        return Icons.error;
      case 'in_progress':
        return Icons.hourglass_empty;
      default:
        return Icons.circle_outlined;
    }
  }

  Color _taskStatusColor(String? status) {
    switch (status) {
      case 'completed':
        return CyberpunkColors.greenSuccess;
      case 'failed':
        return CyberpunkColors.redAlert;
      case 'in_progress':
        return CyberpunkColors.orangePrimary;
      default:
        return CyberpunkColors.lightGray;
    }
  }
}

/// Agents tab content - session scoped
class _AgentsTabContent extends ConsumerWidget {
  final String sessionId;

  const _AgentsTabContent({required this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // TODO Phase 5: Filter agents by session
    final agentsState = ref.watch(agentProvider);

    if (agentsState.agents.isEmpty) {
      return const _EmptyState(
        icon: Icons.smart_toy,
        message: 'no agents available',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: agentsState.agents.length,
      itemBuilder: (context, index) {
        final agent = agentsState.agents[index];
        return _AgentItem(agent: agent);
      },
    );
  }
}

/// Single agent item in the list
class _AgentItem extends StatelessWidget {
  final dynamic agent; // TODO: Use proper Agent type

  const _AgentItem({required this.agent});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          Icon(
            Icons.smart_toy,
            size: 20,
            color: CyberpunkColors.orangePrimary,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  agent.name ?? 'Unknown Agent',
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
                if (agent.role != null && agent.role!.isNotEmpty)
                  Text(
                    agent.role!,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.lightGray,
                      fontSize: 10,
                    ),
                  ),
              ],
            ),
          ),
          if (agent.status == 'active')
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(
                color: CyberpunkColors.greenSuccess,
                shape: BoxShape.circle,
                boxShadow: [
                  BoxShadow(
                    color: CyberpunkColors.greenSuccess.withValues(alpha: 0.5),
                    blurRadius: 4,
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Empty state placeholder
class _EmptyState extends StatelessWidget {
  final IconData icon;
  final String message;

  const _EmptyState({required this.icon, required this.message});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: 48,
            color: CyberpunkColors.midGray,
          ),
          const SizedBox(height: 16),
          Text(
            message,
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
