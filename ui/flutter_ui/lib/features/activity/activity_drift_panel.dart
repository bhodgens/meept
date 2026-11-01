import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import 'activity_models.dart';

/// Drift panel - shows per-employee drift scores
class ActivityDriftPanel extends ConsumerStatefulWidget {
  const ActivityDriftPanel({super.key});

  @override
  ConsumerState<ActivityDriftPanel> createState() => _ActivityDriftPanelState();
}

class _ActivityDriftPanelState extends ConsumerState<ActivityDriftPanel> {
  List<AgentOverview> _agents = [];

  @override
  void initState() {
    super.initState();
    _fetchOverview();
  }

  Future<void> _fetchOverview() async {
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.getActivityOverview();
      final result = ActivityOverviewResult.fromJson(raw);
      setState(() {
        _agents = result.byAgent;
      });
    } catch (e) {
      debugPrint('activity overview fetch failed: $e');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'drift scores',
            style: TextStyle(
              color: Colors.orange,
              fontSize: 14,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 8),
          if (_agents.isEmpty)
            const Text(
              'no agent data',
              style: TextStyle(color: Colors.grey, fontSize: 12),
            )
          else
            ..._agents.map((agent) {
              final drift = agent.driftScore ?? 0.0;
              final driftColor = drift > 0.5
                  ? Colors.red
                  : drift > 0.2
                  ? Colors.orange
                  : Colors.green;
              return Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        agent.agentId,
                        style: const TextStyle(
                          color: Colors.white70,
                          fontSize: 12,
                        ),
                      ),
                    ),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: driftColor.withOpacity(0.2),
                        borderRadius: BorderRadius.circular(3),
                      ),
                      child: Text(
                        '${drift.toStringAsFixed(2)}',
                        style: TextStyle(
                          color: driftColor,
                          fontSize: 10,
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ),
                  ],
                ),
              );
            }),
        ],
      ),
    );
  }
}
