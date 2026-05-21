import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'agents_list.dart';
import '../../models/agent.dart';
import '../../models/task.dart';

/// Agents tab - displays agents grouped by their assigned tasks
class AgentsTab extends StatelessWidget {
  const AgentsTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: AgentsList(
        agents: _getAgents(),
        tasks: _getTasks(),
        onAgentSelected: (agentId) {
          // Open agent transcript
        },
      ),
    );
  }

  List<Agent> _getAgents() {
    // TODO: Replace with Riverpod provider
    return [
      Agent(
        id: 'coder-01',
        name: 'coder',
        status: AgentStatus.working,
        currentTaskId: 'task-001',
        lastActiveAt: DateTime.now(),
      ),
      Agent(
        id: 'debugger-01',
        name: 'debugger',
        status: AgentStatus.working,
        currentTaskId: 'task-001',
        lastActiveAt: DateTime.now(),
      ),
      Agent(
        id: 'analyst-01',
        name: 'analyst',
        status: AgentStatus.complete,
        currentTaskId: 'task-003',
        lastActiveAt: DateTime.now().subtract(const Duration(hours: 5)),
      ),
    ];
  }

  List<Task> _getTasks() {
    // TODO: Replace with Riverpod provider
    return [
      Task(
        id: 'task-001',
        title: 'Implement HTTP API Endpoints',
        status: TaskStatus.running,
        createdAt: DateTime.now().subtract(const Duration(hours: 3)),
        agentIds: ['coder', 'debugger'],
      ),
      Task(
        id: 'task-003',
        title: 'update documentation',
        status: TaskStatus.complete,
        createdAt: DateTime.now().subtract(const Duration(days: 1)),
        agentIds: ['analyst'],
      ),
    ];
  }
}
