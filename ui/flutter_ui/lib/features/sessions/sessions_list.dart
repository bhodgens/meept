import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';

/// Sessions list widget - displays all sessions with selection
class SessionsList extends StatelessWidget {
  final List<Session> sessions;
  final String? selectedSessionId;
  final ValueChanged<String> onSessionSelected;

  const SessionsList({
    super.key,
    required this.sessions,
    this.selectedSessionId,
    required this.onSessionSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 280,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withOpacity(0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Text(
                  'sessions',
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                IconButton(
                  icon: const Icon(Icons.add, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: () {
                    // Create new session
                  },
                ),
              ],
            ),
          ),
          Expanded(
            child: ListView.builder(
              itemCount: sessions.length,
              itemBuilder: (context, index) {
                final session = sessions[index];
                final isSelected = session.id == selectedSessionId;
                return _buildSessionTile(session, isSelected);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSessionTile(Session session, bool isSelected) {
    return InkWell(
      onTap: () => onSessionSelected(session.id),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withOpacity(0.1)
              : null,
          border: Border(
            left: BorderSide(
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              session.title.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: isSelected
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.greenSuccess,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              _formatLastActivity(session.updatedAt),
              style: CyberpunkTypography.bodySmall,
            ),
          ],
        ),
      ),
    );
  }

  String _formatLastActivity(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }
}
