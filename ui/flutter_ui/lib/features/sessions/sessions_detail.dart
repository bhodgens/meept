import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';

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
              'created',
              _formatDateTime(session.createdAt),
            ),
            if (session.lastActivity != null)
              _buildDetailRow(
                'last activity',
                _formatDateTime(session.lastActivity!),
              ),
            const Spacer(),
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

  String _formatDateTime(DateTime date) {
    return '${date.year}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')}';
  }
}
