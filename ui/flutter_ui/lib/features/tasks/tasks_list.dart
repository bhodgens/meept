import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';

/// Tasks list widget - displays all tasks for the active session
class TasksList extends ConsumerStatefulWidget {
  final ValueChanged<Task>? onTaskSelected;

  const TasksList({super.key, this.onTaskSelected});

  @override
  ConsumerState<TasksList> createState() => _TasksListState();
}

class _TasksListState extends ConsumerState<TasksList> {
  final _textController = TextEditingController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(taskProvider.notifier).loadTasks();
    });
  }

  @override
  void dispose() {
    _textController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final taskState = ref.watch(taskProvider);

    return Container(
      width: 280,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
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
                  onPressed: _showCreateTaskDialog,
                ),
              ],
            ),
          ),
          if (taskState.isLoading)
            const Expanded(
              child: Center(
                child: CircularProgressIndicator(),
              ),
            )
          else if (taskState.error != null)
            Expanded(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  SizedBox(
                    width: double.infinity,
                    child: _TaskErrorBanner(message: taskState.error!),
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () => ref.read(taskProvider.notifier).loadTasks(),
                    child: const Text('retry', style: CyberpunkTypography.bodySmall),
                  ),
                ],
              ),
            )
          else if (taskState.tasks.isEmpty)
            const Expanded(
              child: Center(
                child: Text('no tasks'),
              ),
            )
          else
            Expanded(
              child: ListView.builder(
                itemCount: taskState.tasks.length,
                itemBuilder: (context, index) {
                  final task = taskState.tasks[index];
                  return _buildTaskTile(task);
                },
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildTaskTile(Task task) {
    final statusColor = _getStatusColor(task.status);
    return InkWell(
      onTap: () {
        widget.onTaskSelected?.call(task);
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          border: Border(
            left: BorderSide(
              color: statusColor,
              width: 2,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              task.title.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.greenSuccess,
              ),
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
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
                ),
                const Spacer(),
                Text(
                  _formatAge(task.createdAt),
                  style: CyberpunkTypography.bodySmall,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Color _getStatusColor(String status) {
    switch (status.toLowerCase()) {
      case 'pending':
        return CyberpunkColors.orangePrimary;
      case 'running':
        return CyberpunkColors.blueInfo;
      case 'completed':
        return CyberpunkColors.greenSuccess;
      case 'failed':
        return CyberpunkColors.redAlert;
      default:
        return CyberpunkColors.midGray;
    }
  }

  String _formatAge(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }

  void _showCreateTaskDialog() async {
    _textController.clear();
    await showDialog(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: CyberpunkColors.darkGray,
        title: const Text(
          'create task',
          style: CyberpunkTypography.headlineMedium,
        ),
        content: TextField(
          controller: _textController,
          style: CyberpunkTypography.bodyMedium,
          decoration: const InputDecoration(
            hintText: 'enter task title...',
            hintStyle: CyberpunkTypography.bodySmall,
          ),
          maxLines: 3,
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('cancel', style: CyberpunkTypography.bodyMedium),
          ),
          FilledButton(
            onPressed: () {
              final title = _textController.text.trim();
              if (title.isNotEmpty) {
                ref.read(taskProvider.notifier).createTask(title: title);
                Navigator.pop(context);
              }
            },
            child: const Text('create', style: CyberpunkTypography.bodyMedium),
          ),
        ],
      ),
    );
  }
}

/// Inline error banner for task list errors
class _TaskErrorBanner extends StatelessWidget {
  final String message;

  const _TaskErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              'error: $message',
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
