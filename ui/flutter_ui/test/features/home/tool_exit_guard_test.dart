// Verifies the shared unsaved-edits guard that every path out of a tool panel
// consults, other than the shared chrome's own back control and esc (those are
// covered by test/widgets/tool_panel_shell_test.dart):
//   - the hamburger menu's tool switch (openToolFromMenu, used by both home
//     layouts),
//   - a home tab switch (HomeScreen: tab bar, shortcut/palette and the
//     tabActivation requests children make),
//   - the registration lifetime that makes it work: a panel registers while
//     mounted, a gone panel cannot veto anything, and a newer panel's guard is
//     never cleared by an older one.
//
// The open panel is stubbed with a guard registered in the provider (and, for
// the lifetime cases, a small panel that registers and releases), rather than
// building the whole settings panel: the real settings wiring is covered by
// test/features/settings/main_config_editor_test.dart.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/home/home_screen.dart';
import 'package:meept_ui/features/home/tools_dropdown.dart';
import 'package:meept_ui/features/settings/discard_edits_dialog.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';
import 'package:meept_ui/widgets/tab_bar.dart';

/// Offline stub: HomeScreen reaches no daemon while it renders.
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listProjects() async => [];
}

/// Reports a live connection so no provider starts a poll timer.
class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}
  @override
  void disconnect() {}
  @override
  void send(Map<String, dynamic> message) {}

  @override
  bool get isConnected => true;
}

List<Override> get _stubs => [
  sdkClientProvider.overrideWith((_) => _StubSdkClient()),
  websocketProvider.overrideWith((_) => _StubWebSocket()),
];

/// Key of the text field that stands in for a panel's unsaved edits.
const Key menuTextKey = ValueKey('tool-exit-guard-menu-text');

/// Home-screen wiring for the menu path: the real hamburger menu and the real
/// [openToolFromMenu] helper, plus a text field that holds edits a switch
/// would drop.
class _MenuHost extends ConsumerStatefulWidget {
  const _MenuHost();

  @override
  ConsumerState<_MenuHost> createState() => _MenuHostState();
}

class _MenuHostState extends ConsumerState<_MenuHost> {
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        children: [
          const Text('chat panel marker'),
          HamburgerMenu(
            // Mirrors HomeScreen's menu wiring: the helper asks the open
            // panel's exit guard first, and only a route-less tool falls back
            // to the embedded chat-tab slot.
            onToolSelected: (tool) async {
              final outcome = await openToolFromMenu(context, ref, tool);
              if (outcome != ToolMenuOutcome.noRoute) return;
              ref.read(activeToolProvider.notifier).state = tool;
            },
          ),
          const TextField(key: menuTextKey),
        ],
      ),
    );
  }
}

/// A panel that registers an exit guard while it is mounted and releases it on
/// dispose, exactly like the settings panel does.
class _RegisteringPanel extends ConsumerStatefulWidget {
  const _RegisteringPanel({super.key, required this.guard});

  final ToolExitGuard guard;

  @override
  ConsumerState<_RegisteringPanel> createState() => _RegisteringPanelState();
}

class _RegisteringPanelState extends ConsumerState<_RegisteringPanel> {
  late final ToolExitGuardRegistry _registry = ref.read(toolExitGuardProvider);

  @override
  void initState() {
    super.initState();
    _registry.register(widget.guard);
  }

  @override
  void dispose() {
    _registry.release(widget.guard);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => const SizedBox();
}

/// Router for the menu harness: the panel on `/`, a tool route and a tab
/// route, matching how both home layouts open a menu pick.
GoRouter _menuRouter() => GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(path: '/', builder: (_, __) => const _MenuHost()),
    GoRoute(
      path: '/tools/memory',
      builder: (_, __) => const Scaffold(body: Text('memory route marker')),
    ),
  ],
);

/// Router for the HomeScreen harness. `/sessions` is a marker route so the
/// test can see the switch happen.
GoRouter _homeRouter() => GoRouter(
  initialLocation: '/',
  routes: [
    GoRoute(path: '/', builder: (_, __) => const HomeScreen()),
    GoRoute(
      path: '/sessions',
      builder: (_, __) => const Scaffold(body: Text('sessions route marker')),
    ),
  ],
);

