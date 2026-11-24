import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/background_image.dart';
import '../../models/api_models.dart';
import '../../models/ws_events.dart' show WsEventTypes;
import '../../providers/providers.dart';
import '../../providers/tab_activation_provider.dart'
    show keyboardFocusProvider;

class PlansTab extends ConsumerStatefulWidget {
  const PlansTab({super.key});

  @override
  ConsumerState<PlansTab> createState() => _PlansTabState();
}

class _PlansTabState extends ConsumerState<PlansTab> {
  int _selectedIndex = 0;
  final _listFocusNode = FocusNode();

  /// Live gate refresh: a task.pending_approval bus frame arrives as a
  /// job_update (source_topic-tagged) while this tab is mounted, so the
  /// gate card appears without leaving and re-entering the tab (bughunt
  /// wave2 parity with the TUI, which rebuilds gates on the event).
  StreamSubscription<Map<String, dynamic>>? _pendingApprovalSub;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final session = ref.read(activeSessionProvider);
      ref.read(planProvider.notifier).loadPlans(sessionID: session?.id);
      // The strategic approval gate lives in tasks.db (awaiting_approval
      // TASKS, 06-planning/02 T1): the plans tab needs the task list to
      // render the gate/seal cards alongside the plans.db rows.
      ref.read(taskProvider.notifier).loadTasks();
    });
    _pendingApprovalSub = ref.read(websocketProvider).messageStream.listen((
      msg,
    ) {
      if (msg['type'] != WsEventTypes.jobUpdate) return;
      if (msg['source_topic'] != 'task.pending_approval') return;
      // Cheap full reload; no debounce needed (one frame per gate).
      ref.read(taskProvider.notifier).loadTasks();
    });
  }

  @override
  void dispose() {
    _pendingApprovalSub?.cancel();
    _listFocusNode.dispose();
    super.dispose();
  }

  /// Handle keyboard navigation for the plans list.
  KeyEventResult _handleKey(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent && node.hasFocus) {
      final planState = ref.read(planProvider);
      final maxIndex = planState.plans.length - 1;
      if (maxIndex < 0) return KeyEventResult.ignored;

      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        setState(() {
          _selectedIndex = (_selectedIndex + 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        setState(() {
          _selectedIndex = (_selectedIndex - 1).clamp(0, maxIndex);
        });
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    final planState = ref.watch(planProvider);
    final activeSession = ref.watch(activeSessionProvider);

    return Row(
      children: [
        // Plan list
        SizedBox(width: 300, child: _buildPlanList(planState)),
        Container(
          width: 1,
          color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
        ),
        // Plan detail
        Expanded(
          child: BackgroundImage(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // Strategic task-gate cards (06-planning/02): the
                // awaiting_approval approval card and the compiler-draft
                // seal surface live in tasks.db and render ABOVE the
                // plans.db detail, distinct from the plans gate.
                _TaskGateSection(sessionId: activeSession?.id),
                Expanded(
                  child: planState.plans.isEmpty
                      ? _buildEmptyState()
                      : _PlanDetailPane(
                          plans: planState.plans,
                          sessionId: activeSession?.id,
                          selectedIndex: _selectedIndex,
                        ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildPlanList(PlanState planState) {
    return Column(
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
                tooltip: 'refresh plans',
                icon: const Icon(Icons.refresh, size: 18),
                color: CyberpunkColors.orangePrimary,
                onPressed: () {
                  final session = ref.read(activeSessionProvider);
                  ref
                      .read(planProvider.notifier)
                      .loadPlans(sessionID: session?.id);
                },
              ),
            ],
          ),
        ),
        if (planState.isLoading)
          const Expanded(child: Center(child: CircularProgressIndicator()))
        else if (planState.error != null)
          Expanded(
            child: Center(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Text(
                  planState.error!,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.redAlert,
                  ),
                ),
              ),
            ),
          )
        else if (planState.plans.isEmpty)
          const Expanded(child: Center(child: Text('no plans')))
        else
          Expanded(
            child: Focus(
              onFocusChange: (hasFocus) {
                if (hasFocus) {
                  ref.read(keyboardFocusProvider.notifier).setFocusedPane(0);
                }
              },
              onKeyEvent: _handleKey,
              child: ListView.builder(
                itemCount: planState.plans.length,
                itemBuilder: (context, index) => _PlanTile(
                  plan: planState.plans[index],
                  selected: _selectedIndex == index,
                  onTap: () => setState(() => _selectedIndex = index),
                ),
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildEmptyState() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.assignment_outlined,
            size: 64,
            color: CyberpunkColors.midGray,
          ),
          const SizedBox(height: 16),
          Text(
            'no plan selected',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.lightGray,
            ),
          ),
        ],
      ),
    );
  }
}

class _PlanTile extends ConsumerWidget {
  final Plan plan;
  final bool selected;
  final VoidCallback onTap;

  const _PlanTile({
    required this.plan,
    this.selected = false,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ListTile(
      dense: true,
      selected: selected,
      selectedTileColor: CyberpunkColors.orangePrimary.withValues(alpha: 0.1),
      onTap: onTap,
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
  final int selectedIndex;

  const _PlanDetailPane({
    required this.plans,
    this.sessionId,
    required this.selectedIndex,
  });

  @override
  ConsumerState<_PlanDetailPane> createState() => _PlanDetailPaneState();
}

class _PlanDetailPaneState extends ConsumerState<_PlanDetailPane> {
  @override
  Widget build(BuildContext context) {
    var selected = widget.selectedIndex;
    if (selected >= widget.plans.length) selected = 0;
    if (widget.plans.isEmpty) return const SizedBox.shrink();
    final plan = widget.plans[selected];

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
            Text(
              'phases',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 8),
            ...plan.phases.map((phase) => _PhaseCard(phase: phase)),
            const SizedBox(height: 24),
          ],

          // Actions
          Text(
            'actions',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
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
        style: CyberpunkTypography.bodySmall.copyWith(
          color: _color,
          fontFamily: 'SourceCodePro',
        ),
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
            child: Text(
              '${phase.sequence}',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  phase.name.toLowerCase(),
                  style: CyberpunkTypography.bodyMedium,
                ),
                const SizedBox(height: 4),
                Text(
                  '${phase.completedSteps}/${phase.totalSteps} steps',
                  style: CyberpunkTypography.bodySmall,
                ),
                // Produces artifacts
                if (phase.produces.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  _ArtifactList(
                    label: 'produces',
                    artifacts: phase.produces,
                    color: CyberpunkColors.greenSuccess,
                  ),
                ],
                // Consumes artifacts
                if (phase.consumes.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  _ArtifactList(
                    label: 'consumes',
                    artifacts: phase.consumes,
                    color: CyberpunkColors.orangeBright,
                  ),
                ],
              ],
            ),
          ),
          _StateBadge(state: phase.state),
        ],
      ),
    );
  }
}

