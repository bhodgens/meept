import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/orchestrator_config_editor.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub client returning a fixed orchestrator config; records saves.
class _StubConfigClient extends SdkApiClient {
  Map<String, dynamic> config = {
    'max_plan_steps': 25,
    'max_research_steps': 15,
    'planner_timeout': 120,
    'token_budget_alert': 150000,
    'max_handoff_steps': 10,
    'handoff_use_amendment': true,
    'ambiguity_threshold': 0.65,
    'interview_ambiguity_threshold': 0.8,
    'max_steps_per_phase': 20,
    'max_phases': 6,
  };
  Map<String, dynamic>? lastSaved;

  _StubConfigClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getOrchestratorConfig() async => config;

  @override
  Future<Map<String, dynamic>> saveOrchestratorConfig(
    Map<String, dynamic> oc,
  ) async {
    lastSaved = oc;
    return oc;
  }
}

Future<void> _pump(WidgetTester tester, _StubConfigClient client) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [sdkClientProvider.overrideWithValue(client)],
      child: const MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(child: OrchestratorConfigEditor()),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('loads and renders all orchestrator fields', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    expect(find.text('orchestrator'), findsOneWidget);
    expect(find.text('max plan steps'), findsOneWidget);
    expect(find.text('handoff uses amendment'), findsOneWidget);
    // Seeded value appears in its text field.
    expect(find.widgetWithText(TextField, '25'), findsOneWidget);
    expect(find.byType(SwitchListTile), findsOneWidget);
  });

  testWidgets('edit + save PUTs the full typed block', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    // Change max_plan_steps from 25 to 40.
    final field = find.widgetWithText(TextField, '25');
    await tester.enterText(field, '40');
    await tester.pump(); // rebuild so the save button appears

    // Save button appears after the change.
    await tester.tap(find.byType(ElevatedButton));
    await tester.pumpAndSettle();

    expect(client.lastSaved, isNotNull);
    expect(client.lastSaved!['max_plan_steps'], 40);
    // Untouched fields are preserved verbatim.
    expect(client.lastSaved!['ambiguity_threshold'], 0.65);
    expect(client.lastSaved!['handoff_use_amendment'], true);
  });

  testWidgets('toggle bool field and save', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    await tester.tap(find.byType(SwitchListTile));
    await tester.pump();
    await tester.tap(find.text('save'));
    await tester.pumpAndSettle();

    expect(client.lastSaved?['handoff_use_amendment'], false);
  });

  testWidgets('invalid number blocks save with an error', (tester) async {
    final client = _StubConfigClient();
    await _pump(tester, client);

    await tester.enterText(
      find.widgetWithText(TextField, '25'),
      'not-a-number',
    );
    await tester.pump();
    await tester.tap(find.byType(ElevatedButton));
    await tester.pumpAndSettle();

    expect(client.lastSaved, isNull);
    expect(find.textContaining('invalid numeric'), findsOneWidget);
  });
}
