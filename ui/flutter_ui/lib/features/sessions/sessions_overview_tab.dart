import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class SessionsOverviewTab extends StatelessWidget {
  const SessionsOverviewTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                'SESSIONS',
                style: CyberpunkTypography.headlineLarge,
              ),
              const Spacer(),
              ElevatedButton.icon(
                onPressed: () {
                  // Create new session
                },
                icon: const Icon(Icons.add, size: 18),
                label: const Text('NEW SESSION'),
              ),
            ],
          ),
          const SizedBox(height: 16),
          // Session stats - placeholder
          _buildSessionStats(),
          const SizedBox(height: 16),
          // Session list - placeholder
          Expanded(
            child: Center(
              child: Text(
                'SESSION_LIST_PLACEHOLDER',
                style: CyberpunkTypography.bodyMedium,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSessionStats() {
    return Row(
      children: [
        _buildStatCard('total', '24', CyberpunkColors.orangePrimary),
        const SizedBox(width: 12),
        _buildStatCard('active', '3', CyberpunkColors.greenSuccess),
        const SizedBox(width: 12),
        _buildStatCard('completed', '18', CyberpunkColors.blueInfo),
        const SizedBox(width: 12),
        _buildStatCard('tokens', '156K', Colors.purple),
      ],
    );
  }

  Widget _buildStatCard(String label, String value, Color color) {
    return Container(
      width: 120,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: color.withOpacity(0.1),
        border: Border.all(color: color, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        children: [
          Text(
            value,
            style: CyberpunkTypography.headlineMedium.copyWith(
              color: color,
              fontSize: 28,
            ),
          ),
          Text(
            label.toUpperCase(),
            style: CyberpunkTypography.bodySmall.copyWith(
              color: color.withOpacity(0.8),
              fontSize: 10,
              letterSpacing: 1,
            ),
          ),
        ],
      ),
    );
  }
}
