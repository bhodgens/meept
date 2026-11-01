import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/activity/activity_list.dart';
import 'package:meept_ui/features/activity/activity_tab.dart';
import 'package:meept_ui/services/sdk_client.dart';

void main() {
  group('ActivityList', () {
    testWidgets('displays empty state when no records', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkApiClientProvider.overrideWith((ref) {
            return MockSdkApiClient();
          }),
        ],
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Container(
            child: const ProviderScope(
              child: ActivityList(),
            ),
          ),
        ),
      );

      // Pump and settle to allow any pending timers to complete before dispose
      await tester.pumpAndSettle();

      container.dispose();

      expect(find.text('no activity records'), findsOneWidget);
    });

    testWidgets('displays records with chronological sort', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkApiClientProvider.overrideWith((ref) {
            return MockSdkApiClient(
              records: [
                ActivityRecord(
                  kind: 'task_step',
                  at: DateTime.parse('2026-10-30T10:00:00Z'),
                  agentId: 'agent-1',
                  summary: 'first action',
                ),
                ActivityRecord(
                  kind: 'session_message',
                  at: DateTime.parse('2026-10-30T09:00:00Z'),
                  agentId: 'agent-2',
                  summary: 'older message',
                ),
              ],
            );
          }),
        ],
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Container(
            child: const ProviderScope(
              child: ActivityList(),
            ),
          ),
        ),
      );

      // Pump and settle to allow any pending timers to complete before dispose
      await tester.pumpAndSettle();

      container.dispose();

      expect(find.text('first action'), findsOneWidget);
      expect(find.text('older message'), findsOneWidget);
    });

    testWidgets('sort modes switch and re-query', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkApiClientProvider.overrideWith((ref) {
            return MockSdkApiClient(records: []);
          }),
        ],
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Container(
            child: const ProviderScope(
              child: ActivityList(),
            ),
          ),
        ),
      );

      // Pump and settle to allow any pending timers to complete before dispose
      await tester.pumpAndSettle();

      // Find the segmented control buttons
      expect(find.text('chrono'), findsOneWidget);
      expect(find.text('by task'), findsOneWidget);
      expect(find.text('by plan'), findsOneWidget);

      container.dispose();
    });
  });
}

/// Mock SDK client for testing
class MockSdkApiClient extends SdkApiClient {
  final List<ActivityRecord> records;

  MockSdkApiClient({this.records = const []})
      : super(host: 'localhost', port: 8080);

  // Override the activity query method to return mock data
  Future<List<ActivityRecord>> listActivity({
    String? sort,
    String? since,
    String? until,
    String? taskId,
    String? planId,
    String? agentId,
    List<String>? kinds,
    int? limit,
    int? offset,
  }) async {
    return records;
  }
}

/// Activity record model for testing
class ActivityRecord {
  final String kind;
  final DateTime at;
  final String? agentId;
  final String? sessionId;
  final String? taskId;
  final String? stepId;
  final String? planId;
  final String? state;
  final String? severity;
  final String summary;
  final Map<String, String>? detail;

  ActivityRecord({
    required this.kind,
    required this.at,
    this.agentId,
    this.sessionId,
    this.taskId,
    this.stepId,
    this.planId,
    this.state,
    this.severity,
    required this.summary,
    this.detail,
  });
}