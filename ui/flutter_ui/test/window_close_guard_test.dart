// Verifies the desktop window-close path asks the same exit guard every in-app
// exit asks (audit F62).
//
// Before this, the close listener saved window geometry and destroyed the
// window with no dirty check, so the window close button (the red traffic
// light) unmounted the settings panel and its unsaved meept.json5 edits
// silently. The guard here is the settings panel's own: a refused close must
// leave the window - and therefore the panel and its edits - alone.
//
// SCOPE (corrected): this path covers the window close button / the red
// traffic light, NOT Cmd+Q. window_manager 0.4.3 implements its veto as
// NSWindowDelegate.windowShouldClose, and `NSApp.terminate` (Cmd+Q, the Quit
// menu item) never calls it - the plugin implements no
// applicationShouldTerminate and neither does macos/Runner/AppDelegate.swift,
// so Cmd+Q still terminates with unsaved edits. An earlier version of this
// header claimed "Cmd+Q, the red button"; only the red button is true. See the
// known-gap note next to the guard's installation in main.dart.
//
// Two seams are exercised: a widget-level WindowCloseActions fake, which pins
// the order of the decision (ask, then save geometry, then close), and the
// REAL WindowCloseActions against a mocked 'window_manager' method channel,
// which pins the plugin calls the veto depends on. Still untested here (needs
// a desktop build and a real window): main()'s windowManager.addListener
// registration, and the native windowShouldClose round trip.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/discard_edits_dialog.dart';
import 'package:meept_ui/main.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

const MethodChannel _windowManagerChannel = MethodChannel('window_manager');

/// Records the window-lifecycle calls the handler makes, in order.
class _WindowCalls {
  final List<String> order = [];

  WindowCloseActions get actions => WindowCloseActions(
    saveGeometry: () async => order.add('saveGeometry'),
    keepOpen: () async => order.add('keepOpen'),
    close: () async => order.add('close'),
  );
}

/// Pumps a minimal app so [showDiscardEditsDialog] has a Navigator to use, and
/// returns a context inside it, captured before any guard is registered.
Future<BuildContext> _pumpHarness(WidgetTester tester) async {
  await tester.pumpWidget(
    const MaterialApp(home: Scaffold(body: Text('window harness'))),
  );
  await tester.pumpAndSettle();
  return tester.element(find.text('window harness'));
}

/// Answers the real plugin's channel and records what it was asked, so
/// [WindowCloseActions.real] can be driven without a platform window.
List<MethodCall> _mockWindowManagerChannel(WidgetTester tester) {
  final calls = <MethodCall>[];
  final messenger = tester.binding.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(_windowManagerChannel, (call) async {
    calls.add(call);
    switch (call.method) {
      case 'isMaximized':
        return false;
      case 'getBounds':
        return <String, dynamic>{
          'x': 0.0,
          'y': 0.0,
          'width': 800.0,
          'height': 600.0,
        };
      default:
        return null;
    }
  });
  addTearDown(
    () => messenger.setMockMethodCallHandler(_windowManagerChannel, null),
  );
  return calls;
}

