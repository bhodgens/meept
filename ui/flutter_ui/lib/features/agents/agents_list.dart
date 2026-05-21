import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/agent.dart';
import '../../models/task.dart';

/// Agents list widget - displays agents grouped by task
class AgentsList extends StatelessWidget {
  final List<Agent> agents;
  final List<Task> tasks;
  final ValueChanged<String>? onAgentSelected;

  const AgentsList({
    super.key,
    required this.agents,
    required this.tasks,
    this.onAgentSelected,
  });

  @override
  Widget build(BuildContext context) {
    final agentsByTask = _groupAgentsByTask(agents, tasks);

    return Container(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'agents',
            style: CyberpunkTypography.headlineMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 16),
          Expanded(
            child: ListView(
              children: [
                ...agentsByTask.entries.map((entry) {
                  final task = entry.key;
                  final taskAgents = entry.value;
                  return _buildTaskGroup(task, taskAgents);
                }),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Map<Task, List<Agent>> _groupAgentsByTask(List<Agent> agents, List<Task> tasks) {
    final result = <Task, List<Agent>>{};
    for (final agent in agents) {
      if (agent.currentTaskId != null) {
        try {
          final task = tasks.firstWhere(
            (t) => t.id == agent.currentTaskId,
          );
          result.putIfAbsent(task, () => []).add(agent);
        } catch (e) {
          // Task not found, skip this agent
        }
      }
    }
    return result;
  }

  Widget _buildTaskGroup(Task task, List<Agent> agents) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Text(
            task.title.toLowerCase(),
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.greenSuccess,
            ),
          ),
        ),
        ...agents.map((agent) => _buildAgentTile(agent)),
      ],
    );
  }

  Widget _buildAgentTile(Agent agent) {
    return InkWell(
      onTap: () => onAgentSelected?.call(agent.id),
      child: Container(
        margin: const EdgeInsets.only(bottom: 4, left: 16),
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border(
            left: BorderSide(
              color: _getStatusColor(agent.status),
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: [
            _buildStatusIndicator(agent.status),
            const SizedBox(width: 8),
            Text(
              agent.name.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeGlow,
              ),
            ),
            const Spacer(),
            Text(
              agent.status.name.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: _getStatusColor(agent.status),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(AgentStatus status) {
    return Container(
      width: 6,
      height: 6,
      decoration: BoxDecoration(
        color: _getStatusColor(status),
        shape: BoxShape.circle,
      ),
    );
  }

  Color _getStatusColor(AgentStatus status) {
    switch (status) {
      case AgentStatus.idle:
        return Colors.grey;
      case AgentStatus.working:
        return CyberpunkColors.blueInfo;
      case AgentStatus.complete:
        return CyberpunkColors.greenSuccess;
      case AgentStatus.error:
        return CyberpunkColors.redAlert;
    }
  }
}
