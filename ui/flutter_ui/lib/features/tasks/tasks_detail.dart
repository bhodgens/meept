import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import 'tasks_tab.dart' show activeTaskProvider;

/// Task detail pane - displays task info with status controls.
///
/// 05-navigation T2: the detail hydrates its steps through the SDK
/// `getTask` seam (the daemon embeds `steps` on the task document). The
/// fetch is bounded: at most one attempt per selected task id, so
/// rebuilds never cause an RPC storm.
class TasksDetail extends ConsumerStatefulWidget {
  const TasksDetail({super.key});

  @override
  ConsumerState<TasksDetail> createState() => _TasksDetailState();
}

class _TasksDetailState extends ConsumerState<TasksDetail> {
  Task? _hydrated;
  String? _fetchedId;
  bool _fetchInFlight = false;

  void _hydrateFor(Task active) {
    if (_fetchedId == active.id && (_hydrated != null || _fetchInFlight)) {
      return;
    }
    _fetchedId = active.id;
    _hydrated = null;
    _fetchInFlight = true;
    final client = ref.read(sdkClientProvider);
    client
        .getTask(active.id)
        .then((raw) {
          if (!mounted) return;
          setState(() {
            _fetchInFlight = false;
            try {
              _hydrated = Task.fromJson(raw);
            } catch (_) {
              _hydrated = null;
            }
          });
        })
        .catchError((Object _) {
          if (!mounted) return;
          setState(() => _fetchInFlight = false);
        });
  }

