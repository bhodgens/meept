/// Session Info Overlay - Tabbed panel showing session-scoped data
///
/// Opens when clicking the [i] icon next to a session in the sidebar.
/// Shows four tabs: sessions (2-pane), plans, tasks, agents.
/// The sessions tab shows all sessions with the clicked one highlighted.
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

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';
import '../sessions/sessions_list.dart';
import '../sessions/sessions_detail.dart';

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
      insetPadding: const EdgeInsets.all(24),
      child: Container(
        width: 800,
        height: 560,
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
              tabs: const ['sessions', 'plans', 'tasks', 'agents'],
              selectedIndex: _selectedTabIndex,
              onTap: (index) => _tabController.animateTo(index),
            ),
            // Tab content
            Expanded(
              child: IndexedStack(
                index: _selectedTabIndex,
                children: [
                  _SessionsTabContent(selectedSession: widget.session),
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

/// Sessions tab: 2-pane view (list + detail) with the clicked session selected.
class _SessionsTabContent extends ConsumerWidget {
  final Session selectedSession;

  const _SessionsTabContent({required this.selectedSession});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Ensure the clicked session is the active one for the detail pane
    final activeSession = ref.watch(activeSessionProvider);
    final displaySession = activeSession?.id == selectedSession.id
        ? activeSession!
        : selectedSession;

    return Row(
      children: [
        const SizedBox(width: 260, child: SessionsList()),
        const VerticalDivider(width: 1),
        Expanded(
          child: SessionsDetailPane(session: displaySession),
        ),
      ],
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
    final plansState = ref.watch(planProvider);
    final sessionPlans = plansState.plans
        .where((p) => p.sourceSession == sessionId)
        .toList();

    if (sessionPlans.isEmpty) {
      return const _EmptyState(
        icon: Icons.document_scanner,
        message: 'no plans for this session',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: sessionPlans.length,
      itemBuilder: (context, index) {
        final plan = sessionPlans[index];
        return _PlanItem(plan: plan);
      },
    );
  }
}

/// Single plan item in the list
class _PlanItem extends StatelessWidget {
  final Plan plan;

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
            plan.title.isEmpty ? 'Untitled Plan' : plan.title,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
              fontFamily: 'SourceCodePro',
            ),
          ),
          if (plan.description.isNotEmpty)
            const SizedBox(height: 4),
          if (plan.description.isNotEmpty)
            Text(
              plan.description,
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
    final tasksState = ref.watch(taskProvider);
    final sessionTasks = tasksState.tasks
        .where((t) => t.sessionId == sessionId)
        .toList();

    if (sessionTasks.isEmpty) {
      return const _EmptyState(
        icon: Icons.task_alt,
        message: 'no tasks for this session',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: sessionTasks.length,
      itemBuilder: (context, index) {
        final task = sessionTasks[index];
        return _TaskItem(task: task);
      },
    );
  }
}

/// Single task item in the list
class _TaskItem extends StatelessWidget {
  final Task task;

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
              task.title.isEmpty ? 'Untitled Task' : task.title,
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

  IconData _taskStatusIcon(String status) {
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

  Color _taskStatusColor(String status) {
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

/// Agents tab content - shows agents assigned to tasks in this session
class _AgentsTabContent extends ConsumerWidget {
  final String sessionId;

  const _AgentsTabContent({required this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final agentsState = ref.watch(agentProvider);
    final tasksState = ref.watch(taskProvider);

    // Collect unique agent IDs from tasks in this session
    final sessionTaskAgentIds = <String>{
      for (final t in tasksState.tasks)
        if (t.sessionId == sessionId && t.agentId != null) t.agentId!,
    };

    // If we have specific agents, show them; otherwise show all agents
    final List<Agent> displayAgents;
    if (sessionTaskAgentIds.isEmpty || sessionTaskAgentIds.length >= agentsState.agents.length) {
      displayAgents = agentsState.agents;
    } else {
      displayAgents = agentsState.agents
          .where((a) => sessionTaskAgentIds.contains(a.id))
          .toList();
    }

    if (displayAgents.isEmpty) {
      return const _EmptyState(
        icon: Icons.smart_toy,
        message: 'no agents for this session',
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(16),
      itemCount: displayAgents.length,
      itemBuilder: (context, index) {
        final agent = displayAgents[index];
        return _AgentItem(agent: agent);
      },
    );
  }
}

/// Single agent item in the list
class _AgentItem extends StatelessWidget {
  final Agent agent;

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
                  agent.name.isEmpty ? 'Unknown Agent' : agent.name,
                  style: CyberpunkTypography.bodyMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
                if (agent.description.isNotEmpty)
                  Text(
                    agent.description,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.lightGray,
                      fontSize: 10,
                    ),
                  ),
              ],
            ),
          ),
          if (agent.enabled)
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
