import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class TasksTab extends StatelessWidget {
  const TasksTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                'TASKS',
                style: CyberpunkTypography.headlineLarge,
              ),
              const Spacer(),
              ElevatedButton.icon(
                onPressed: () {
                  // Create new task
                },
                icon: const Icon(Icons.add, size: 18),
                label: const Text('NEW TASK'),
              ),
            ],
          ),
          const SizedBox(height: 16),
          // Task stats - placeholder
          _buildTaskStats(),
          const SizedBox(height: 16),
          // Task list - placeholder
          Expanded(
            child: Center(
              child: Text(
                'TASK_LIST_PLACEHOLDER',
                style: CyberpunkTypography.bodyMedium,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTaskStats() {
    return Row(
      children: [
        _buildStatChip('PENDING', '5', CyberpunkColors.yellowWarning),
        const SizedBox(width: 8),
        _buildStatChip('RUNNING', '2', CyberpunkColors.blueInfo),
        const SizedBox(width: 8),
        _buildStatChip('COMPLETED', '15', CyberpunkColors.greenSuccess),
        const SizedBox(width: 8),
        _buildStatChip('FAILED', '1', CyberpunkColors.redAlert),
      ],
    );
  }

  Widget _buildStatChip(String label, String value, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: color.withOpacity(0.1),
        border: Border.all(color: color, width: 1),
        borderRadius: BorderRadius.circular(2),
      ),
      child: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: '$value ',
              style: TextStyle(
                fontFamily: 'JetBrainsMono',
                color: color,
                fontSize: 11,
                fontWeight: FontWeight.w600,
                letterSpacing: 1,
              ),
            ),
            TextSpan(
              text: label,
              style: TextStyle(
                fontFamily: 'JetBrainsMono',
                color: color,
                fontSize: 10,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
