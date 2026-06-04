import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../providers/providers.dart';
import '../../../theme/colors.dart';
import '../../../theme/typography.dart';

/// Agent Activity panel — shows active agents with state icons and iteration progress.
class AgentActivityPanel extends ConsumerStatefulWidget {
  const AgentActivityPanel({super.key});

  @override
  ConsumerState<AgentActivityPanel> createState() => _AgentActivityPanelState();
}

class _AgentActivityPanelState extends ConsumerState<AgentActivityPanel> {
  @override
  Widget build(BuildContext context) {
    final agents = ref.watch(agentProvider);

    if (agents.isLoading) {
      return const Center(
        child: SizedBox(
          width: 24,
          height: 24,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }

    if (agents.agents.isEmpty) {
      return Center(
        child: Text(
          'no agents',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.midGray,
          ),
        ),
      );
    }

    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: agents.agents.length,
      itemBuilder: (context, index) {
        final agent = agents.agents[index];
        return Container(
          margin: const EdgeInsets.only(bottom: 8),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: CyberpunkColors.darkGray,
            borderRadius: BorderRadius.circular(4),
            border: Border.all(
              color: agent.enabled == true
                  ? CyberpunkColors.greenSuccess.withValues(alpha: 0.3)
                  : CyberpunkColors.redAlert.withValues(alpha: 0.3),
            ),
          ),
          child: Row(
            children: [
              Container(
                width: 8,
                height: 8,
                decoration: BoxDecoration(
                  color: agent.enabled == true
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
                  shape: BoxShape.circle,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      agent.name.toLowerCase(),
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.orangePrimary,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      agent.id,
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: CyberpunkColors.midGray,
                        fontSize: 10,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}
