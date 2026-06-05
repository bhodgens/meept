import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/plan_provider.dart';
import '../../providers/providers.dart';

class PlansTab extends ConsumerStatefulWidget {
  const PlansTab({super.key});

  @override
  ConsumerState<PlansTab> createState() => _PlansTabState();
}

class _PlansTabState extends ConsumerState<PlansTab> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final session = ref.read(activeSessionProvider);
      ref.read(planProvider.notifier).loadPlans(sessionID: session?.id);
    });
  }

  @override
  Widget build(BuildContext context) {
    final planState = ref.watch(planProvider);
    final activeSession = ref.watch(activeSessionProvider);

    return Container(
      color: CyberpunkColors.black,
      child: planState.when(
        initial: () => _buildPlanList(const [], false, null, activeSession?.id),
        loading: () => _buildPlanList(const [], true, null, activeSession?.id),
        error: (error, _) => _buildPlanList(const [], false, error.toString(), activeSession?.id),
        data: (plans) => _buildPlanList(plans, false, null, activeSession?.id),
      ),
    );
  }

  Widget _buildPlanList(List<Plan> plans, bool isLoading, String? error, String? sessionId) {
    return Row(
      children: [
        // Plan list
        SizedBox(
          width: 300,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Text(
                      'plans',
                      style: CyberpunkTypography.headlineMedium.copyWith(
                        color: CyberpunkColors.orangePrimary,
                      ),
                    ),
                    const Spacer(),
                    IconButton(
                      icon: const Icon(Icons.refresh, size: 18),
                      color: CyberpunkColors.orangePrimary,
                      onPressed: () {
                        final session = ref.read(activeSessionProvider);
                        ref.read(planProvider.notifier).loadPlans(sessionID: session?.id);
                      },
                    ),
                  ],
                ),
              ),
              if (isLoading)
                const Expanded(child: Center(child: CircularProgressIndicator()))
              else if (error != null)
                Expanded(
                  child: Center(
                    child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Text(error, style: CyberpunkTypography.bodySmall.copyWith(color: CyberpunkColors.redAlert)),
                    ),
                  ),
                )
              else if (plans.isEmpty)
                const Expanded(child: Center(child: Text('no plans')))
              else
                Expanded(
                  child: ListView.builder(
                    itemCount: plans.length,
                    itemBuilder: (context, index) => _PlanTile(plan: plans[index]),
                  ),
                ),
            ],
          ),
        ),
        Container(width: 1, color: CyberpunkColors.orangeDark.withValues(alpha: 0.3)),
        // Plan detail
        Expanded(
          child: plans.isEmpty
              ? _buildEmptyState()
              : _PlanDetailPane(plans: plans, sessionId: sessionId),
        ),
      ],
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.assignment_outlined, size: 64, color: CyberpunkColors.midGray),
          const SizedBox(height: 16),
          Text('no plan selected', style: CyberpunkTypography.bodyMedium.copyWith(color: CyberpunkColors.lightGray)),
        ],
      ),
    );
  }
}

class _PlanTile extends ConsumerWidget {
  final Plan plan;

  const _PlanTile({required this.plan});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ListTile(
      dense: true,
      title: Text(
        plan.title.toLowerCase(),
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: _stateColor(plan.state),
        ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text(
        plan.state.replaceAll('_', ' '),
        style: CyberpunkTypography.bodySmall,
      ),
      trailing: Container(
        width: 8,
        height: 8,
        decoration: BoxDecoration(
          color: _stateColor(plan.state),
          shape: BoxShape.circle,
        ),
      ),
    );
  }

  Color _stateColor(String state) {
    switch (state) {
      case 'completed':
      case 'confirmed':
        return CyberpunkColors.greenSuccess;
      case 'pending_approval':
        return CyberpunkColors.orangePrimary;
      case 'approved':
      case 'executing':
        return CyberpunkColors.orangeBright;
      case 'failed':
      case 'cancelled':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.lightGray;
    }
  }
}

class _PlanDetailPane extends ConsumerStatefulWidget {
  final List<Plan> plans;
  final String? sessionId;

  const _PlanDetailPane({required this.plans, this.sessionId});

  @override
  ConsumerState<_PlanDetailPane> createState() => _PlanDetailPaneState();
}

class _PlanDetailPaneState extends ConsumerState<_PlanDetailPane> {
  int _selected = 0;

  @override
  Widget build(BuildContext context) {
    if (_selected >= widget.plans.length) _selected = 0;
    if (widget.plans.isEmpty) return const SizedBox.shrink();
    final plan = widget.plans[_selected];

    return SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Header
          Row(
            children: [
              Expanded(
                child: Text(
                  plan.title.toLowerCase(),
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
              ),
              _StateBadge(state: plan.state),
            ],
          ),
          if (plan.description.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(plan.description, style: CyberpunkTypography.bodyMedium),
          ],
          const SizedBox(height: 24),

          // Phases
          if (plan.phases.isNotEmpty) ...[
            Text('phases', style: CyberpunkTypography.label.copyWith(color: CyberpunkColors.orangePrimary)),
            const SizedBox(height: 8),
            ...plan.phases.map((phase) => _PhaseCard(phase: phase)),
            const SizedBox(height: 24),
          ],

          // Actions
          Text('actions', style: CyberpunkTypography.label.copyWith(color: CyberpunkColors.orangePrimary)),
          const SizedBox(height: 8),
          _ActionButtons(plan: plan, sessionId: widget.sessionId),
        ],
      ),
    );
  }
}