  @override
  Widget build(BuildContext context) {
    final active = ref.watch(activeTaskProvider);
    if (active == null) {
      return const Center(child: SizedBox.shrink());
    }

    // Re-resolve task from the live provider so we always show fresh data
    final tasks = ref.watch(taskProvider).tasks;
    final listed = tasks.where((t) => t.id == active.id).firstOrNull;
    // Prefer the hydrated (steps-bearing) document over the list stub.
    final hydrated = _hydrated;
    final task = (hydrated != null && hydrated.id == active.id)
        ? hydrated
        : (listed ?? active);

    // Detail is reachable from BOTH seams: list tap and reply-chip
    // selection (activeTaskProvider). The authoritative document (and
    // its steps) always come through the getTask seam -- the list row
    // can be stale -- bounded to one fetch per selected id so rebuilds
    // never storm the RPC.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _hydrateFor(active);
    });

    return Container(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              _buildStatusIndicator(task.status),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  task.title.toLowerCase(),
                  style: CyberpunkTypography.headlineLarge.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          // Status selector
          Row(
            children: [
              Text(
                'status',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const SizedBox(width: 16),
              Expanded(child: _buildStatusDropdown(context, ref, task)),
            ],
          ),
          if (!_canTransition(task.status)) ...[
            const SizedBox(height: 12),
            Row(
              children: [
                Text(
                  'cancel',
                  style: CyberpunkTypography.label.copyWith(
                    color: CyberpunkColors.redAlert,
                  ),
                ),
                const Spacer(),
                TextButton.icon(
                  onPressed: () => _showCancelConfirm(context, ref, task),
                  icon: const Icon(Icons.cancel, size: 16),
                  label: const Text(
                    'cancel task',
                    style: CyberpunkTypography.bodySmall,
                  ),
                  style: TextButton.styleFrom(
                    foregroundColor: CyberpunkColors.redAlert,
                  ),
                ),
              ],
            ),
          ],
          const SizedBox(height: 24),
          Text(
            'description',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            task.description.toLowerCase(),
            style: CyberpunkTypography.bodyMedium,
          ),
          const SizedBox(height: 24),
          // 05-navigation T2: hydrated steps render here (title/state/
          // result per gate tasks_detail_test).
          ..._buildSteps(task),
          const Spacer(),
        ],
      ),
    );
  }

  /// Step rows from the hydrated task document.
  List<Widget> _buildSteps(Task task) {
    final steps = task.steps;
    if (steps == null || steps.isEmpty) return const [];
    return [
      Text(
        'steps',
        style: CyberpunkTypography.label.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
      const SizedBox(height: 8),
      Flexible(
        child: ListView(
          shrinkWrap: true,
          children: [
            for (final step in steps)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _buildStatusIndicator(step.status),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            step.description.toLowerCase(),
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.greenSuccess,
                            ),
                          ),
                          if (step.output != null && step.output!.isNotEmpty)
                            Text(
                              step.output!.toLowerCase(),
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.lightGray,
                              ),
                            ),
                        ],
                      ),
                    ),
                    Text(
                      step.status.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.lightGray,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    ];
  }

  Widget _buildStatusDropdown(BuildContext context, WidgetRef ref, Task task) {
    final colors = _allStatusColors();
    final transitions = _validTransitions(task.status);
    final statusColor = colors[task.status.toLowerCase()] ?? Colors.grey;

    if (transitions.isEmpty) {
      // Terminal state — show static text instead of a dropdown
      return Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: statusColor.withValues(alpha: 0.2),
          borderRadius: BorderRadius.circular(3),
        ),
        child: Text(
          task.status.toLowerCase(),
          style: CyberpunkTypography.bodySmall.copyWith(
            color: statusColor,
            fontFamily: 'SourceCodePro',
          ),
        ),
      );
    }

    return DropdownButton<String>(
      value: task.status.toLowerCase(),
      isDense: true,
      isExpanded: true,
      style: CyberpunkTypography.bodySmall.copyWith(
        color: CyberpunkColors.greenSuccess,
        fontFamily: 'SourceCodePro',
      ),
      dropdownColor: CyberpunkColors.darkGray,
      underline: const SizedBox(),
      iconEnabledColor: colors[task.status.toLowerCase()] ?? Colors.grey,
      items: [task.status.toLowerCase(), ..._validTransitions(task.status)].map(
        (status) {
          final color = colors[status.toLowerCase()] ?? Colors.grey;
          return DropdownMenuItem<String>(
            value: status.toLowerCase(),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              decoration: BoxDecoration(
                color: color.withValues(alpha: 0.2),
                borderRadius: BorderRadius.circular(3),
              ),
              child: Text(
                status.toLowerCase(),
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: color,
                  fontFamily: 'SourceCodePro',
                ),
              ),
            ),
          );
        },
      ).toList(),
      onChanged: (value) async {
        if (value == null || value == task.status.toLowerCase()) return;
        final result = await ref
            .read(taskProvider.notifier)
            .updateTaskStatus(task.id, value);
        if (result == false && context.mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: const Text('failed to update task status'),
              backgroundColor: CyberpunkColors.redAlert,
            ),
          );
          // Refetch to restore correct state
          ref.read(taskProvider.notifier).loadTasks();
        }
      },
    );
  }

  /// Show a confirmation dialog before cancelling.
  void _showCancelConfirm(BuildContext context, WidgetRef ref, Task task) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'cancel task',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: Text(
          'are you sure you want to cancel "${task.title}"?',
          style: CyberpunkTypography.bodyMedium,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('dismiss', style: CyberpunkTypography.bodyMedium),
          ),
          TextButton(
            onPressed: () async {
              final messenger = ScaffoldMessenger.of(context);
              Navigator.pop(context);
              final result = await ref
                  .read(taskProvider.notifier)
                  .cancelTask(task.id);
              if (result) {
                // The refreshed task list/provider should update automatically
                messenger.showSnackBar(
                  SnackBar(
                    content: const Text('task cancelled'),
                    backgroundColor: CyberpunkColors.orangePrimary,
                  ),
                );
              } else {
                messenger.showSnackBar(
                  SnackBar(
                    content: const Text('failed to cancel task'),
                    backgroundColor: CyberpunkColors.redAlert,
                  ),
                );
              }
            },
            style: TextButton.styleFrom(
              foregroundColor: CyberpunkColors.redAlert,
            ),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }

  Widget _buildStatusIndicator(String status) {
    final color = _getStatusColor(status);
    return Container(
      width: 10,
      height: 10,
      decoration: BoxDecoration(color: color, shape: BoxShape.circle),
    );
  }

  Color _getStatusColor(String status) {
    switch (status.toLowerCase()) {
      case 'pending':
        return CyberpunkColors.yellowWarning;
      case 'in_progress':
      case 'running':
        return CyberpunkColors.blueInfo;
      case 'completed':
        return CyberpunkColors.greenSuccess;
      case 'failed':
        return CyberpunkColors.redAlert;
      default:
        return Colors.grey;
    }
  }

  /// Returns all status colors for use in dropdown
  Map<String, Color> _allStatusColors() {
    return {
      'pending': CyberpunkColors.yellowWarning,
      'in_progress': CyberpunkColors.blueInfo,
      'running': CyberpunkColors.blueInfo,
      'completed': CyberpunkColors.greenSuccess,
      'failed': CyberpunkColors.redAlert,
    };
  }

  /// Terminal states that cannot be transitioned FROM
  bool _canTransition(String status) {
    return status.toLowerCase() == 'completed' ||
        status.toLowerCase() == 'failed';
  }

  /// Valid next statuses based on current state.
  List<String> _validTransitions(String currentStatus) {
    final lower = currentStatus.toLowerCase();
    switch (lower) {
      case 'pending':
        return ['in_progress', 'completed', 'failed'];
      case 'in_progress':
      case 'running':
        return ['completed', 'failed'];
      case 'completed':
      case 'failed':
        return [];
      default:
        return ['in_progress', 'completed', 'failed'];
    }
  }
}
