import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../widgets/background_image.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';
import '../../models/api_models.dart';

/// Provider for the currently selected task
final activeTaskProvider = StateProvider<Task?>((ref) => null);

/// Tasks tab - master-detail view with task list and detail panes
class TasksTab extends ConsumerWidget {
  const TasksTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selectedTask = ref.watch(activeTaskProvider);

    return Row(
      children: [
        SizedBox(
          width: 300,
          // 05-navigation T2: the list tap must SELECT the task (write
          // activeTaskProvider) -- without this callback the detail pane
          // was unreachable (gate tasks_detail_test).
          child: TasksList(
            onTaskSelected: (task) {
              ref.read(activeTaskProvider.notifier).state = task;
            },
          ),
        ),
        const VerticalDivider(width: 1),
        Expanded(
          child: BackgroundImage(
            child: selectedTask != null
                ? const TasksDetail()
                : Center(
                    child: Text(
                      'select a task',
                      style: TextStyle(color: CyberpunkColors.orangePrimary),
                    ),
                  ),
          ),
        ),
      ],
    );
  }
}
