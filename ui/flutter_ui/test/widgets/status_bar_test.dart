import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/models/session.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/status_message_provider.dart';
import 'package:meept_ui/providers/acp_status_provider.dart';
import 'package:meept_ui/models/acp_status.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/widgets/status_bar.dart';

/// A [CurrentProjectNotifier] that skips the SDK call entirely and starts
/// from [CurrentProject.empty]. Tests don't need to call refresh(), so the
/// SDK client is never invoked. We pass a real [SdkApiClient] to satisfy
/// the constructor signature (it's only stored, never used in tests).
class _FakeProjectNotifier extends CurrentProjectNotifier {
  _FakeProjectNotifier() : super(SdkApiClient(host: 'localhost'));
}

Session _session({int? leafMessageId}) => Session(
  id: 's1',
  title: 'test session',
  createdAt: DateTime(2026, 1, 1),
  leafMessageId: leafMessageId,
);

/// Wraps the bar with a session-scoped provider override set: everything
/// network-touching is overridden to an already-resolved value so the test
/// stays hermetic.
Widget _wrapWithSession({
  required Session? session,
  int? pendingChanges,
  int? pendingClaims,
  String? branch,
}) {
  return ProviderScope(
    overrides: [
      verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
      currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
      acpStatusProvider.overrideWith((ref) async => AcpStatusView.disabled),
      activeSessionProvider.overrideWith((ref) => session),
      if (session != null) ...[
        sessionPendingChangesProvider(
          session.id,
        ).overrideWith((ref) => Future<int>.value(pendingChanges ?? 0)),
        memoryReviewPendingProvider.overrideWith(
          (ref) => Future<int>.value(pendingClaims ?? 0),
        ),
        sessionBranchProvider(
          session.id,
        ).overrideWith((ref) => Future<String?>.value(branch)),
      ],
    ],
    child: const MaterialApp(
      home: Scaffold(body: StatusBar(selectedTabIndex: 0)),
    ),
  );
}

/// Wraps the bar with no active session and a given tab (keybind-hint tests).
Widget _wrapNoSession({required int tab}) {
  return ProviderScope(
    overrides: [
      verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
      currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
      acpStatusProvider.overrideWith((ref) async => AcpStatusView.disabled),
      activeSessionProvider.overrideWith((ref) => null),
    ],
    child: MaterialApp(
      home: Scaffold(body: StatusBar(selectedTabIndex: tab)),
    ),
  );
}

/// Finds text inside RichText spans (find.textContaining only sees
/// plain [Text] widgets; StatusBar renders its parts as TextSpans).
Finder findSpanText(String needle) => find.byWidgetPredicate(
  (w) => w is RichText && w.text.toPlainText().contains(needle),
);

void main() {
  testWidgets('renders verbosity + connection', (tester) async {
    await tester.pumpWidget(_wrapNoSession(tab: 0));
    await tester.pump();
    expect(findSpanText('verbosity'), findsOneWidget);
  });

  testWidgets('renders transient status message when set, hides other parts', (
    tester,
  ) async {
    final container = ProviderContainer(
      overrides: [
        verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
        currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
        acpStatusProvider.overrideWith((ref) async => AcpStatusView.disabled),
      ],
    );
    addTearDown(container.dispose);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
          home: Scaffold(body: StatusBar(selectedTabIndex: 0)),
        ),
      ),
    );
    await tester.pump();
    container.read(statusMessageProvider.notifier).state = 'session archived';
    await tester.pump();
    expect(find.text('session archived'), findsOneWidget);
    expect(findSpanText('verbosity'), findsNothing);
  });

  testWidgets('keybind hint shows sessions-specific text on sessions tab', (
    tester,
  ) async {
    await tester.pumpWidget(_wrapNoSession(tab: 1));
    await tester.pump();
    // Two assertions guard against a session title like "archive-test"
    // leaking into the session-part and false-matching the keybind hint.
    expect(findSpanText('dbl-click'), findsOneWidget);
    expect(findSpanText('archive'), findsOneWidget);
  });

  testWidgets('keybind hint shows chat-specific text on chat tab', (
    tester,
  ) async {
    await tester.pumpWidget(_wrapNoSession(tab: 0));
    await tester.pump();
    expect(findSpanText('focus'), findsOneWidget);
    expect(findSpanText('verbosity'), findsOneWidget);
  });

  testWidgets('shows acp:N when live agents exist', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          verbosityProvider.overrideWith((ref) => VerbosityNotifier()),
          currentProjectProvider.overrideWith((ref) => _FakeProjectNotifier()),
          acpStatusProvider.overrideWith(
            (ref) async => AcpStatusView.fromJson({
              'enabled': true,
              'agents': [
                {
                  'id': 'codex',
                  'enabled': true,
                  'running': true,
                  'state': 'ready',
                  'uptime_s': 0,
                },
              ],
            }),
          ),
        ],
        child: const MaterialApp(
          home: Scaffold(body: StatusBar(selectedTabIndex: 0)),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('acp:1'), findsOneWidget);
  });

  testWidgets('shows branch span when the session has an active thread', (
    tester,
  ) async {
    await tester.pumpWidget(
      _wrapWithSession(
        session: _session(leafMessageId: 42),
        branch: 'Feature Work',
      ),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('branch: feature work'), findsOneWidget);
  });

  testWidgets('hides branch span when the provider resolves null', (
    tester,
  ) async {
    // NOTE: the leaf_message_id gate lives inside sessionBranchProvider;
    // overriding it here bypasses that gate, so this test only pins the
    // render-side rule: a null/empty branch label renders no span.
    await tester.pumpWidget(
      _wrapWithSession(session: _session(leafMessageId: 42), branch: null),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('branch:'), findsNothing);
  });

  testWidgets('shows pending changes count when > 0', (tester) async {
    await tester.pumpWidget(
      _wrapWithSession(session: _session(), pendingChanges: 3),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('3 pending changes'), findsOneWidget);
  });

  testWidgets('hides pending changes span when 0', (tester) async {
    await tester.pumpWidget(
      _wrapWithSession(session: _session(), pendingChanges: 0),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('pending changes'), findsNothing);
  });

  testWidgets('shows claims-to-review count when > 0', (tester) async {
    await tester.pumpWidget(
      _wrapWithSession(session: _session(), pendingClaims: 2),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('2 claims to review'), findsOneWidget);
  });

  testWidgets('hides claims-to-review span when 0', (tester) async {
    await tester.pumpWidget(
      _wrapWithSession(session: _session(), pendingClaims: 0),
    );
    await tester.pump();
    await tester.pump();
    expect(findSpanText('claims to review'), findsNothing);
  });

  testWidgets('hides all conditional spans without an active session', (
    tester,
  ) async {
    await tester.pumpWidget(_wrapNoSession(tab: 0));
    await tester.pump();
    expect(findSpanText('branch:'), findsNothing);
    expect(findSpanText('pending changes'), findsNothing);
    expect(findSpanText('claims to review'), findsNothing);
  });
}
