import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'dart:async';
import '../../core/constants.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/background_image.dart';
import '../../providers/providers.dart';
import '../../providers/tab_activation_provider.dart'
    show keyboardFocusProvider;
import '../../models/api_models.dart';
import '../../providers/agent_provider.dart';
import 'eval_runs_panel.dart';
import 'facts_panel.dart';
import 'quota_status.dart';

/// Agents tab - displays all available agents.
///
/// S3: This is the Flutter full-window agents view. The menubar app
/// (menubar/MeeptMenuBar/Views/AgentsView.swift) is a separate native
/// Swift implementation for the macOS menu bar. Both surfaces query
/// the same REST API (/api/v1/agents/*) but serve different UX roles:
/// - Swift menubar tab: quick status, pause/resume, approve plans
/// - Flutter full window: detailed constitution editing, audit review
///
/// Currently this widget shows the employee grid plus a goals pane
/// (health, gate command, approve/reject) when an agent is selected.
class AgentsTab extends ConsumerStatefulWidget {
  const AgentsTab({super.key});

  @override
  ConsumerState<AgentsTab> createState() => _AgentsTabState();
}

class _AgentsTabState extends ConsumerState<AgentsTab> {
  int _selectedIndex = 0;
  final _gridFocusNode = FocusNode();
  List<EmployeeGoal> _goals = const [];
  bool _goalsLoading = false;
  String? _goalsError;
  String? _goalsForId;
  Timer? _quotaTimer;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(agentProvider.notifier).loadAgents();
      _initQuotaListener();
    });
    _startQuotaTimer();
  }

  @override
  void dispose() {
    _gridFocusNode.dispose();
    _quotaTimer?.cancel();
    _quotaTimer = null;
    super.dispose();
  }

  void _startQuotaTimer() {
    _quotaTimer?.cancel();
    _quotaTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      // Only rebuild if there are active quota episodes.
      final episodes = ref.read(agentProvider).quotaEpisodes;
      if (episodes.isNotEmpty && mounted) {
        setState(() {});
      }
    });
  }

  void _initQuotaListener() {
    final ws = ref.read(websocketProvider);
    ws.messageStream.where((m) {
      final type = m['type'] as String?;
      return type == 'agent_progress' && m['to'] != null;
    }).listen((msg) {
      // AgentProgress.fromJson is null-safe by design; a malformed payload
      // must never crash the listener. Guard with try/catch anyway.
      try {
        final progress = AgentProgress.fromJson(msg);
        final quota = progress.quota;
        if (quota == null) return;
        ref.read(agentProvider.notifier).handleQuotaEvent(
          agentId: quota.agentId,
          to: quota.to,
          unblockAt: quota.unblockAt,
          fallbackModel: quota.fallbackModel,
          escalation: quota.escalation,
          // Parked-turn class (leaf 04); null-safe for legacy events.
          waitClass: quota.waitClass,
        );
      } catch (_) {
        // malformed quota payload — ignore without crash
      }
    });
  }

  /// Handle keyboard navigation for the agents grid.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final agentState = ref.read(agentProvider);
      final count = agentState.agents.length;
      if (count == 0) return KeyEventResult.ignored;

      // Calculate columns based on grid delegate (maxCrossAxisExtent: 225)
      final screenWidth = MediaQuery.of(node.context!).size.width;
      final cols = (screenWidth / 225).floor().clamp(1, 4);

      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _selectedIndex = (_selectedIndex + cols).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _selectedIndex = (_selectedIndex - cols).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
        setState(() {
          _selectedIndex = (_selectedIndex + 1).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
        setState(() {
          _selectedIndex = (_selectedIndex - 1).clamp(0, count - 1);
        });
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  Future<void> _loadGoals(String employeeId) async {
    setState(() {
      _goalsLoading = true;
      _goalsError = null;
      _goalsForId = employeeId;
    });
    try {
      final raw = await ref
          .read(sdkClientProvider)
          .listEmployeeGoals(employeeId);
      final goals = raw.map(EmployeeGoal.fromJson).toList(growable: false);
      if (!mounted || _goalsForId != employeeId) return;
      setState(() {
        _goals = goals;
        _goalsLoading = false;
      });
    } catch (e) {
      if (!mounted || _goalsForId != employeeId) return;
      setState(() {
        _goals = const [];
        _goalsLoading = false;
        _goalsError = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final agentState = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    return BackgroundImage(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(child: _buildAgentColumn(agentState)),
            if (activeAgent != null) ...[
              const SizedBox(width: 12),
              SizedBox(
                width: 300,
                child: Column(
                  children: [
                    // Expanded: the goals pane's own Column uses Expanded
                    // children (goal list); without a flex parent it would
                    // receive unbounded height and crash layout.
                    Expanded(child: _buildGoalsPane(activeAgent)),
                    const SizedBox(height: 12),
                    const EvalRunsPanel(),
                    const SizedBox(height: 12),
                    const FactsPanel(),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildAgentColumn(AgentState agentState) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              'agents',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Spacer(),
            IconButton(
              tooltip: 'refresh employees',
              icon: const Icon(Icons.refresh, size: 18),
              color: CyberpunkColors.orangePrimary,
              onPressed: () {
                ref.read(agentProvider.notifier).loadAgents();
              },
            ),
          ],
        ),
        const SizedBox(height: 16),
        if (agentState.isLoading)
          const Expanded(child: Center(child: CircularProgressIndicator()))
        else if (agentState.error != null)
          Expanded(
            child: Center(
              child: Column(
                children: [
                  SizedBox(
                    width: 280,
                    child: _AgentErrorBanner(message: agentState.error!),
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () =>
                        ref.read(agentProvider.notifier).loadAgents(),
                    child: const Text(
                      'retry',
                      style: CyberpunkTypography.bodySmall,
                    ),
                  ),
                ],
              ),
            ),
          )
        else if (agentState.agents.isEmpty)
          const Expanded(child: Center(child: Text('no agents available')))
        else
          Expanded(
            child: Focus(
              onFocusChange: (hasFocus) {
                if (hasFocus) {
                  ref
                      .read(keyboardFocusProvider.notifier)
                      .setFocusedPane(0);
                }
              },
              onKeyEvent: _handleKey,
              child: GridView.builder(
                gridDelegate:
                    const SliverGridDelegateWithMaxCrossAxisExtent(
                      maxCrossAxisExtent: 225,
                      crossAxisSpacing: 8,
                      mainAxisSpacing: 8,
                      childAspectRatio: 0.87,
                    ),
                itemCount: agentState.agents.length,
                itemBuilder: (context, index) {
                  final agent = agentState.agents[index];
                  final selected = ref.watch(activeAgentProvider);
                  final isSelected = selected?.id == agent.id;
                  final isKeyboardSelected = _selectedIndex == index;
                  final quotaState = agentState.quotaEpisodes[agent.id];
                  return _buildAgentCard(
                    agent,
                    isSelected,
                    isKeyboardSelected,
                    quotaState,
                  );
                },
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildAgentCard(
    Agent agent,
    bool isSelected,
    bool isKeyboardSelected,
    AgentQuotaState? quotaState,
  ) {
    return InkWell(
      key: ValueKey('agent-tile-${agent.id}'),
      onTap: () {
        ref.read(activeAgentProvider.notifier).state = agent;
        _loadGoals(agent.id);
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        decoration: BoxDecoration(
          color: isKeyboardSelected
              ? CyberpunkColors.orangeDark.withValues(alpha: 0.3)
              : (isSelected
                  ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
                  : CyberpunkColors.black),
          border: Border.all(
            color: isKeyboardSelected
                ? CyberpunkColors.orangeDark
                : (isSelected
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.midGray),
            width: isKeyboardSelected ? 3 : 1,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  getAgentIcon(agent.id),
                  color: isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.greenSuccess,
                  size: 20,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    agent.name.toLowerCase(),
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.greenSuccess,
                      fontFamily: 'SourceCodePro',
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            if (quotaState != null) ...[
              const SizedBox(height: 4),
              QuotaStatusBadge(quotaState: quotaState),
            ],
          ],
        ),
      ),
    );
  }

  Widget _buildGoalsPane(Agent agent) {
    // Quota episode for the selected agent (parity with the TUI detail
    // view): when a fallback model is carrying work, show the
    // primary/active model lines under the agent name. The event's
    // model_id (primary model) is not stored in AgentQuotaState yet, so
    // the primary line degrades to "unknown" via quotaDetailLines.
    final quotaState = ref.watch(agentProvider).quotaEpisodes[agent.id];
    final quotaBlocked = quotaState?.quotaBlocked ?? false;
    final quotaLines = quotaDetailLines(
      null,
      quotaState?.fallbackModel,
      quotaState?.quotaWaitUntilEpoch,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'goals',
          style: CyberpunkTypography.headlineMedium.copyWith(
            color: CyberpunkColors.orangePrimary,
            fontSize: 16,
          ),
        ),
        Text(
          agent.name.toLowerCase(),
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
        if (quotaLines.isNotEmpty) ...[
          const SizedBox(height: 8),
          for (final line in quotaLines)
            Text(
              line,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: quotaBlocked
                    ? CyberpunkColors.redAlert
                    : CyberpunkColors.yellowWarning,
                fontFamily: 'SourceCodePro',
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
        ],
        const SizedBox(height: 12),
        if (_goalsLoading)
          const Expanded(child: Center(child: CircularProgressIndicator()))
        else if (_goalsError != null)
          Expanded(
            child: Text(
              _goalsError!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          )
        else if (_goals.isEmpty)
          const Expanded(child: Center(child: Text('no goals')))
        else
          Expanded(
            child: ListView.separated(
              itemCount: _goals.length,
              separatorBuilder: (_, __) => const SizedBox(height: 8),
              itemBuilder: (context, i) {
                final g = _goals[i];
                return _GoalCard(
                  goal: g,
                  onApprove: g.activePlanId.isEmpty
                      ? null
                      : () async {
                          await ref
                              .read(sdkClientProvider)
                              .approvePlan(g.activePlanId);
                          await _loadGoals(agent.id);
                        },
                  onReject: g.activePlanId.isEmpty
                      ? null
                      : () async {
                          await ref
                              .read(sdkClientProvider)
                              .rejectPlan(g.activePlanId, reason: 'rejected via gui');
                          await _loadGoals(agent.id);
                        },
                );
              },
            ),
          ),
      ],
    );
  }
}

/// Inline error banner for agent list errors
class _AgentErrorBanner extends StatelessWidget {
  final String message;

  const _AgentErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}

class EmployeeGoal {
  final String id;
  final String title;
  final String health;
  final String activePlanId;
  final String gateCommand;

  const EmployeeGoal({
    required this.id,
    required this.title,
    required this.health,
    required this.activePlanId,
    required this.gateCommand,
  });

  factory EmployeeGoal.fromJson(Map<String, dynamic> json) {
    final gate = json['gate'];
    var gateCmd = '';
    if (gate is Map) {
      gateCmd = '${gate['command'] ?? ''}';
    }
    return EmployeeGoal(
      id: '${json['id'] ?? ''}',
      title: '${json['title'] ?? json['id'] ?? ''}',
      health: _healthLabel(json['health']),
      activePlanId: '${json['active_plan_id'] ?? ''}',
      gateCommand: gateCmd,
    );
  }

  static String _healthLabel(dynamic raw) {
    if (raw is String && raw.isNotEmpty) return raw;
    if (raw is num) {
      switch (raw.toInt()) {
        case 0:
          return 'healthy';
        case 1:
          return 'at_risk';
        case 2:
          return 'broken';
        default:
          return 'unknown';
      }
    }
    return 'unknown';
  }
}

class _GoalCard extends StatelessWidget {
  final EmployeeGoal goal;
  final Future<void> Function()? onApprove;
  final Future<void> Function()? onReject;

  const _GoalCard({
    required this.goal,
    this.onApprove,
    this.onReject,
  });

  Color get _healthColor {
    switch (goal.health) {
      case 'healthy':
        return CyberpunkColors.greenSuccess;
      case 'at_risk':
        return CyberpunkColors.orangePrimary;
      case 'broken':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.midGray;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      key: ValueKey('goal-card-${goal.id}'),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: CyberpunkColors.black,
        border: Border.all(color: CyberpunkColors.midGray),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.circle, size: 8, color: _healthColor),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  goal.title.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.greenSuccess,
                  ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            goal.health,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: _healthColor,
              fontSize: 10,
            ),
          ),
          if (goal.gateCommand.isNotEmpty)
            Text(
              'gate: ${goal.gateCommand}',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          if (onApprove != null || onReject != null)
            Row(
              children: [
                if (onApprove != null)
                  TextButton(
                    onPressed: onApprove,
                    child: const Text('approve'),
                  ),
                if (onReject != null)
                  TextButton(
                    onPressed: onReject,
                    child: const Text('reject'),
                  ),
              ],
            ),
        ],
      ),
    );
  }
}
