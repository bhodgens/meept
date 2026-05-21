import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/task.dart';

/// Tasks list widget - displays all tasks with status indicators
class TasksList extends StatelessWidget {
  final List<Task> tasks;
  final String? selectedTaskId;
  final ValueChanged<Task> onTaskSelected;

  const TasksList({
    super.key,
    required this.tasks,
    this.selectedTaskId,
    required this.onTaskSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 320,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withOpacity(0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Text(
                  'tasks',
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                IconButton(
                  icon: const Icon(Icons.add, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: () {
                    // Create new task
                  },
                ),
              ],
            ),
          ),
          Expanded(
            child: ListView.builder(
              itemCount: tasks.length,
              itemBuilder: (context, index) {
                final task = tasks[index];
                final isSelected = task.id == selectedTaskId;
                return _buildTaskTile(task, isSelected);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTaskTile(Task task, bool isSelected) {
    return InkWell(
      onTap: () => onTaskSelected(task),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withOpacity(0.1)
              : null,
          border: Border(
            left: BorderSide(
              color: _getStatusColor(task.status),
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: [
            _buildStatusIndicator(task.status),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    task.title.toLowerCase(),
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.greenSuccess,
                    ),
                  ),
                  if (task.lastActivityAt != null)
                    Text(
                      _formatLastActivity(task.lastActivityAt!),
                      style: CyberpunkTypography.bodySmall,
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(TaskStatus status) {
    final color = _getStatusColor(status);
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
    );
  }

  Color _getStatusColor(TaskStatus status) {
    switch (status) {
      case TaskStatus.pending:
        return CyberpunkColors.yellowWarning;
      case TaskStatus.running:
        return CyberpunkColors.blueInfo;
      case TaskStatus.complete:
        return CyberpunkColors.greenSuccess;
      case TaskStatus.error:
        return CyberpunkColors.redAlert;
    }
  }

  String _formatLastActivity(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }
}
