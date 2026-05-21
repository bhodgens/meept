import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/task.dart';

/// Task detail pane - displays task info and agent list
class TasksDetail extends StatelessWidget {
  final Task task;

  const TasksDetail({super.key, required this.task});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                _buildStatusIndicator(task.status),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    task.title.toLowerCase(),
                    style: CyberpunkTypography.headlineLarge.copyWith(
                      color: CyberpunkColors.orangePrimary,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 24),
            Text(
              'agents',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.builder(
                itemCount: task.agentIds.length,
                itemBuilder: (context, index) {
                  final agentId = task.agentIds[index];
                  return _buildAgentTile(agentId);
                },
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildAgentTile(String agentId) {
    return InkWell(
      onTap: () {
        // Open agent transcript
      },
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border.all(color: CyberpunkColors.orangeDark),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Text(
          agentId.toLowerCase(),
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.greenSuccess,
          ),
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(TaskStatus status) {
    final color = _getStatusColor(status);
    return Container(
      width: 10,
      height: 10,
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
}
