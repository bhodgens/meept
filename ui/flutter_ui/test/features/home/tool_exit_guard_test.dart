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

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
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

/// Router for the system-pop harness: the panel route carries the same
/// `onExit` veto the real panel routes do (core/router.dart), plus a second
/// tool route so an app-driven navigation has somewhere else to go.
GoRouter _systemBackRouter() => GoRouter(
  initialLocation: '/settings',
  routes: [
    GoRoute(
      path: '/',
      builder: (_, __) => const Scaffold(body: Text('chat home marker')),
    ),
    GoRoute(
      path: '/settings',
      builder: (_, __) => const Scaffold(body: Text('settings panel marker')),
      onExit: guardRouteExit,
    ),
    GoRoute(
      path: '/tools/memory',
      builder: (_, __) => const Scaffold(body: Text('memory route marker')),
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

/// Awaits an exit request, but gives up after a moment.
///
/// A request that nothing completes (a guard that never answers, a latch that
/// was never settled) would otherwise hang the test instead of failing it;
/// this turns that hang into a null the assertion can name.
Future<bool?> _answerOrNull(Future<bool> request) => request
    .then<bool?>((allowed) => allowed)
    .timeout(const Duration(seconds: 1), onTimeout: () => null);

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

    // Two exits can be asked for at once - a second browser pop while the
    // discard dialog is up, a menu pick during the shared back control's
    // request - and a second dialog stacked on the first is what
    // ToolPanelShell._exitPending exists to prevent elsewhere. The registry
    // answers the second request with the first one's answer instead of
    // asking the guard again.
    test('a request while one is in flight shares its answer', () async {
      final registry = ToolExitGuardRegistry();
      var asked = 0;
      final gate = Completer<bool>();
      registry.register(() async {
        asked++;
        return gate.future;
      });

      final first = registry.requestExit();
      final second = registry.requestExit();

      expect(asked, 1, reason: 'one request in flight is one ask');

      gate.complete(true);
      expect(await first, isTrue);
      expect(await second, isTrue);
      // The allow still released the registration, and the latch cleared: the
      // next request is a fresh one.
      expect(registry.guard, isNull);

      var askedAgain = 0;
      registry.register(() async {
        askedAgain++;
        return true;
      });
      expect(await registry.requestExit(), isTrue);
      expect(askedAgain, 1);
    });

    // A guard that answers without awaiting anything completes synchronously.
    // If the latch were installed after asking, it would keep that already
    // finished answer and every later request would share it: the panel would
    // never be asked again, and its edits would go unguarded.
    test('a request answered without awaiting does not latch', () async {
      final registry = ToolExitGuardRegistry();

      // Nothing registered: the first request answers immediately.
      expect(await registry.requestExit(), isTrue);

      var asked = 0;
      registry.register(() async {
        asked++;
        return false;
      });
      expect(await registry.requestExit(), isFalse);
      expect(
        asked,
        1,
        reason: 'the newly registered guard must still be asked',
      );
    });

    // A guard the panel registered can go away while its own answer is still
    // pending - the panel disposes on a route change, or the dialog is torn
    // down with the navigator it was shown in, and nothing ever completes that
    // future. Left latched, the never-completing future is handed to every
    // later exit: each one is silently unasked, and the panel that could have
    // vetoed it is already gone.
    test('a guard released while its answer is pending does not wedge '
        'later exits', () async {
      final registry = ToolExitGuardRegistry();
      final stuck = Completer<bool>();
      Future<bool> guard() => stuck.future;
      registry.register(guard);

      final first = registry.requestExit();
      expect(registry.guard, isNotNull);

      // The panel goes away (route change / dispose) with its answer owed.
      registry.release(guard);

      // The caller that was waiting is settled instead of hanging forever.
      expect(
        await _answerOrNull(first),
        isTrue,
        reason: 'the panel that owed the answer is gone; nothing is lost',
      );
      expect(registry.guard, isNull);

      // And the next exit is a fresh request: the guard that replaced it is
      // asked, rather than sharing a future that never completes.
      var asked = 0;
      registry.register(() async {
        asked++;
        return false;
      });
      expect(await _answerOrNull(registry.requestExit()), isFalse);
      expect(asked, 1, reason: 'the new guard must be asked');
    });

    test('releasing another guard leaves an in-flight request alone', () async {
      final registry = ToolExitGuardRegistry();
      var asked = 0;
      final gate = Completer<bool>();
      Future<bool> guard() async {
        asked++;
        return gate.future;
      }

      registry.register(guard);
      final pending = registry.requestExit();

      var decided = false;
      unawaited(pending.then((_) => decided = true));

      // A panel that never owned this request disposes: its release must not
      // answer a question that is still being asked. The request must still
      // be UNDECIDED afterwards - a real answer is still owed - so the guard
      // is asked once and its answer is what settles the request.
      registry.release(() async => true);
      expect(registry.guard, same(guard));

      await Future<void>.delayed(Duration.zero);
      expect(
        decided,
        isFalse,
        reason: 'a foreign release must not settle a request it does not own',
      );

      // The owed guard answers "no": that refusal is the request's answer,
      // which a foreign release answering "yes" would have masked.
      gate.complete(false);
      expect(await _answerOrNull(pending), isFalse);
      expect(asked, 1);
    });

    // A newer panel can register on top while an older panel's request is
    // still undecided (the older guard's discard dialog is open, then a
    // superseding panel opens). The older panel then disposes. The
    // superseding guard owns the screen now, so the owed request must NOT be
    // answered "allowed": that released a window close (or a browser pop)
    // past the guard that would have refused it, with the newer panel still
    // mounted and never asked. Refusing is the safe answer - the newer guard
    // is left registered, and a fresh request asks it.
    test('a release while a newer guard owns the screen refuses, never '
        'allows', () async {
      final registry = ToolExitGuardRegistry();
      final gate = Completer<bool>();
      Future<bool> older() => gate.future;
      registry.register(older);

      final pending = registry.requestExit();

      // A superseding panel registers on top while the first request is owed.
      var newerAsked = 0;
      registry.register(() async {
        newerAsked++;
        return false;
      });

      // The older panel goes away.
      registry.release(older);

      expect(
        await _answerOrNull(pending),
        isFalse,
        reason:
            'a superseding guard owns the screen; the exit must be '
            'refused, not waved through unasked',
      );

      // The newer guard still owns the screen, and a fresh request asks it.
      expect(registry.guard, isNotNull);
      expect(await _answerOrNull(registry.requestExit()), isFalse);
      expect(newerAsked, 1);
    });

    // The contract for a request whose guard goes away with its answer owed
    // and nothing left on screen: it settles ALLOWED once, and that is the
    // final answer. An answer arriving from the released guard afterwards
    // loses. Pinned so the choice is explicit - a late refusal is discarded
    // by contract (the request is already closed), never silently ignored
    // while some other behaviour is assumed.
    test(
      'a released request settles allowed once; a late refusal loses',
      () async {
        final registry = ToolExitGuardRegistry();
        final gate = Completer<bool>();
        Future<bool> released() => gate.future;
        registry.register(released);

        final request = registry.requestExit();
        registry.release(released);
        expect(await _answerOrNull(request), isTrue);

        // The dialog the released guard had open is answered later (cancel).
        gate.complete(false);
        await Future<void>.delayed(Duration.zero);

        expect(
          await _answerOrNull(request),
          isTrue,
          reason: 'the settled answer is final; the request did not reopen',
        );
        expect(registry.guard, isNull);
      },
    );

    // A request that has already been settled must not be re-answered by the
    // released guard's late answer: the latch it would complete now belongs
    // to a DIFFERENT, newer request. Without the identity check on the
    // completer, the old refusal settles the new request (a new guard's ask
    // #2 answered by ask #1's late "false"), so a fresh panel is never asked.
    test(
      'a late answer from a released request cannot answer a newer one',
      () async {
        final registry = ToolExitGuardRegistry();
        final firstGate = Completer<bool>();
        Future<bool> released() => firstGate.future;
        registry.register(released);

        final first = registry.requestExit();
        registry.release(released);
        expect(await _answerOrNull(first), isTrue);

        // A new guard registers and asks while request #1 is closing out.
        var asked = 0;
        final secondGate = Completer<bool>();
        registry.register(() async {
          asked++;
          return secondGate.future;
        });
        final second = registry.requestExit();
        expect(asked, 1);

        // Request #1's released guard finally answers (a refusal). It must not
        // reach request #2, which has its own guard to ask.
        firstGate.complete(false);
        var decided = false;
        unawaited(second.then((_) => decided = true));
        await Future<void>.delayed(Duration.zero);
        expect(
          decided,
          isFalse,
          reason: "ask #1's late answer must not settle ask #2",
        );

        secondGate.complete(true);
        expect(
          await _answerOrNull(second),
          isTrue,
          reason: "ask #2 carries its own guard's answer",
        );
      },
    );
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

  // The browser Back button and the OS back gesture never touch the shared
  // back control: they reach the router through RouterDelegate.popRoute, and
  // the panel routes veto there through GoRoute.onExit (F62).
  //
  // An ALLOWED onExit is not a navigation in this go_router version -
  // popRoute answers the platform "not handled" and a single-entry stack
  // stays where it is - so the guard has to leave the route itself. Before
  // that, a confirmed pop left the panel on screen with the registry already
  // released, and the next tool switch or window close dropped its edits with
  // no prompt at all.
  group('system back', () {
    // Pumps the panel route and returns the container plus its router.
    Future<(ProviderContainer, GoRouter)> pumpPanel(WidgetTester tester) async {
      final container = ProviderContainer(overrides: _stubs);
      addTearDown(container.dispose);
      final router = _systemBackRouter();
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(routerConfig: router),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('settings panel marker'), findsOneWidget);
      return (container, router);
    }

    testWidgets('an unsaved pop is vetoed, and an allowed pop leaves the '
        'route without the test navigating', (tester) async {
      final (container, router) = await pumpPanel(tester);
      // Captured up front: the guard below runs from the router's async pop,
      // where no test API may be called.
      final panelContext = tester.element(find.text('settings panel marker'));

      var asked = 0;
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return showDiscardEditsDialog(
          panelContext,
          message:
              'you have unsaved changes in probe.json5. '
              'leaving the panel discards them.',
          confirmLabel: 'discard and exit',
        );
      });

      final pending = router.routerDelegate.popRoute();
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.textContaining('unsaved changes in probe.json5'),
        findsOneWidget,
      );

      // Cancel: the pop is refused, so the route (and the panel behind it)
      // stay exactly where they are.
      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();
      expect(await pending, isTrue);
      expect(find.text('settings panel marker'), findsOneWidget);

      // Confirm: the veto lifts, the pop reports back to the platform as
      // unhandled, and the panel really leaves - the guard navigated, nothing
      // here did.
      final confirmed = router.routerDelegate.popRoute();
      await tester.pumpAndSettle();
      expect(asked, 2);
      await tester.tap(find.text('discard and exit'));
      await tester.pumpAndSettle();
      expect(await confirmed, isFalse);

      expect(find.byType(AlertDialog), findsNothing);
      expect(asked, 2, reason: 'leaving must not ask the guard a second time');
      expect(find.text('chat home marker'), findsOneWidget);
      expect(find.text('settings panel marker'), findsNothing);
    });

    testWidgets('an allowed pop with nothing unsaved leaves the route too', (
      tester,
    ) async {
      final (container, router) = await pumpPanel(tester);
      // No panel registered a guard: nothing to lose and nothing to ask.
      expect(container.read(toolExitGuardProvider).guard, isNull);

      expect(await router.routerDelegate.popRoute(), isFalse);
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
      expect(find.text('settings panel marker'), findsNothing);
    });

    // The no-guard case above short-circuits the registry: `_ask` returns
    // true without touching a guard, so it never exercises the interesting
    // half of the exit - a REGISTERED guard answering across the async gap
    // `guardRouteExit` awaits, and the `requested != applied` check deciding
    // whether the route still has to leave. This case is the one a panel with
    // unsaved edits actually takes.
    testWidgets('an allowed pop with a registered guard is asked once, '
        'releases the guard, and lands on chat', (tester) async {
      final (container, router) = await pumpPanel(tester);

      var asked = 0;
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return true;
      });

      expect(await router.routerDelegate.popRoute(), isFalse);
      await tester.pumpAndSettle();

      expect(asked, 1, reason: 'the one allowed pop asks the guard once');
      expect(
        container.read(toolExitGuardProvider).guard,
        isNull,
        reason: 'the allow released the registration',
      );
      expect(find.text('chat home marker'), findsOneWidget);
      expect(find.text('settings panel marker'), findsNothing);
    });

    testWidgets('an app-driven navigation off the panel route is left alone', (
      tester,
    ) async {
      // A menu pick or a tab switch navigates for itself; the veto runs
      // inside that navigation and must not redirect it to the chat home.
      final (container, router) = await pumpPanel(tester);
      expect(container.read(toolExitGuardProvider).guard, isNull);

      router.go('/tools/memory');
      await tester.pumpAndSettle();

      expect(find.text('memory route marker'), findsOneWidget);
      expect(find.text('chat home marker'), findsNothing);
    });

    // The same app-driven navigation, this time with a guard registered: the
    // route's `onExit` still runs (and still asks), so the `requested !=
    // applied` check - not the empty registry - is what has to leave the
    // navigation the user chose in place.
    testWidgets('an app-driven navigation with a registered guard is not '
        'redirected to chat', (tester) async {
      final (container, router) = await pumpPanel(tester);

      var asked = 0;
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return true;
      });

      router.go('/tools/memory');
      await tester.pumpAndSettle();

      expect(asked, 1, reason: 'the veto ran inside the navigation');
      expect(find.text('memory route marker'), findsOneWidget);
      expect(find.text('chat home marker'), findsNothing);
      expect(
        container.read(toolExitGuardProvider).guard,
        isNull,
        reason: 'the allow released the registration either way',
      );
    });
  });

  // A refused tab switch has to stop the caller's own side effects too: the
  // palette's 'new session' used to arm a create request that survived the
  // refusal and fired the next time the list mounted (F67).
  group('a refused switch stops the caller side effects', () {
    testWidgets('palette new-session arms its request only when allowed', (
      tester,
    ) async {
      var asked = 0;
      final container = await _pumpHomeApp(tester);
      // Captured up front: the guard runs from a palette tap, and the element
      // lookup must not happen while a test API is in flight.
      final homeContext = tester.element(find.byType(HomeScreen));
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return showDiscardEditsDialog(
          homeContext,
          message:
              'you have unsaved changes in probe.json5. '
              'leaving the panel discards them.',
          confirmLabel: 'discard and exit',
        );
      });

      // Nothing is armed before the pick.
      expect(container.read(createSessionRequestProvider), isFalse);

      await _pickPalette(tester, 'new session');

      expect(asked, 1);
      expect(
        find.textContaining('unsaved changes in probe.json5'),
        findsOneWidget,
      );
      // The refusal left the request unarmed and the tab where it was.
      expect(container.read(createSessionRequestProvider), isFalse);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.text('sessions route marker'), findsNothing);

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(container.read(createSessionRequestProvider), isFalse);
      expect(find.byType(HomeScreen), findsOneWidget);

      // The same pick, now confirmed, switches and arms the request.
      await _pickPalette(tester, 'new session');
      expect(asked, 2);
      await tester.tap(find.text('discard and exit'));
      await tester.pumpAndSettle();

      expect(container.read(createSessionRequestProvider), isTrue);
      expect(find.text('sessions route marker'), findsOneWidget);

      await _disposeHomeApp(tester, container);
    });
  });
}