class _ArtifactList extends StatelessWidget {
  final String label;
  final List<PlanArtifact> artifacts;
  final Color color;

  const _ArtifactList({
    required this.label,
    required this.artifacts,
    required this.color,
  });

  @override
  Widget build(BuildContext context) {
    final labelStyle = CyberpunkTypography.bodySmall.copyWith(
      color: color,
      fontWeight: FontWeight.bold,
    );
    final itemStyle = CyberpunkTypography.bodySmall.copyWith(
      color: CyberpunkColors.lightGray,
      fontFamily: 'SourceCodePro',
    );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: labelStyle),
        for (final a in artifacts)
          Padding(
            padding: const EdgeInsets.only(left: 8),
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(text: '${a.name} ', style: itemStyle),
                  TextSpan(
                    text: '(${a.kind}',
                    style: itemStyle.copyWith(color: CyberpunkColors.midGray),
                  ),
                  if (a.required)
                    TextSpan(
                      text: ', required',
                      style: itemStyle.copyWith(
                        color: CyberpunkColors.orangeBright,
                      ),
                    ),
                  TextSpan(
                    text: ')',
                    style: itemStyle.copyWith(color: CyberpunkColors.midGray),
                  ),
                ],
              ),
            ),
          ),
      ],
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
            onPressed: () =>
                notifier.approvePlan(plan.id, sessionID: sessionId),
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
            onPressed: () =>
                notifier.confirmPlan(plan.id, sessionID: sessionId),
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
                SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation(
                      CyberpunkColors.orangeBright,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  'executing...',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.orangeBright,
                  ),
                ),
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
        title: const Text(
          'reject plan',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: 'reason (optional)...',
            hintStyle: CyberpunkTypography.bodySmall,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: CyberpunkColors.redAlert,
            ),
            onPressed: () {
              notifier.rejectPlan(
                plan.id,
                sessionID: sessionId,
                reason: controller.text,
              );
              Navigator.pop(ctx);
            },
            child: const Text('reject', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    ).whenComplete(() => controller.dispose());
  }

  void _showReviseDialog(BuildContext context, PlanNotifier notifier) {
    final controller = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'request revision',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: TextField(
          controller: controller,
          maxLines: 3,
          decoration: const InputDecoration(
            hintText: 'feedback...',
            hintStyle: CyberpunkTypography.bodySmall,
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            onPressed: () {
              notifier.revisePlan(
                plan.id,
                sessionID: sessionId,
                feedback: controller.text,
              );
              Navigator.pop(ctx);
            },
            child: const Text('revise', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    ).whenComplete(() => controller.dispose());
  }
}

class _ActionButton extends StatelessWidget {
  final String label;
  final Color color;
  final VoidCallback? onPressed;

  const _ActionButton({
    required this.label,
    required this.color,
    required this.onPressed,
  });

  @override
  Widget build(BuildContext context) {
    return FilledButton(
      style: FilledButton.styleFrom(backgroundColor: color),
      onPressed: onPressed,
      child: Text(
        label,
        style: CyberpunkTypography.bodyMedium.copyWith(
          color: CyberpunkColors.black,
        ),
      ),
    );
  }
}

// =====================================================================
// Strategic task-gate cards (06-planning/02)
//
// The strategic APPROVAL gate and the plan-compiler SEAL flow operate on
// TASKS (tasks.db via the C2 HTTP twins + the plan.draft/plan.seal
// twins), NOT on plans.db rows -- the two gates are deliberately
// distinct surfaces here so they can never be conflated. The section
// renders above the plans.db detail pane.
// =====================================================================

/// Lists the awaiting_approval / compiler-draft / wait-state tasks and
/// renders one actionable card per gate. Read-only until the user acts:
/// no card ever calls approveTask/rejectTask/sealTask by itself.
class _TaskGateSection extends ConsumerWidget {
  final String? sessionId;

  const _TaskGateSection({this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasks = ref.watch(taskProvider).tasks;
    // Session scoping (TUI parity, internal/tui/models/plans.go
    // pendingTaskGatesForFilter): an empty filter shows every gate, a
    // task with an empty/null session id (legacy publisher / rebind
    // window) shows in every filter, and only an explicit FOREIGN
    // session id is dropped.
    final filter = sessionId ?? '';
    final scoped = tasks.where((t) {
      if (filter.isEmpty) return true;
      final taskSession = t.sessionId ?? '';
      return taskSession.isEmpty || taskSession == filter;
    });
    final pendingApproval = scoped
        .where((t) => t.status == 'awaiting_approval')
        .toList(growable: false);
    final drafts = scoped
        .where((t) => t.status == 'planning' && _draftMarkdownOf(t) != null)
        .toList(growable: false);
    final waiting = scoped
        .where((t) => _waitStateCopy(t.status) != null)
        .toList(growable: false);

    if (pendingApproval.isEmpty && drafts.isEmpty && waiting.isEmpty) {
      return const SizedBox.shrink();
    }

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 0),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          for (final task in pendingApproval) ...[
            _ApprovalTaskCard(task: task, sessionId: sessionId),
            const SizedBox(width: 12),
          ],
          for (final task in drafts) ...[
            _SealTaskCard(task: task),
            const SizedBox(width: 12),
          ],
          for (final task in waiting) ...[
            _WaitStateCard(task: task),
            const SizedBox(width: 12),
          ],
        ],
      ),
    );
  }

  /// Actionable copy for wait states that used to render as silent gray
  /// badges (T3): hostile review skipped and await-seal are WAITING ON
  /// THE USER, not dead ends.
  static String? _waitStateCopy(String status) {
    switch (status) {
      case 'hostile_skipped':
        return 'hostile review skipped - configure a critique model to '
            'harden this plan before it runs';
      case 'await_seal':
        return 'awaiting your seal - review the compiled draft and seal '
            'it to start execution';
      case 'compile_failed':
        return 'compile failed - fix the draft and seal again';
      default:
        return null;
    }
  }
}

