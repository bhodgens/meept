import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/activity/activity_findings_panel.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/features/activity/activity_models.dart';

void main() {
  group('ActivityFindingsPanel', () {
    testWidgets('renders finding severity and resolved state', (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((ref) {
            return MockSdkApiClient(
              findingsResult: ActivityFindingsResult(
                findings: [
                  AuditFinding(
                    id: 'f-1',
                    employeeId: 'emp-1',
                    severity: 'critical',
                    summary: 'security issue',
                    resolved: false,
                  ),
                ],
                total: 1,
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
                  findingsResult: ActivityFindingsResult(
                    findings: [
                      AuditFinding(
                        id: 'f-1',
                        employeeId: 'emp-1',
                        severity: 'critical',
                        summary: 'security issue',
                        resolved: false,
                      ),
                    ],
                    total: 1,
                  ),
                );
              }),
            ],
            child: const ActivityFindingsPanel(),
          ),
        ),
      );

      await tester.pumpAndSettle();

      container.dispose();

      expect(find.text('findings'), findsOneWidget);
      expect(find.text('critical'), findsOneWidget);
      expect(find.text('security issue'), findsOneWidget);
      expect(find.text('open'), findsOneWidget);
    });
  });
}

/// Mock SDK client for findings panel testing
class MockSdkApiClient extends SdkApiClient {
  final ActivityFindingsResult? findingsResult;

  MockSdkApiClient({this.findingsResult})
    : super(host: 'localhost', port: 8080);

  @override
  Future<Map<String, dynamic>> getActivityFindings({
    String? employeeId,
    String? since,
    String? severity,
    bool? resolved,
    int? limit,
    int? offset,
  }) async {
    if (findingsResult == null) return {'findings': [], 'total': 0};
    final result = ActivityFindingsResult.fromJson({
      'findings': findingsResult!.findings.map((f) {
        return {
          'id': f.id,
          'employee_id': f.employeeId,
          'severity': f.severity,
          'summary': f.summary,
          'resolved': f.resolved,
        };
      }).toList(),
      'total': findingsResult!.total,
    });
    return {
      'findings': result.findings.map((f) {
        return {
          'id': f.id,
          'employee_id': f.employeeId,
          'severity': f.severity,
          'summary': f.summary,
          'resolved': f.resolved,
        };
      }).toList(),
      'total': result.total,
    };
  }
}
