import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../providers/providers.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

/// Tasks panel — shows up to 4 tasks with status icon, title, agent, progress.
class TasksPanel extends ConsumerStatefulWidget {
  const TasksPanel({super.key});

  @override
  ConsumerState<TasksPanel> createState() => _TasksPanelState();
}

class _TasksPanelState extends ConsumerState<TasksPanel> {
  @override
  Widget build(BuildContext context) {
    final taskState = ref.watch(taskProvider);

    if (taskState.isLoading) {
      return const Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }

    final tasks = taskState.tasks.take(4).toList();

    if (tasks.isEmpty) {
      return Center(
        child: Text(
          'no tasks',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: tasks.length,
      itemBuilder: (context, index) {
        final task = tasks[index];
        final statusColor = _statusColor(task.status);
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            borderRadius: BorderRadius.circular(4),
            border: Border.all(color: statusColor.withValues(alpha: 0.3)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: statusColor,
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      task.title.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.orangePrimary,
                        fontWeight: FontWeight.bold,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              if (task.agentId != null)
                Text(
                  'agent: ${task.agentId!.toLowerCase()}',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                    fontSize: 10,
                  ),
                ),
              if (task.description.isNotEmpty)
                Text(
                  task.description.toLowerCase(),
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.lightGray,
                    fontSize: 10,
                  ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              // Progress bar derived from completed/total jobs
              if (task.totalJobs != null && task.totalJobs! > 0)
                Container(
                  margin: const EdgeInsets.only(top: 6),
                  height: 3,
                  decoration: BoxDecoration(
                    color: CyberpunkColors.black,
                    borderRadius: BorderRadius.circular(2),
                  ),
                  child: FractionallySizedBox(
                    alignment: Alignment.centerLeft,
                    widthFactor: ((task.completedJobs ?? 0) / task.totalJobs!).clamp(0.0, 1.0),
                    child: Container(
                      decoration: BoxDecoration(
                        color: statusColor,
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
                ),
            ],
          ),
        );
      },
    );
  }

  Color _statusColor(String? status) {
    switch (status?.toLowerCase()) {
      case 'running':
      case 'active':
        return CyberpunkColors.greenSuccess;
      case 'pending':
      case 'queued':
        return CyberpunkColors.orangePrimary;
      case 'failed':
      case 'error':
        return CyberpunkColors.redAlert;
      case 'completed':
      case 'done':
        return CyberpunkColors.blueInfo;
      default:
        return CyberpunkColors.midGray;
    }
  }
}