/// C2 strategic-gate card: step summary + approve/reject wired to
/// approveTask/rejectTask (task endpoints, never the plans.db gate).
/// Reject opens a reason dialog and forwards the full reason; it can
/// never reach the approve seam.
class _ApprovalTaskCard extends ConsumerWidget {
  final Task task;
  final String? sessionId;

  const _ApprovalTaskCard({required this.task, this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final notifier = ref.read(taskProvider.notifier);
    return _GateCardShell(
      icon: Icons.gavel,
      color: CyberpunkColors.orangePrimary,
      title: 'strategic approval: ${task.title.toLowerCase()}',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (task.description.isNotEmpty) ...[
            Text(
              task.description,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
              ),
            ),
            const SizedBox(height: 6),
          ],
          _StepSummary(task: task),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _ActionButton(
                label: 'approve',
                color: CyberpunkColors.greenSuccess,
                onPressed: () => notifier.approveTask(task.id),
              ),
              _ActionButton(
                label: 'reject',
                color: CyberpunkColors.redAlert,
                onPressed: () => _showTaskRejectDialog(context, notifier),
              ),
            ],
          ),
        ],
      ),
    );
  }

  void _showTaskRejectDialog(BuildContext context, TaskNotifier notifier) {
    // The controller lives in the dialog's own State (disposed when the
    // route unmounts), NOT via whenComplete on the showDialog future:
    // that future completes at pop() while the exit transition is still
    // rebuilding the TextField, which throws "controller used after
    // being disposed".
    showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'reject task',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: _RejectReasonField(
          onSubmit: (reason) {
            notifier.rejectTask(task.id, reason: reason);
            Navigator.pop(ctx);
          },
        ),
      ),
    );
  }
}

