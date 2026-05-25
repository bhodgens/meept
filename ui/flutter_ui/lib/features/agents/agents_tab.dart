import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';

/// Agents tab - displays all available agents
class AgentsTab extends ConsumerStatefulWidget {
  const AgentsTab({super.key});

  @override
  ConsumerState<AgentsTab> createState() => _AgentsTabState();
}

class _AgentsTabState extends ConsumerState<AgentsTab> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(agentProvider.notifier).loadAgents();
    });
  }

  @override
  Widget build(BuildContext context) {
    final agentState = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    return Container(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                'agents',
                style: CyberpunkTypography.headlineMedium.copyWith(
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.refresh, size: 18),
                color: CyberpunkColors.orangePrimary,
                onPressed: () {
                  ref.read(agentProvider.notifier).loadAgents();
                },
              ),
            ],
          ),
          const SizedBox(height: 16),
          if (agentState.isLoading)
            const Center(
              child: CircularProgressIndicator(),
            )
          else if (agentState.error != null)
            Center(
              child: Column(
                children: [
                  SizedBox(
                    width: 280,
                    child: _AgentErrorBanner(message: agentState.error!),
                  ),
                  const SizedBox(height: 12),
                  FilledButton.tonal(
                    onPressed: () => ref.read(agentProvider.notifier).loadAgents(),
                    child: const Text('retry', style: CyberpunkTypography.bodySmall),
                  ),
                ],
              ),
            )
          else if (agentState.agents.isEmpty)
            const Center(
              child: Text('no agents available'),
            )
          else
            Expanded(
              child: GridView.builder(
                gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                  crossAxisCount: 2,
                  crossAxisSpacing: 16,
                  mainAxisSpacing: 16,
                  childAspectRatio: 1.5,
                ),
                itemCount: agentState.agents.length,
                itemBuilder: (context, index) {
                  final agent = agentState.agents[index];
                  final isSelected = activeAgent?.id == agent.id;
                  return _buildAgentCard(agent, isSelected);
                },
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildAgentCard(Agent agent, bool isSelected) {
    return InkWell(
      onTap: () {
        ref.read(activeAgentProvider.notifier).state = agent;
      },
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.1)
              : CyberpunkColors.black,
          border: Border.all(
            color: isSelected
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.midGray,
            width: 1,
          ),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  _getAgentIcon(agent.id),
                  color: isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.greenSuccess,
                  size: 24,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    agent.name.toLowerCase(),
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.greenSuccess,
                      fontFamily: 'SourceCodePro',
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              agent.id.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ],
        ),
      ),
    );
  }

  IconData _getAgentIcon(String agentId) {
    switch (agentId.toLowerCase()) {
      case 'coder':
        return Icons.code;
      case 'debugger':
        return Icons.bug_report;
      case 'planner':
        return Icons.account_tree;
      case 'analyst':
        return Icons.analytics;
      case 'chat':
        return Icons.chat;
      case 'committer':
        return Icons.cloud_upload;
      case 'scheduler':
        return Icons.event_note;
      case 'dispatcher':
        return Icons.forward_to_inbox;
      default:
        return Icons.smart_toy;
    }
  }
}

/// Inline error banner for agent list errors
class _AgentErrorBanner extends StatelessWidget {
  final String message;

  const _AgentErrorBanner({required this.message});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.redAlert.withValues(alpha: 0.2),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: CyberpunkColors.redAlert, size: 20),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.redAlert,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}
