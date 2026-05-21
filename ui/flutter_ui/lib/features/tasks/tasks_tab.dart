import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../models/api_models.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';

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
          TasksList(
            tasks: _getTasks(),
            selectedTaskId: _selectedTask?.id,
            onTaskSelected: (task) => setState(() => _selectedTask = task),
          ),
          if (_selectedTask != null)
            TasksDetail(task: _selectedTask!),
        ],
      ),
    );
  }

  List<Task> _getTasks() {
    // TODO: Replace with Riverpod provider
    return [
      Task(
        id: 'task-001',
        title: 'Implement HTTP API Endpoints',
        description: 'Create REST API endpoints for the Flutter client',
        status: 'in_progress',
        createdAt: DateTime.now().subtract(const Duration(hours: 3)),
      ),
      Task(
        id: 'task-002',
        title: 'fix flutter ui theme',
        description: 'Update the Flutter UI to use the ORANGE VOID theme',
        status: 'pending',
        createdAt: DateTime.now().subtract(const Duration(hours: 1)),
      ),
      Task(
        id: 'task-003',
        title: 'update documentation',
        description: 'Update the project documentation with new features',
        status: 'completed',
        createdAt: DateTime.now().subtract(const Duration(days: 1)),
        completedAt: DateTime.now().subtract(const Duration(hours: 5)),
      ),
    ];
  }
}
