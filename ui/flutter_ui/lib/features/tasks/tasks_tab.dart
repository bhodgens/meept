import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../providers/task_provider.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';
import '../../models/api_models.dart';

/// Provider for the currently selected task
final activeTaskProvider = StateProvider<Task?>((ref) => null);

/// Tasks tab - master-detail view with task list and detail
class TasksTab extends ConsumerWidget {
  const TasksTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selectedTask = ref.watch(activeTaskProvider);

    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          TasksList(
            onTaskSelected: (task) {
              ref.read(activeTaskProvider.notifier).state = task;
            },
          ),
          if (selectedTask != null)
            TasksDetail(task: selectedTask),
          if (selectedTask == null)
            Expanded(
              child: Center(
                child: Text(
                  'select a task',
                  style: TextStyle(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}
