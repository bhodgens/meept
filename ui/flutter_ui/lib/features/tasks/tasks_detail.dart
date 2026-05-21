import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';

/// Task detail pane - displays task info
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
              'description',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              task.description.toLowerCase(),
              style: CyberpunkTypography.bodyMedium,
            ),
            const Spacer(),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(String status) {
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

  Color _getStatusColor(String status) {
    switch (status.toLowerCase()) {
      case 'pending':
        return CyberpunkColors.yellowWarning;
      case 'in_progress':
      case 'running':
        return CyberpunkColors.blueInfo;
      case 'completed':
        return CyberpunkColors.greenSuccess;
      case 'failed':
        return CyberpunkColors.redAlert;
      default:
        return Colors.grey;
    }
  }
}