class _StateBadge extends StatelessWidget {
  final String state;
  const _StateBadge({required this.state});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: _color.withValues(alpha: 0.15),
        border: Border.all(color: _color, width: 1),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        state.replaceAll('_', ' '),
        style: CyberpunkTypography.bodySmall.copyWith(color: _color, fontFamily: 'SourceCodePro'),
      ),
    );
  }

  Color get _color {
    switch (state) {
      case 'completed':
      case 'confirmed':
        return CyberpunkColors.greenSuccess;
      case 'pending_approval':
        return CyberpunkColors.orangePrimary;
      case 'approved':
      case 'executing':
        return CyberpunkColors.orangeBright;
      case 'failed':
      case 'cancelled':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.lightGray;
    }
  }
}

class _PhaseCard extends StatelessWidget {
  final PlanPhase phase;
  const _PhaseCard({required this.phase});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(color: CyberpunkColors.midGray, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        children: [
          SizedBox(
            width: 24,
            child: Text('${phase.sequence}', style: CyberpunkTypography.label.copyWith(color: CyberpunkColors.orangePrimary)),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(phase.name.toLowerCase(), style: CyberpunkTypography.bodyMedium),
                const SizedBox(height: 4),
                Text(
                  '${phase.completedSteps}/${phase.totalSteps} steps',
                  style: CyberpunkTypography.bodySmall,
                ),
              ],
            ),
          ),
          _StateBadge(state: phase.state),
        ],
      ),
    );
  }
}

class _ActionButtons extends ConsumerWidget {
  final Plan plan;
  final String? sessionId;

  const _ActionButtons({required this.plan, this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final notifier = ref.read(planProvider.notifier);

    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        if (plan.state == 'pending_approval') ...[
          _ActionButton(
            label: 'approve',
            color: CyberpunkColors.greenSuccess,
            onPressed: () => notifier.approvePlan(plan.id, sessionID: sessionId),
          ),
          _ActionButton(
            label: 'reject',
            color: CyberpunkColors.redAlert,
            onPressed: () => _showRejectDialog(context, notifier),
          ),
          _ActionButton(
            label: 'revise',
            color: CyberpunkColors.orangePrimary,
            onPressed: () => _showReviseDialog(context, notifier),
          ),
        ],
        if (plan.state == 'completed')
          _ActionButton(
            label: 'confirm',
            color: CyberpunkColors.greenSuccess,
            onPressed: () => notifier.confirmPlan(plan.id, sessionID: sessionId),
          ),
        if (plan.state == 'approved')
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            decoration: BoxDecoration(
              color: CyberpunkColors.orangeBright.withValues(alpha: 0.1),
              borderRadius: BorderRadius.circular(4),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2, valueColor: AlwaysStoppedAnimation(CyberpunkColors.orangeBright))),
                const SizedBox(width: 8),
                Text('executing...', style: CyberpunkTypography.bodySmall.copyWith(color: CyberpunkColors.orangeBright)),
              ],
            ),
          ),
      ],
    );
  }

  void _showRejectDialog(BuildContext context, PlanNotifier notifier) {
    final controller = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text('reject plan', style: CyberpunkTypography.headlineMedium),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(hintText: 'reason (optional)...', hintStyle: CyberpunkTypography.bodySmall),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text('cancel', style: CyberpunkTypography.bodyMedium)),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: CyberpunkColors.redAlert),
            onPressed: () {
              notifier.rejectPlan(plan.id, sessionID: sessionId, reason: controller.text);
              Navigator.pop(ctx);
            },
            child: Text('reject', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }

  void _showReviseDialog(BuildContext context, PlanNotifier notifier) {
    final controller = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: Text('request revision', style: CyberpunkTypography.headlineMedium),
        content: TextField(
          controller: controller,
          maxLines: 3,
          decoration: const InputDecoration(hintText: 'feedback...', hintStyle: CyberpunkTypography.bodySmall),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: Text('cancel', style: CyberpunkTypography.bodyMedium)),
          FilledButton(
            onPressed: () {
              notifier.revisePlan(plan.id, sessionID: sessionId, feedback: controller.text);
              Navigator.pop(ctx);
            },
            child: Text('revise', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }
}

class _ActionButton extends StatelessWidget {
  final String label;
  final Color color;
  final VoidCallback onPressed;

  const _ActionButton({required this.label, required this.color, required this.onPressed});

  @override
  Widget build(BuildContext context) {
    return FilledButton(
      style: FilledButton.styleFrom(backgroundColor: color),
      onPressed: onPressed,
      child: Text(label, style: CyberpunkTypography.bodyMedium.copyWith(color: CyberpunkColors.black)),
    );
  }
}
