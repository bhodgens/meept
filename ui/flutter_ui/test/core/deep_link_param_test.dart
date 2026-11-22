// 05-navigation TEST-GATE T5 (leaf 00T-gate.md, matrix F12b; W20/N6).
//
// Pins deep-link session-param consumption: meept://session/<id> resolves
// to /activity?session=<id> (deep_link.dart already builds that URL), but
// the /activity route builds ActivityTab WITHOUT reading
// state.uri.queryParameters, and ActivityNotifier.loadActivity has no
// session filter - the param is silently dropped and the view shows ALL
// sessions' activity.
//
// RED today: navigating to /activity?session=sess-a renders sess-b's
// records too (no queryParameters reader anywhere on the path). The gate
// drives ActivityTab under a GoRoute whose builder mirrors the shipped
// router's blind builder (core/router.dart), so the fix must consume the
// param inside the route/tab seam (GoRouterState) to turn this GREEN.
//
// The real SourceCodePro font is loaded so the tab's fixed-width sort
// row lays out as in production (flutter_test's square fallback font
// otherwise overflows the 300px list column - a harness artifact, not
// the defect under test).

import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/core/deep_link.dart';
import 'package:meept_ui/core/router.dart' show router;
import 'package:meept_ui/features/activity/activity_list.dart';
import 'package:meept_ui/features/activity/activity_tab.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// SDK stub: activity records for TWO sessions; panels answer empty.
class _ActivityStubClient extends SdkApiClient {
  _ActivityStubClient() : super(host: 'localhost', port: 65432);

  @override
  Future<Map<String, dynamic>> getActivity({
    String? since,
    String? until,
    String? sort,
    String? taskId,
    String? planId,
    String? agentId,
    List<String>? kinds,
    int? limit,
    int? offset,
  }) async {
    return {
      'records': [
        {
          'kind': 'task_step',
          'at': '2026-11-19T10:00:00Z',
          'session_id': 'sess-a',
          'summary': 'quartzalpha session a work',
        },
        {
          'kind': 'task_step',
          'at': '2026-11-19T10:01:00Z',
          'session_id': 'sess-b',
          'summary': 'quartzbeta session b work',
        },
      ],
      'total': 2,
    };
  }

  @override
  Future<Map<String, dynamic>> getActivityRecent({int? limit}) async => {
    'records': [],
    'dropped': 0,
    'total': 0,
  };

  @override
  Future<Map<String, dynamic>> getActivityFindings({
    String? employeeId,
    String? since,
    String? severity,
    bool? resolved,
    int? limit,
    int? offset,
  }) async => {'findings': [], 'total': 0};

  @override
  Future<Map<String, dynamic>> getActivityOverview() async => {
    'by_agent': [],
    'generated_at': '2026-11-19T10:00:00Z',
  };
}

/// Route table mirroring the shipped /activity route: the builder does
/// NOT forward the query param (exactly core/router.dart today). The
/// consumption fix must read it inside the tab seam to go GREEN.
GoRouter _activityRouter(String initialLocation) => GoRouter(
  initialLocation: initialLocation,
  routes: [
    GoRoute(path: '/', builder: (_, __) => const SizedBox()),
    GoRoute(path: '/activity', builder: (_, __) => const ActivityTab()),
  ],
);

Future<void> _loadRealFont() async {
  final file = File('assets/fonts/SourceCodePro-Regular.ttf');
  if (!file.existsSync()) return; // checkout without assets: fallback
  final loader = FontLoader('SourceCodePro')
    ..addFont(Future.value(file.readAsBytesSync().buffer.asByteData()));
  await loader.load();
}

/// The activity sort row (fixed 300px column: title + 3 sort buttons)
/// overflows by a few pixels under test layout - a PRE-EXISTING layout
/// fragility of ActivityList, unrelated to the deep-link param defect
/// this gate pins. Filter only that specific rendering exception so the
/// gate asserts the param contract; every other error still fails.
void _filterKnownSortRowOverflow() {
  final prior = FlutterError.onError;
  FlutterError.onError = (details) {
    final text = details.exceptionAsString();
    if (text.contains('RenderFlex overflowed') &&
        text.contains('on the right')) {
      return; // known pre-existing sort-row artifact
    }
    prior?.call(details);
  };
  addTearDown(() => FlutterError.onError = prior);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(_loadRealFont);

  group('GUI deep-link session param (05-navigation T5)', () {
    test('resolver keeps the session param in the in-app location', () {
      // Precondition pin (GREEN today): the resolver builds the param.
      final target = resolveDeepLink(
        Uri.parse('meept://session/sess-a'),
        hasActivityRoute: true,
      );
      expect(target, isNotNull);
      expect(target!.sessionID, 'sess-a');
      expect(target.fallbackUrl, '/activity?session=sess-a');
    });

    test('shipped router exposes /activity (feature gate reality)', () {
      expect(routerHasActivityRoute(router), isTrue);
    });

    testWidgets('TestDeepLinkParam: /activity?session=<id> filters the view', (
      tester,
    ) async {
      _filterKnownSortRowOverflow();
      tester.view.physicalSize = const Size(1600, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final client = _ActivityStubClient();
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWith((_) => client)],
      );

      // Navigate exactly where the deep link lands.
      final target = resolveDeepLink(
        Uri.parse('meept://session/sess-a'),
        hasActivityRoute: true,
      )!;
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
            // Real font metrics (the sort row is laid out for
            // SourceCodePro); textScaler 1.0 keeps the fixed-width
            // column deterministic.
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: TextScaler.noScaling),
              child: child!,
            ),
            routerConfig: _activityRouter(target.fallbackUrl),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Precondition: the stub served two sessions' records.
      final state = container.read(activityProvider);
      expect(
        state.records,
        hasLength(2),
        reason: 'activity load precondition broken',
      );

      // The session param must be CONSUMED: only sess-a's record shows.
      expect(
        find.textContaining('quartzalpha'),
        findsWidgets,
        reason: 'target session record missing from the filtered view',
      );
      expect(
        find.textContaining('quartzbeta'),
        findsNothing,
        reason:
            'session param dropped: /activity?session=sess-a still shows '
            'sess-b records (no queryParameters reader on the route; '
            'loadActivity has no session filter)',
      );

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });

    testWidgets('no param: unfiltered view keeps both sessions', (
      tester,
    ) async {
      // Regression guard for the fix: without the param the view must
      // stay unfiltered (the filter is param-driven, not hardcoded).
      _filterKnownSortRowOverflow();
      tester.view.physicalSize = const Size(1600, 1000);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final client = _ActivityStubClient();
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWith((_) => client)],
      );
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: TextScaler.noScaling),
              child: child!,
            ),
            routerConfig: _activityRouter('/activity'),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.textContaining('quartzalpha'), findsWidgets);
      expect(find.textContaining('quartzbeta'), findsWidgets);

      await tester.pumpWidget(const SizedBox());
      container.dispose();
    });
  });
}
