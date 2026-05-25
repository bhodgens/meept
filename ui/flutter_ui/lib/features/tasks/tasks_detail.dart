import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import 'tasks_tab.dart' show activeTaskProvider;

/// Task detail pane - displays task info with status controls
class TasksDetail extends ConsumerWidget {
  const TasksDetail({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final active = ref.watch(activeTaskProvider);
    if (active == null) {
      return const Expanded(
        child: Center(child: SizedBox.shrink()),
      );
    }

    // Re-resolve task from the live provider so we always show fresh data
    final tasks = ref.watch(taskProvider).tasks;
    final task = tasks.where((t) => t.id == active.id).firstOrNull ?? active;

    return Expanded(
      child: Container(
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
                Expanded(
                  child: _buildStatusDropdown(context, ref, task),
                ),
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
                    label: Text(
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
            const Spacer(),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusDropdown(
    BuildContext context, WidgetRef ref, Task task,
  ) {
    final colors = _allStatusColors();
    final isTerminal = _canTransition(task.status);

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
      items: _validTransitions(task.status).map((status) {
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
      }).toList(),
      onChanged: isTerminal
          ? null
          : (value) async {
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
  void _showCancelConfirm(
    BuildContext context, WidgetRef ref, Task task,
  ) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'cancel task',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: Text(
          'Are you sure you want to cancel "${task.title}"?',
          style: CyberpunkTypography.bodyMedium,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('dismiss', style: CyberpunkTypography.bodyMedium),
          ),
          TextButton(
            onPressed: () async {
              Navigator.pop(context);
              final result = await ref
                  .read(taskProvider.notifier)
                  .cancelTask(task.id);
              if (context.mounted) {
                if (result) {
                  // The refreshed task list/provider should update automatically
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(
                      content: Text('task cancelled'),
                      backgroundColor: CyberpunkColors.orangePrimary,
                    ),
                  );
                } else {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(
                      content: const Text('failed to cancel task'),
                      backgroundColor: CyberpunkColors.redAlert,
                    ),
                  );
                }
              }
            },
            child: const Text(
              'cancel',
              style: CyberpunkTypography.bodyMedium,
            ),
            style: TextButton.styleFrom(
              foregroundColor: CyberpunkColors.redAlert,
            ),
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
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
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
