import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/activity/activity_recent_panel.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/features/activity/activity_models.dart';

void main() {
  group('ActivityRecentPanel', () {
    testWidgets('renders the drop counter when dropped > 0', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((ref) {
            return MockSdkApiClient(
              recentResult: ActivityRecentResult(
                records: [],
                dropped: 5,
                total: 0,
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
                  recentResult: ActivityRecentResult(
                    records: [],
                    dropped: 5,
                    total: 0,
                  ),
                );
              }),
            ],
            child: const ActivityRecentPanel(),
          ),
        ),
      );

      // Pump enough times to trigger the 2-second periodic timer
      await tester.pump(const Duration(seconds: 3));
      await tester.pumpAndSettle();

      container.dispose();

      expect(find.text('recent activity'), findsOneWidget);
      expect(find.text('dropped: 5'), findsOneWidget);
    });
  });
}

/// Mock SDK client for recent panel testing
class MockSdkApiClient extends SdkApiClient {
  final ActivityRecentResult? recentResult;

  MockSdkApiClient({this.recentResult}) : super(host: 'localhost', port: 8080);

  @override
  Future<Map<String, dynamic>> getActivityRecent({int? limit = 20}) async {
    if (recentResult == null) {
      return {'records': [], 'dropped': 0, 'total': 0};
    }
    return {
      'records': recentResult!.records.map((r) {
        return {
          'kind': r.kind,
          'at': r.at.toIso8601String(),
          if (r.agentId != null) 'agent_id': r.agentId,
          'summary': r.summary,
        };
      }).toList(),
      'dropped': recentResult!.dropped,
      'total': recentResult!.total,
    };
  }
}
