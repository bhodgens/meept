// E2E wire-shape proof for the metrics-panel grades section (leaf
// 03-flutter-panel of model-rating-metrics-plan, R9/F7).
//
// The REAL MetricsPanel + MetricsNotifier (no service mocks) run against the
// StubDaemon, whose GET /api/v1/metrics/reviews handler mirrors
// handleReviewsMetrics (internal/comm/http/reviews_endpoint.go, contract
// C5): the grades tables must render from daemon-shaped snake_case data
// without any client-side transformation. runAsync brackets the real HTTP
// fetches (the flutter_test fake clock does not advance them).
//
// DO NOT import from production code — test/e2e only.
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/features/metrics/metrics_panel.dart';
import 'package:meept_ui/providers/metrics_provider.dart';

import 'pump_app.dart';
import 'stub_daemon.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  useRealHttp();

  group('metrics panel grades section (C6 wire shape)', () {
    late StubDaemon daemon;

    setUp(() async {
      daemon = await StubDaemon.start();
    });

    tearDown(() async {
      await daemon.dispose();
    });

    testWidgets(
      'grades-by-model and grades-by-judge render from the stub daemon',
      (tester) async {
        late ProviderContainer container;
        await tester.runAsync(() async {
          container = pumpRealApp(
            tester,
            daemon: daemon,
            child: const Scaffold(body: MetricsPanel()),
          ).$1;
        });
        addTearDown(container.dispose);
        await tester.pump();

        // The panel renders once the live snapshot + both grades fetches land.
        bool gradesLoaded() =>
            container.read(metricsProvider).reviewGrades['model'] != null &&
            container.read(metricsProvider).reviewGrades['judge'] != null;
        await tester.runAsync(() async {
          final deadline = DateTime.now().add(const Duration(seconds: 10));
          while (DateTime.now().isBefore(deadline)) {
            if (gradesLoaded()) break;
            await Future<void>.delayed(const Duration(milliseconds: 25));
          }
        });
        expect(gradesLoaded(), isTrue);

        await tester.pump();
        await tester.pump(const Duration(milliseconds: 20));

        // Two lowercase-titled grades sections rendered from wire-shape data.
        expect(find.text('grades by model'), findsOneWidget);
        expect(find.text('grades by judge'), findsOneWidget);

        // By-model rows: daemon label when present, else the raw group key.
        expect(find.text('GLM X'), findsOneWidget);
        expect(find.text('local/llama'), findsOneWidget);
        // By-judge row (judge label, never 'reviewer').
        expect(find.text('code reviewer'), findsOneWidget);

        // Column headers mirror the C6 consumption shape, lowercase.
        expect(find.text('verdicts'), findsWidgets);
        expect(find.text('defects'), findsWidgets);
        expect(find.text('pass rate'), findsWidgets);

        // Live tiles still render alongside the new sections.
        expect(find.text('active agents'), findsOneWidget);
      },
    );
  });
}
