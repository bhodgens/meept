import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class AgentsTab extends StatelessWidget {
  const AgentsTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'AGENTS',
            style: CyberpunkTypography.headlineLarge,
          ),
          const SizedBox(height: 8),
          Text(
            'select an agent for your task',
            style: CyberpunkTypography.bodySmall,
          ),
          const SizedBox(height: 24),
          Expanded(
            child: Center(
              child: Text(
                'AGENT_GRID_PLACEHOLDER',
                style: CyberpunkTypography.bodyMedium,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
