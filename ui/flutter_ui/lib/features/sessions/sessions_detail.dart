import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/session.dart';

/// Session detail pane - displays in-depth session information
class SessionsDetailPane extends StatelessWidget {
  final Session session;

  const SessionsDetailPane({super.key, required this.session});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'session details',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 24),
            _buildDetailRow(
              'title',
              session.title.toLowerCase(),
            ),
            _buildDetailRow(
              'duration',
              _formatDuration(session.duration),
            ),
            _buildDetailRow(
              'tokens',
              '${session.tokenCount}',
            ),
            _buildDetailRow(
              'status',
              session.status.toLowerCase(),
            ),
            const Spacer(),
            _buildTasksSection(),
          ],
        ),
      ),
    );
  }

  Widget _buildDetailRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 100,
            child: Text(
              label,
              style: CyberpunkTypography.bodySmall,
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.greenSuccess,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTasksSection() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(color: CyberpunkColors.midGray),
        const SizedBox(height: 8),
        Text(
          'associated tasks',
          style: CyberpunkTypography.label.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        const SizedBox(height: 12),
        ...session.taskIds.map((taskId) => _buildTaskChip(taskId)),
      ],
    );
  }

  Widget _buildTaskChip(String taskId) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.orangePrimary.withOpacity(0.1),
        border: Border.all(color: CyberpunkColors.orangePrimary),
        borderRadius: BorderRadius.circular(2),
      ),
      child: Text(
        taskId.substring(0, 8).toLowerCase(),
        style: CyberpunkTypography.label.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }

  String _formatDuration(Duration duration) {
    final hours = duration.inHours;
    final minutes = duration.inMinutes.remainder(60);
    final seconds = duration.inSeconds.remainder(60);
    return '${hours}h ${minutes}m ${seconds}s';
  }
}