/// Reject-reason input + confirm/cancel actions for the strategic task
/// gate. Owns its TextEditingController for its full element lifetime.
class _RejectReasonField extends StatefulWidget {
  final ValueChanged<String> onSubmit;

  const _RejectReasonField({required this.onSubmit});

  @override
  State<_RejectReasonField> createState() => _RejectReasonFieldState();
}

class _RejectReasonFieldState extends State<_RejectReasonField> {
  final controller = TextEditingController();

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextField(
          controller: controller,
          maxLines: 3,
          decoration: const InputDecoration(
            hintText: 'reason for rejection...',
            hintStyle: CyberpunkTypography.bodySmall,
          ),
        ),
        const SizedBox(height: 12),
        Row(
          mainAxisAlignment: MainAxisAlignment.end,
          children: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text(
                'cancel',
                style: CyberpunkTypography.bodyMedium,
              ),
            ),
            // LOAD-BEARING: the only action is rejectTask -> task.reject.
            // Rejecting can never approve or execute the task.
            FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: CyberpunkColors.redAlert,
              ),
              onPressed: () => widget.onSubmit(controller.text),
              child: const Text(
                'reject',
                style: CyberpunkTypography.bodyMedium,
              ),
            ),
          ],
        ),
      ],
    );
  }
}

/// Planned-step rollout summary for the gated task (steps render).
class _StepSummary extends StatelessWidget {
  final Task task;
  const _StepSummary({required this.task});

  @override
  Widget build(BuildContext context) {
    final style = CyberpunkTypography.bodySmall.copyWith(
      color: CyberpunkColors.orangeBright,
    );
    if (task.steps != null && task.steps!.isNotEmpty) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('planned steps (${task.steps!.length}):', style: style),
          for (final step in task.steps!.take(6))
            Text('- ${step.description}', style: style),
        ],
      );
    }
    final jobs = task.totalJobs ?? 0;
    return Text(
      jobs > 0
          ? '$jobs planned steps await your approval'
          : 'plan steps '
                'await your approval',
      style: style,
    );
  }
}

/// Seal surface for a compiler draft: renders the draft markdown inline,
/// warns on raw scaffolds, and offers an explicit seal action. NEVER
/// auto-seals -- sealTask fires only from the button. Compile problems
/// come back as a RESULT and render inline (line + message); the draft
/// stays a draft.
class _SealTaskCard extends ConsumerStatefulWidget {
  final Task task;

  const _SealTaskCard({required this.task});

  @override
  ConsumerState<_SealTaskCard> createState() => _SealTaskCardState();
}

class _SealTaskCardState extends ConsumerState<_SealTaskCard> {
  bool _sealing = false;
  List<Map<String, dynamic>> _problems = const [];
  Map<String, dynamic>? _sealed;
  String? _sealError;

