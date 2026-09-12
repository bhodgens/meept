// Verifies the shared tool-panel chrome (ToolPanelShell / exitToolPanel):
// every panel gets one identical back control and one esc handler, both
// of which leave the panel and land back on chat.
//
// Also pins the esc contract that keeps the other esc consumers working:
// key events reach the primary focus first, so an inner handler (the find
// bar's FocusNode, a modal's own focus scope) wins and the shell must not
// fire its own exit.

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:meept_ui/features/chat/find_bar.dart';
import 'package:meept_ui/features/chat/find_state.dart';
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
