import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/task.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';

/// Tasks tab - master-detail view with task list and agent detail
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
        status: TaskStatus.running,
        createdAt: DateTime.now().subtract(const Duration(hours: 3)),
        lastActivityAt: DateTime.now(),
        agentIds: ['coder', 'debugger'],
        sessionId: 'session-001',
      ),
      Task(
        id: 'task-002',
        title: 'fix flutter ui theme',
        status: TaskStatus.pending,
        createdAt: DateTime.now().subtract(const Duration(hours: 1)),
        agentIds: ['coder'],
        sessionId: 'session-001',
      ),
      Task(
        id: 'task-003',
        title: 'update documentation',
        status: TaskStatus.complete,
        createdAt: DateTime.now().subtract(const Duration(days: 1)),
        lastActivityAt: DateTime.now().subtract(const Duration(hours: 5)),
        agentIds: ['analyst'],
        sessionId: 'session-002',
      ),
    ];
  }
}
