// Verifies the shared tool-panel chrome (ToolPanelShell / exitToolPanel):
// every panel gets one identical back control and one esc handler, both
// of which leave the panel and land back on chat.
//
// Also pins the esc contract that keeps the other esc consumers working:
// key events reach the primary focus first, so an inner handler (the find
// bar's FocusNode, a modal's own focus scope) wins and the shell must not
// fire its own exit.
//
// Also pins the exit-guard contract: a panel can veto both affordances
// (the settings panel uses it to protect unsaved config edits), and a
// refusal leaves the panel and its state mounted.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/chat/find_bar.dart';
import 'package:meept_ui/features/chat/find_state.dart';
import 'package:meept_ui/features/settings/discard_edits_dialog.dart';
import 'package:meept_ui/widgets/command_palette.dart';
import 'package:meept_ui/widgets/tool_panel_shell.dart';

/// Router with the panel on `/tools/probe` and a marker on `/`, so a test
/// can see which one is mounted.
GoRouter _router(Widget panel) => GoRouter(
  initialLocation: '/tools/probe',
  routes: [
    GoRoute(
      path: '/',
      builder: (_, __) => const Scaffold(body: Text('chat home marker')),
    ),
    GoRoute(
      path: '/tools/probe',
      builder: (_, __) => Scaffold(body: panel),
    ),
  ],
);

Widget _app(Widget panel) =>
    ProviderScope(child: MaterialApp.router(routerConfig: _router(panel)));

/// Pumps the panel, waits for the router transition, and confirms we start
/// on the panel route.
Future<void> _pumpPanel(WidgetTester tester, Widget panel) async {
  await tester.pumpWidget(_app(panel));
  await tester.pumpAndSettle();
  expect(find.text('chat home marker'), findsNothing);
}

