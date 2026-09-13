// The sidebar layout's leader/palette navigation must ask the same exit guard
// the top-tabs layout asks (audit F64).
//
// The palette's 'projects' item used to call `context.goToolBranches()` raw, so
// a pick replaced the open panel with no warning while the identical callback
// in layout 4 (top tabs) was guarded. Both now run the shared guarded helper,
// and a refusal must leave the route, the panel and its text untouched.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/home/sidebar_home_screen.dart';
import 'package:meept_ui/features/settings/discard_edits_dialog.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../mocks/mock_websocket_service.dart';

/// Offline stub: the sidebar reaches no daemon while it renders.
class _StubSidebarSdkClient extends SdkApiClient {
  _StubSidebarSdkClient() : super(host: 'localhost', port: 65441);

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async => [];

  @override
  Future<List<Map<String, dynamic>>> listProjects() async => [];
}

/// The sidebar at `/` plus the route the palette's projects pick opens, so a
/// navigation is observable.
GoRouter _sidebarRouter() => GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(path: '/', builder: (_, __) => const SidebarHomeScreen()),
    GoRoute(
      path: '/tools/branches',
      builder: (_, __) => const Scaffold(body: Text('branches route marker')),
    ),
  ],
);

/// Opens the command palette (ctrl+X, the default modifier) and picks [label].
Future<void> _pickPalette(WidgetTester tester, String label) async {
  await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
  await tester.sendKeyEvent(LogicalKeyboardKey.keyX);
  await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
  await tester.pumpAndSettle();
  await tester.tap(find.text(label));
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'api_port': 8081,
      'api_host': 'localhost',
    });
    await StorageService.instance.init();
  });

  /// Pumps the real sidebar on the router and returns its container.
  ///
  /// The container is disposed inside the test body (see the end of the test):
  /// the status bar watches a provider that holds a periodic timer, and
  /// flutter_test's pending-timer check runs before tear-downs.
  Future<ProviderContainer> pumpSidebar(WidgetTester tester) async {
    tester.view.physicalSize = const Size(1400, 1000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    final container = ProviderContainer(
      overrides: [
        sdkClientProvider.overrideWith((_) => _StubSidebarSdkClient()),
        websocketProvider.overrideWith((_) => MockWebSocketService()),
      ],
    );
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: _sidebarRouter()),
      ),
    );
    await tester.pumpAndSettle();
    return container;
  }

  Future<void> disposeSidebar(
    WidgetTester tester,
    ProviderContainer container,
  ) async {
    await tester.pumpWidget(const SizedBox());
    container.dispose();
  }

  testWidgets('the palette projects pick asks the guard before navigating', (
    tester,
  ) async {
    var asked = 0;
    final container = await pumpSidebar(tester);
    // Captured up front: the guard runs from a palette tap, and the element
    // lookup must not happen while a test API is in flight.
    final sidebarContext = tester.element(find.byType(SidebarHomeScreen));
    container.read(toolExitGuardProvider).register(() async {
      asked++;
      return showDiscardEditsDialog(
        sidebarContext,
        message:
            'you have unsaved changes in probe.json5. '
            'leaving the panel discards them.',
        confirmLabel: 'discard and exit',
      );
    });

    await _pickPalette(tester, 'projects');

    expect(asked, 1);
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(
      find.textContaining('unsaved changes in probe.json5'),
      findsOneWidget,
    );
    // Nothing was replaced while the panel is still deciding.
    expect(find.text('branches route marker'), findsNothing);

    // Cancel: the sidebar and the route stay exactly as they were.
    await tester.tap(find.text('cancel'));
    await tester.pumpAndSettle();

    expect(find.text('branches route marker'), findsNothing);
    expect(find.byType(SidebarHomeScreen), findsOneWidget);

    // Confirm: the same pick now goes through.
    await _pickPalette(tester, 'projects');
    expect(asked, 2);
    await tester.tap(find.text('discard and exit'));
    await tester.pumpAndSettle();

    expect(find.text('branches route marker'), findsOneWidget);

    await disposeSidebar(tester, container);
  });
}