/// Opens the real hamburger menu and picks [tool].
Future<void> _pickTool(WidgetTester tester, String tool) async {
  await tester.tap(find.byIcon(Icons.menu));
  await tester.pump();
  await tester.tap(find.text(tool));
  await tester.pumpAndSettle();
}

/// Taps the [label] tab on the home tab bar.
Future<void> _tapTab(WidgetTester tester, String label) async {
  await tester.tap(
    find.descendant(
      of: find.byType(OrangeVoidTabBar),
      matching: find.text(label),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _pumpMenuApp(
  WidgetTester tester,
  ProviderContainer container,
) async {
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: _menuRouter()),
    ),
  );
  await tester.pumpAndSettle();
}

/// Pumps the real HomeScreen on a router and returns its container.
///
/// The caller disposes the container with [_disposeHomeApp] inside the test
/// body: the home screen watches a provider that holds a periodic timer, and
/// flutter_test's pending-timer check runs before tear-downs.
Future<ProviderContainer> _pumpHomeApp(WidgetTester tester) async {
  final container = ProviderContainer(overrides: _stubs);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: _homeRouter()),
    ),
  );
  await tester.pumpAndSettle();
  return container;
}

/// Unmounts the home harness and disposes its container, cancelling the
/// provider-held timers before flutter_test checks for pending ones.
Future<void> _disposeHomeApp(
  WidgetTester tester,
  ProviderContainer container,
) async {
  await tester.pumpWidget(const SizedBox());
  container.dispose();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ToolExitGuardRegistry', () {
    test('nothing registered allows an exit and changes nothing', () async {
      final registry = ToolExitGuardRegistry();

      expect(registry.guard, isNull);
      expect(await registry.requestExit(), isTrue);
      expect(registry.guard, isNull);
    });

    test('a refusal keeps the registration, an allow releases it', () async {
      final registry = ToolExitGuardRegistry();
      var allow = false;
      registry.register(() async => allow);

      expect(await registry.requestExit(), isFalse);
      expect(registry.guard, isNotNull);

      allow = true;
      expect(await registry.requestExit(), isTrue);
      expect(registry.guard, isNull);
    });

    test('a disposed panel cannot clear a newer registration', () {
      final registry = ToolExitGuardRegistry();
      Future<bool> older() async => true;
      Future<bool> newer() async => true;

      registry.register(older);
      registry.register(newer);

      // The older panel disposes after the newer one registered.
      registry.release(older);
      expect(registry.guard, same(newer));

      registry.release(newer);
      expect(registry.guard, isNull);
    });
  });

  group('hamburger menu tool switch', () {
    testWidgets('a pick with nothing unsaved switches with no dialog', (
      tester,
    ) async {
      final container = ProviderContainer(overrides: _stubs);
      addTearDown(container.dispose);
      await _pumpMenuApp(tester, container);

      // No panel registered a guard: nothing to lose.
      expect(container.read(toolExitGuardProvider).guard, isNull);

      await _pickTool(tester, 'memory');

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('memory route marker'), findsOneWidget);
    });

    testWidgets(
      'a pick with unsaved edits warns; cancel keeps the panel and its text, '
      'confirm switches',
      (tester) async {
        final container = ProviderContainer(overrides: _stubs);
        addTearDown(container.dispose);
        var asked = 0;
        container.read(toolExitGuardProvider).register(() async {
          asked++;
          return showDiscardEditsDialog(
            tester.element(find.byKey(menuTextKey)),
            message:
                'you have unsaved changes in probe.json5. '
                'leaving the panel discards them.',
            confirmLabel: 'discard and exit',
          );
        });
        await _pumpMenuApp(tester, container);

        await tester.enterText(find.byKey(menuTextKey), 'unsaved probe text');
        await tester.pump();

        await _pickTool(tester, 'memory');

        expect(asked, 1);
        expect(find.byType(AlertDialog), findsOneWidget);
        expect(
          find.textContaining('unsaved changes in probe.json5'),
          findsOneWidget,
        );
        // Nothing was replaced while the panel is still deciding.
        expect(find.text('memory route marker'), findsNothing);

        // Cancel: the panel and the typed text are exactly as they were.
        await tester.tap(find.text('cancel'));
        await tester.pumpAndSettle();

        expect(find.byType(AlertDialog), findsNothing);
        expect(find.text('chat panel marker'), findsOneWidget);
        expect(find.text('memory route marker'), findsNothing);
        expect(
          tester
              .widget<EditableText>(
                find.descendant(
                  of: find.byKey(menuTextKey),
                  matching: find.byType(EditableText),
                ),
              )
              .controller
              .text,
          'unsaved probe text',
        );

        // Confirm: the same pick now goes through.
        await _pickTool(tester, 'memory');
        expect(asked, 2);
        await tester.tap(find.text('discard and exit'));
        await tester.pumpAndSettle();

        expect(find.text('memory route marker'), findsOneWidget);
        expect(find.byKey(menuTextKey), findsNothing);
      },
    );
  });

  group('home tab switch', () {
    testWidgets('a switch with unsaved edits warns, and cancel keeps the tab', (
      tester,
    ) async {
      var asked = 0;
      final container = await _pumpHomeApp(tester);
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return showDiscardEditsDialog(
          tester.element(find.byType(HomeScreen)),
          message:
              'you have unsaved changes in probe.json5. '
              'leaving the panel discards them.',
          confirmLabel: 'discard and exit',
        );
      });

      await _tapTab(tester, 'sessions');

      expect(asked, 1);
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('sessions route marker'), findsNothing);
      expect(find.byType(HomeScreen), findsOneWidget);

      // Cancel: the tab, the route and the open panel all stay.
      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('sessions route marker'), findsNothing);
      expect(find.byType(HomeScreen), findsOneWidget);

      // Confirm: the same switch goes through.
      await _tapTab(tester, 'sessions');
      expect(asked, 2);
      await tester.tap(find.text('discard and exit'));
      await tester.pumpAndSettle();

      expect(find.text('sessions route marker'), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);

      await _disposeHomeApp(tester, container);
    });

    testWidgets('a switch with nothing unsaved does not warn', (tester) async {
      final container = await _pumpHomeApp(tester);

      await _tapTab(tester, 'sessions');

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('sessions route marker'), findsOneWidget);

      await _disposeHomeApp(tester, container);
    });
  });

  group('registration lifetime', () {
    testWidgets('a guard registered by a disposed panel is not consulted', (
      tester,
    ) async {
      final container = ProviderContainer(overrides: _stubs);
      addTearDown(container.dispose);
      var asked = 0;

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            home: _RegisteringPanel(
              guard: () async {
                asked++;
                return false;
              },
            ),
          ),
        ),
      );
      expect(container.read(toolExitGuardProvider).guard, isNotNull);

      // The panel goes away (route replaced): its registration goes with it.
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(home: SizedBox()),
        ),
      );
      expect(container.read(toolExitGuardProvider).guard, isNull);

      // A later menu pick is neither vetoed nor prompted by the stale guard.
      await _pumpMenuApp(tester, container);
      await _pickTool(tester, 'memory');

      expect(asked, 0);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('memory route marker'), findsOneWidget);
    });

    testWidgets('a disposed panel cannot clear a newer panel guard', (
      tester,
    ) async {
      final container = ProviderContainer(overrides: _stubs);
      addTearDown(container.dispose);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            home: _RegisteringPanel(
              key: const ValueKey('older panel'),
              guard: () async => false,
            ),
          ),
        ),
      );
      final older = container.read(toolExitGuardProvider).guard;
      expect(older, isNotNull);

      // A newer panel mounts while the older one is on its way out: the
      // newer registration wins and the disposal below must not clear it.
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            home: _RegisteringPanel(
              key: const ValueKey('newer panel'),
              guard: () async => true,
            ),
          ),
        ),
      );

      final newer = container.read(toolExitGuardProvider).guard;
      expect(newer, isNotNull);
      expect(identical(older, newer), isFalse);

      // The registered guard is the newer one, and it still answers.
      expect(await container.read(toolExitGuardProvider).requestExit(), isTrue);
    });
  });
}