void main() {
  testWidgets('renders one back control with the shared esc tooltip', (
    tester,
  ) async {
    await _pumpPanel(
      tester,
      const ToolPanelShell(title: 'Probe Panel', child: SizedBox()),
    );

    expect(find.byTooltip('back (esc)'), findsOneWidget);
    // UI text is lowercase (repo-wide GUI convention).
    expect(find.text('probe panel'), findsOneWidget);
    expect(find.text('Probe Panel'), findsNothing);
  });

  testWidgets('back control returns to chat', (tester) async {
    await _pumpPanel(
      tester,
      const ToolPanelShell(title: 'probe', child: SizedBox()),
    );

    await tester.tap(find.byTooltip('back (esc)'));
    await tester.pumpAndSettle();

    expect(find.text('chat home marker'), findsOneWidget);
  });

  testWidgets('esc returns to chat', (tester) async {
    await _pumpPanel(
      tester,
      const ToolPanelShell(title: 'probe', child: SizedBox()),
    );

    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();

    expect(find.text('chat home marker'), findsOneWidget);
  });

  testWidgets(
    'esc exits while a panel text field holds focus; typing does not',
    (tester) async {
      final controller = TextEditingController();
      addTearDown(controller.dispose);
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          child: TextField(controller: controller),
        ),
      );

      await tester.tap(find.byType(TextField));
      await tester.pump();
      await tester.enterText(find.byType(TextField), 'memory hint');
      await tester.pump();

      // Typing stays in the text field and does not exit the panel.
      expect(controller.text, 'memory hint');
      expect(find.text('chat home marker'), findsNothing);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
    },
  );

  testWidgets('the optional header slot renders under the shared header row', (
    tester,
  ) async {
    await _pumpPanel(
      tester,
      const ToolPanelShell(
        title: 'probe',
        header: Text('extra controls'),
        child: SizedBox(),
      ),
    );

    expect(find.text('extra controls'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pumpAndSettle();
    expect(find.text('chat home marker'), findsOneWidget);
  });

  group('exit guard', () {
    // A guard is the panel's veto over both exit affordances. It exists
    // because a PopScope cannot intercept a go_router exit: the full-screen
    // path pops with NavigatorState.pop (not maybePop) and the embedded path
    // replaces the route with `go`.

    testWidgets('back asks the guard, and a refusal keeps the panel open', (
      tester,
    ) async {
      var asked = 0;
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () async {
            asked++;
            return false;
          },
          child: const SizedBox(),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('chat home marker'), findsNothing);
      expect(find.text('probe'), findsOneWidget);
    });

    testWidgets('esc asks the guard, and a refusal keeps the panel open', (
      tester,
    ) async {
      var asked = 0;
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () async {
            asked++;
            return false;
          },
          child: const SizedBox(),
        ),
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      // The key event is still consumed (no fall-through to another esc
      // consumer), but the panel stays.
      expect(asked, 1);
      expect(find.text('chat home marker'), findsNothing);
      expect(find.text('probe'), findsOneWidget);
    });

    testWidgets('back exits when the guard allows it', (tester) async {
      var asked = 0;
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () async {
            asked++;
            return true;
          },
          child: const SizedBox(),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('esc exits when the guard allows it', (tester) async {
      var asked = 0;
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () async {
            asked++;
            return true;
          },
          child: const SizedBox(),
        ),
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('no guard exits both ways, exactly as before', (tester) async {
      await _pumpPanel(
        tester,
        const ToolPanelShell(title: 'probe', child: SizedBox()),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pumpAndSettle();
      expect(find.text('chat home marker'), findsOneWidget);

      await _pumpPanel(
        tester,
        const ToolPanelShell(title: 'probe', child: SizedBox()),
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('the panel stays until the guard answers', (tester) async {
      final answer = Completer<bool>();
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () => answer.future,
          child: const SizedBox(),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump();

      // The guard is still deciding: nothing has been torn down yet.
      expect(find.text('probe'), findsOneWidget);
      expect(find.text('chat home marker'), findsNothing);

      answer.complete(true);
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
    });
  });

  group('in-flight exit latch', () {
    // The guard is asynchronous (it usually opens a confirmation dialog), so
    // a repeated press while the first request is still pending used to ask
    // it again and stack a second dialog over the panel. One press, one
    // request.

    testWidgets('a second back press while the guard is deciding is ignored', (
      tester,
    ) async {
      var asked = 0;
      final answer = Completer<bool>();
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () {
            asked++;
            return answer.future;
          },
          child: const SizedBox(),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump();
      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump();

      expect(asked, 1);
      expect(find.text('probe'), findsOneWidget);

      answer.complete(true);
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('chat home marker'), findsOneWidget);
    });

    testWidgets('a second esc while the guard is deciding is ignored', (
      tester,
    ) async {
      var asked = 0;
      final answer = Completer<bool>();
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () {
            asked++;
            return answer.future;
          },
          child: const SizedBox(),
        ),
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();

      expect(asked, 1);
      expect(find.text('probe'), findsOneWidget);

      answer.complete(false);
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('probe'), findsOneWidget);
      expect(find.text('chat home marker'), findsNothing);
    });

    testWidgets('two rapid back presses stack one confirmation dialog', (
      tester,
    ) async {
      var asked = 0;
      late BuildContext panelContext;
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          // Slow guard: the confirmation opens a moment after the press, so
          // the second press arrives while the first request is still in
          // flight - the window where two dialogs used to stack.
          exitGuard: () async {
            asked++;
            await Future<void>.delayed(const Duration(milliseconds: 50));
            if (!panelContext.mounted) return false;
            return showDiscardEditsDialog(
              panelContext,
              message:
                  'you have unsaved changes in probe.json5. '
                  'leaving the panel discards them.',
              confirmLabel: 'discard and exit',
            );
          },
          child: Builder(
            builder: (context) {
              panelContext = context;
              return const SizedBox();
            },
          ),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump(const Duration(milliseconds: 10));
      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump(const Duration(milliseconds: 100));

      // One press, one request, one dialog.
      expect(asked, 1);
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        find.textContaining('unsaved changes in probe.json5'),
        findsOneWidget,
      );

      // Cancelling keeps the panel, and the latch is free for the next try.
      await tester.tap(find.text('cancel'));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('probe'), findsOneWidget);
      expect(find.text('chat home marker'), findsNothing);
    });

    testWidgets('a refusal releases the latch for the next press', (
      tester,
    ) async {
      var asked = 0;
      final answers = <Completer<bool>>[];
      await _pumpPanel(
        tester,
        ToolPanelShell(
          title: 'probe',
          exitGuard: () {
            asked++;
            final answer = Completer<bool>();
            answers.add(answer);
            return answer.future;
          },
          child: const SizedBox(),
        ),
      );

      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump();
      answers.last.complete(false);
      await tester.pumpAndSettle();

      expect(asked, 1);
      expect(find.text('probe'), findsOneWidget);

      // The refusal released the latch: a later press starts a new request
      // and the allowed answer leaves the panel.
      await tester.tap(find.byTooltip('back (esc)'));
      await tester.pump();
      expect(asked, 2);

      answers.last.complete(true);
      await tester.pumpAndSettle();

      expect(find.text('chat home marker'), findsOneWidget);
    });
  });

  group('esc priority for other esc consumers', () {
    testWidgets('the find bar closes instead of the panel exiting', (
      tester,
    ) async {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      const sessionId = 's-esc';
      container.read(findBarVisibleProvider(sessionId).notifier).state = true;

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
            routerConfig: _router(
              const ToolPanelShell(
                title: 'probe',
                child: FindBar(sessionId: sessionId, matchCount: 3),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      // Find bar handled it...
      expect(container.read(findBarVisibleProvider(sessionId)), isFalse);
      // ...and the panel is still open.
      expect(find.text('chat home marker'), findsNothing);
    });

    testWidgets('a modal above the panel handles esc and the panel stays', (
      tester,
    ) async {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      late BuildContext hostContext;
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
            routerConfig: _router(
              ToolPanelShell(
                title: 'probe',
                child: Builder(
                  builder: (context) {
                    hostContext = context;
                    return const SizedBox();
                  },
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Open the command palette on top of the open panel, exactly as the
      // home screens do, then dismiss it with esc.
      showDialog<void>(
        context: hostContext,
        builder: (_) => AlertDialog(
          backgroundColor: const Color(0xFF1A1A1A),
          title: const Text('command palette'),
          contentPadding: const EdgeInsets.symmetric(vertical: 8),
          content: SizedBox(
            width: 480,
            child: CommandPalette(
              items: CommandPalette.defaultItems,
              onSelected: (_) {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('command palette'), findsOneWidget);
      expect(find.text('chat'), findsOneWidget);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      // Palette dismissed, panel untouched.
      expect(find.text('command palette'), findsNothing);
      expect(find.text('chat'), findsNothing);
      expect(find.text('chat home marker'), findsNothing);
      expect(find.text('probe'), findsOneWidget);
    });
  });
}
