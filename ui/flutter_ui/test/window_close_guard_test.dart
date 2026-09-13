// Verifies the desktop window-close path asks the same exit guard every in-app
// exit asks (audit F62).
//
// Before this, the close listener saved window geometry and destroyed the
// window with no dirty check, so the native close button (Cmd+Q, the red
// button) unmounted the settings panel and its unsaved meept.json5 edits
// silently. The guard here is the settings panel's own: a refused close must
// leave the window - and therefore the panel and its edits - alone.
//
// Every window-lifecycle call is injected through WindowCloseActions, because
// window_manager and the geometry persistence both need a real platform
// window.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/settings/discard_edits_dialog.dart';
import 'package:meept_ui/main.dart';
import 'package:meept_ui/providers/tool_exit_guard.dart';
import 'package:meept_ui/services/storage_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

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
      return showDiscardEditsDialog(
        harness,
        message:
            'you have unsaved changes in meept.json5. '
            'leaving the panel discards them.',
        confirmLabel: 'discard and exit',
      );
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

    // Cancel: the window stays open and the latch is re-armed so the next
    // close is intercepted too.
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

    container
        .read(toolExitGuardProvider)
        .register(
          () async => showDiscardEditsDialog(
            harness,
            message:
                'you have unsaved changes in meept.json5. '
                'leaving the panel discards them.',
            confirmLabel: 'discard and exit',
          ),
        );

    final pending = handler.handleCloseRequest();
    await tester.pumpAndSettle();
    await tester.tap(find.text('discard and exit'));
    await tester.pumpAndSettle();
    await pending;

    expect(calls.order, ['saveGeometry', 'close']);
  });
}
