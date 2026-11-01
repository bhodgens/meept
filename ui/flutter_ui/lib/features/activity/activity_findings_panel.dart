import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import 'activity_models.dart';

/// Findings panel - lists audit findings with severity/resolved status
class ActivityFindingsPanel extends ConsumerStatefulWidget {
  const ActivityFindingsPanel({super.key});

  @override
  ConsumerState<ActivityFindingsPanel> createState() =>
      _ActivityFindingsPanelState();
}

class _ActivityFindingsPanelState extends ConsumerState<ActivityFindingsPanel> {
  List<AuditFinding> _findings = [];

  @override
  void initState() {
    super.initState();
    _fetchFindings();
  }

  Future<void> _fetchFindings() async {
    try {
      final client = ref.read(sdkClientProvider);
      final raw = await client.getActivityFindings(limit: 10);
      final result = ActivityFindingsResult.fromJson(raw);
      setState(() {
        _findings = result.findings;
      });
    } catch (e) {
      debugPrint('activity findings fetch failed: $e');
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
            'findings',
            style: TextStyle(
              color: Colors.orange,
              fontSize: 14,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 8),
          if (_findings.isEmpty)
            const Text(
              'no findings',
              style: TextStyle(color: Colors.grey, fontSize: 12),
            )
          else
            ..._findings.map((finding) {
              final severityColor = finding.severity == 'critical'
                  ? Colors.red
                  : finding.severity == 'warning'
                  ? Colors.orange
                  : Colors.green;
              return Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  children: [
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 2,
                      ),
                      decoration: BoxDecoration(
                        color: severityColor.withOpacity(0.2),
                        borderRadius: BorderRadius.circular(3),
                      ),
                      child: Text(
                        finding.severity,
                        style: TextStyle(
                          color: severityColor,
                          fontSize: 10,
                          fontFamily: 'SourceCodePro',
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        finding.summary.toLowerCase(),
                        style: const TextStyle(
                          color: Colors.white70,
                          fontSize: 12,
                        ),
                      ),
                    ),
                    Text(
                      finding.resolved ? 'resolved' : 'open',
                      style: const TextStyle(color: Colors.grey, fontSize: 10),
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
