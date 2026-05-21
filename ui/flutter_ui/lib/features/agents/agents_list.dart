import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';

/// Agents list widget - displays agents (simplified for now)
class AgentsList extends StatelessWidget {
  final List<Agent> agents;
  final ValueChanged<String>? onAgentSelected;

  const AgentsList({
    super.key,
    required this.agents,
    this.onAgentSelected,
  });

  @override
  Widget build(BuildContext context) {
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
                ...agents.map((agent) => _buildAgentTile(agent)),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildAgentTile(Agent agent) {
    return InkWell(
      onTap: () => onAgentSelected?.call(agent.id),
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border.all(color: CyberpunkColors.orangeDark),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Row(
          children: [
            Container(
              width: 8,
              height: 8,
              decoration: BoxDecoration(
                color: agent.enabled
                    ? CyberpunkColors.greenSuccess
                    : Colors.grey,
                shape: BoxShape.circle,
              ),
            ),
            const SizedBox(width: 12),
            Text(
              agent.name.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeGlow,
              ),
            ),
            const Spacer(),
            Text(
              agent.enabled ? 'enabled' : 'disabled',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: agent.enabled
                    ? CyberpunkColors.greenSuccess
                    : Colors.grey,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
