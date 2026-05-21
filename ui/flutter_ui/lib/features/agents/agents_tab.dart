import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'agents_list.dart';
import '../../models/api_models.dart';

/// Agents tab - displays all available agents
class AgentsTab extends StatelessWidget {
  const AgentsTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: AgentsList(
        agents: _getAgents(),
        onAgentSelected: (agentId) {
          // Open agent details
        },
      ),
    );
  }

  List<Agent> _getAgents() {
    // TODO: Replace with Riverpod provider
    return [
      Agent(
        id: 'chat',
        name: 'chat',
        description: 'General conversation agent',
        prompt: 'You are a helpful chat assistant.',
        enabled: true,
      ),
      Agent(
        id: 'coder',
        name: 'coder',
        description: 'Code writing and editing agent',
        prompt: 'You are a coding expert.',
        enabled: true,
      ),
      Agent(
        id: 'debugger',
        name: 'debugger',
        description: 'Debugging and troubleshooting agent',
        prompt: 'You are a debugging expert.',
        enabled: true,
      ),
      Agent(
        id: 'analyst',
        name: 'analyst',
        description: 'Research and analysis agent',
        prompt: 'You are a research analyst.',
        enabled: false,
      ),
    ];
  }
}
