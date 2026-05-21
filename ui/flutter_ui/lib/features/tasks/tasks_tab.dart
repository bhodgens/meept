import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';
import '../../models/api_models.dart';

/// Tasks tab - master-detail view with task list and detail
class TasksTab extends StatefulWidget {
  const TasksTab({super.key});

  @override
  State<TasksTab> createState() => _TasksTabState();
}

class _TasksTabState extends State<TasksTab> {
  Task? _selectedTask;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          TasksList(),
          if (_selectedTask != null)
            TasksDetail(task: _selectedTask!),
          if (_selectedTask == null)
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