/// The guard the settings panel registers: opens the shared discard dialog.
ToolExitGuard _dialogGuard(
  BuildContext harness, {
  String file = 'meept.json5',
}) {
  return () async => showDiscardEditsDialog(
    harness,
    message:
        'you have unsaved changes in $file. '
        'leaving the panel discards them.',
    confirmLabel: 'discard and exit',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    await StorageService.instance.init();
  });

  testWidgets('a close with no registered guard saves geometry and closes', (
    tester,
  ) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final calls = _WindowCalls();
    final handler = WindowCloseHandler(container, actions: calls.actions);

    // Nothing unsaved: the guard registry is empty, so the close goes
    // straight through with no dialog.
    expect(container.read(toolExitGuardProvider).guard, isNull);

    await handler.handleCloseRequest();

    expect(calls.order, ['saveGeometry', 'close']);
  });

  testWidgets('a refused close keeps the window, the panel and its edits', (
    tester,
  ) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final calls = _WindowCalls();
    final handler = WindowCloseHandler(container, actions: calls.actions);
    final harness = await _pumpHarness(tester);

    var asked = 0;
    container.read(toolExitGuardProvider).register(() async {
      asked++;
      return _dialogGuard(harness)();
    });

    final pending = handler.handleCloseRequest();
    await tester.pumpAndSettle();

    expect(asked, 1);
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(
      find.textContaining('unsaved changes in meept.json5'),
      findsOneWidget,
    );
    // Nothing happened while the dialog is open.
    expect(calls.order, isEmpty);

    // Cancel: the window stays open.
    await tester.tap(find.text('cancel'));
    await tester.pumpAndSettle();
    await pending;

    expect(calls.order, ['keepOpen']);
  });

  testWidgets('confirming the loss saves geometry and closes the window', (
    tester,
  ) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final calls = _WindowCalls();
    final handler = WindowCloseHandler(container, actions: calls.actions);
    final harness = await _pumpHarness(tester);

    container.read(toolExitGuardProvider).register(_dialogGuard(harness));

    final pending = handler.handleCloseRequest();
    await tester.pumpAndSettle();
    await tester.tap(find.text('discard and exit'));
    await tester.pumpAndSettle();
    await pending;

    expect(calls.order, ['saveGeometry', 'close']);
  });

  // The listener API hands the handler no future, so a second close request
  // arriving before the first answer (a double click on the close button, a
  // close while the dialog is up) used to ask the guard again and stack a
  // second dialog. The handler now latches, like ToolPanelShell._exitPending.
  testWidgets(
    'a second close request while the first is undecided is ignored',
    (tester) async {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final calls = _WindowCalls();
      final handler = WindowCloseHandler(container, actions: calls.actions);
      final harness = await _pumpHarness(tester);

      var asked = 0;
      container.read(toolExitGuardProvider).register(() async {
        asked++;
        return _dialogGuard(harness)();
      });

      // Two rapid requests: the listener's own entry point for both, so this is
      // the path a double click actually takes.
      handler.onWindowClose();
      handler.onWindowClose();
      await tester.pumpAndSettle();

      expect(asked, 1, reason: 'one request in flight is one ask');
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(calls.order, isEmpty);

      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      // One decision, one keepOpen: the ignored request changed nothing.
      expect(calls.order, ['keepOpen']);
    },
  );

  testWidgets('the listener entry point drives the same close path', (
    tester,
  ) async {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final calls = _WindowCalls();
    final handler = WindowCloseHandler(container, actions: calls.actions);
    final harness = await _pumpHarness(tester);

    container.read(toolExitGuardProvider).register(_dialogGuard(harness));

    // What window_manager calls on the close event: onWindowClose, not the
    // awaitable half the other tests use.
    handler.onWindowClose();
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);

    await tester.tap(find.text('discard and exit'));
    await tester.pumpAndSettle();

    expect(calls.order, ['saveGeometry', 'close']);
  });

  // The veto is only as good as the plugin calls it makes: `keepOpen` has to
  // leave the prevent-close latch set and `close` has to clear it before
  // destroying the window. Both go through window_manager's method channel, so
  // the real WindowCloseActions are exercised against a mocked one.
  testWidgets('the real actions make the plugin calls the veto depends on', (
    tester,
  ) async {
    final calls = _mockWindowManagerChannel(tester);

    await WindowCloseActions.real.keepOpen();
    expect(calls.map((call) => call.method), ['setPreventClose']);
    expect(calls.single.arguments, {'isPreventClose': true});

    calls.clear();
    await WindowCloseActions.real.saveGeometry();
    expect(
      calls.map((call) => call.method),
      containsAllInOrder(['isMaximized', 'getBounds']),
    );

    calls.clear();
    await WindowCloseActions.real.close();
    expect(calls.map((call) => call.method), ['setPreventClose', 'destroy']);
    expect(calls.first.arguments, {'isPreventClose': false});
  });

  testWidgets(
    'a vetoed close through the real actions never destroys the window',
    (tester) async {
      final calls = _mockWindowManagerChannel(tester);
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final handler = WindowCloseHandler(container);
      final harness = await _pumpHarness(tester);

      container.read(toolExitGuardProvider).register(_dialogGuard(harness));

      final pending = handler.handleCloseRequest();
      await tester.pumpAndSettle();
      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();
      await pending;

      expect(calls.map((call) => call.method), ['setPreventClose']);
      expect(calls.single.arguments, {'isPreventClose': true});
      expect(calls.map((call) => call.method), isNot(contains('destroy')));
    },
  );
}