  Future<void> _seal() async {
    // Explicit user action only. No route to this method runs at render.
    setState(() {
      _sealing = true;
      _problems = const [];
      _sealed = null;
      _sealError = null;
    });
    try {
      final result = await ref.read(sdkClientProvider).sealTask(widget.task.id);
      if (!mounted) return;
      setState(() {
        _sealing = false;
        if (result['status'] == 'problems') {
          // Problems are a result, not an error: render inline, stay draft.
          _problems = (result['problems'] as List? ?? const [])
              .whereType<Map>()
              .map((p) => Map<String, dynamic>.from(p))
              .toList(growable: false);
        } else {
          _sealed = result;
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _sealing = false;
        _sealError = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final task = widget.task;
    final markdown = _draftMarkdownOf(task) ?? '';
    final rawScaffold = _isRawScaffold(markdown);

    return _GateCardShell(
      icon: Icons.verified_outlined,
      color: CyberpunkColors.orangeBright,
      title: 'compiler draft: ${task.title.toLowerCase()}',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (rawScaffold && _sealed == null) ...[
            Text(
              'warning: raw scaffold - Decisions and Phases are empty; '
              'sealing this draft will fail to compile',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
            const SizedBox(height: 6),
          ],
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 220, maxWidth: 420),
            child: SingleChildScrollView(
              child: SelectableText(
                markdown,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontFamily: 'SourceCodePro',
                ),
              ),
            ),
          ),
          const SizedBox(height: 10),
          if (_sealed != null)
            Text(
              'sealed (${_sealed!['mode'] ?? 'flat'}, hash '
              '${_sealed!['hash'] ?? '?'}) - execution dispatched',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.greenSuccess,
              ),
            )
          else ...[
            Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                _ActionButton(
                  label: 'seal',
                  color: CyberpunkColors.orangeBright,
                  onPressed: _sealing ? null : () => _seal(),
                ),
                if (_sealing)
                  SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      valueColor: AlwaysStoppedAnimation(
                        CyberpunkColors.orangeBright,
                      ),
                    ),
                  ),
              ],
            ),
            if (_problems.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                'compile problems (${_problems.length}) - draft stays a '
                'draft:',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                  fontWeight: FontWeight.bold,
                ),
              ),
              for (final p in _problems)
                Text(
                  'line ${p['line'] ?? '?'}: ${p['message'] ?? ''}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.redAlert,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
            ],
            if (_sealError != null)
              Text(
                _sealError!,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                ),
              ),
          ],
        ],
      ),
    );
  }
}

/// T3: actionable wait state (never a silent gray badge).
class _WaitStateCard extends StatelessWidget {
  final Task task;

  const _WaitStateCard({required this.task});

  @override
  Widget build(BuildContext context) {
    final copy = _TaskGateSection._waitStateCopy(task.status) ?? task.status;
    return _GateCardShell(
      icon: Icons.hourglass_bottom,
      color: CyberpunkColors.orangePrimary,
      title: 'waiting: ${task.title.toLowerCase()}',
      child: Text(
        copy,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: CyberpunkColors.orangeBright,
        ),
      ),
    );
  }
}

class _GateCardShell extends StatelessWidget {
  final IconData icon;
  final Color color;
  final String title;
  final Widget child;

  const _GateCardShell({
    required this.icon,
    required this.color,
    required this.title,
    required this.child,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 460,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(color: color, width: 1),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 16, color: color),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  title,
                  style: CyberpunkTypography.bodyMedium.copyWith(color: color),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          child,
        ],
      ),
    );
  }
}

/// Extract the compiler draft markdown from task metadata
/// (`plan_draft.markdown`), mirroring the plan.draft get shape.
String? _draftMarkdownOf(Task task) {
  final draft = task.metadata?['plan_draft'];
  if (draft is Map) {
    final md = draft['markdown'];
    if (md is String && md.isNotEmpty) return md;
  }
  return null;
}

/// A freshly seeded scaffold has empty Decisions AND empty Phases
/// sections -- not ready to compile; the seal surface warns before any
/// explicit seal.
bool _isRawScaffold(String markdown) {
  if (markdown.isEmpty) return false;
  return _sectionBody(markdown, 'Decisions').isEmpty &&
      _sectionBody(markdown, 'Phases').isEmpty;
}

/// Body text (non-blank lines) of a `## Heading` section.
String _sectionBody(String markdown, String heading) {
  final out = <String>[];
  var inSection = false;
  for (final line in markdown.split('\n')) {
    final trimmed = line.trim();
    if (trimmed.startsWith('## ')) {
      if (inSection) break;
      inSection =
          trimmed.substring(3).trim().toLowerCase() == heading.toLowerCase();
      continue;
    }
    if (inSection && trimmed.isNotEmpty) out.add(trimmed);
  }
  return out.join(' ');
}
