import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/activity/activity_drift_panel.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/features/activity/activity_models.dart';

void main() {
  group('ActivityDriftPanel', () {
    testWidgets('renders drift score 0.4 when mock returns it', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((ref) {
            return MockSdkApiClient(
              overviewResult: ActivityOverviewResult(
                byAgent: [
                  AgentOverview(
                    agentId: 'agent-1',
                    driftScore: 0.4,
                    activeTasks: 0,
                    openFindings: 0,
                  ),
                ],
                generatedAt: DateTime.now(),
              ),
            );
          }),
        ],
      );

      await tester.pumpWidget(
        MaterialApp(
          home: ProviderScope(
            overrides: [
              sdkClientProvider.overrideWith((ref) {
                return MockSdkApiClient(
                  overviewResult: ActivityOverviewResult(
                    byAgent: [
                      AgentOverview(
                        agentId: 'agent-1',
                        driftScore: 0.4,
                        activeTasks: 0,
                        openFindings: 0,
                      ),
                    ],
                    generatedAt: DateTime.now(),
                  ),
                );
              }),
            ],
            child: const ActivityDriftPanel(),
          ),
        ),
      );

      await tester.pumpAndSettle();

      container.dispose();

      expect(find.text('drift scores'), findsOneWidget);
      expect(find.text('agent-1'), findsOneWidget);
      expect(find.text('0.40'), findsOneWidget);
    });
  });
}

/// Mock SDK client for drift panel testing
class MockSdkApiClient extends SdkApiClient {
  final ActivityOverviewResult? overviewResult;

  MockSdkApiClient({this.overviewResult})
    : super(host: 'localhost', port: 8080);

  @override
  Future<Map<String, dynamic>> getActivityOverview() async {
    if (overviewResult == null) return {};
    final result = ActivityOverviewResult.fromJson({
      'by_agent': overviewResult!.byAgent.map((a) {
        return {
          'agent_id': a.agentId,
          'drift_score': a.driftScore ?? 0.0,
          'active_tasks': a.activeTasks,
          'open_findings': a.openFindings,
        };
      }).toList(),
      'generated_at': DateTime.now().toIso8601String(),
    });
    return {
      'by_agent': result.byAgent.map((a) {
        return {
          'agent_id': a.agentId,
          'drift_score': a.driftScore ?? 0.0,
          'active_tasks': a.activeTasks,
          'open_findings': a.openFindings,
        };
      }).toList(),
      'generated_at': result.generatedAt.toIso8601String(),
    };
  }
}
