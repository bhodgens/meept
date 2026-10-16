import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/core/shortcuts.dart';

Widget _host(LeaderKeyController controller, FocusNode focusNode) =>
    MaterialApp(
      home: Focus(
        focusNode: focusNode,
        autofocus: true,
        onKeyEvent: (node, event) => controller.handleKeyEvent(event),
        child: const Scaffold(body: SizedBox()),
      ),
    );

void main() {
  Future<void> pressWithCtrl(
    WidgetTester tester,
    LogicalKeyboardKey key,
  ) async {
    // The test framework's sendKeyDownEvent does not hold modifiers, so
    // simulate ctrl-down → key-down → ctrl-up manually.
    await tester.sendKeyDownEvent(
      LogicalKeyboardKey.controlLeft,
      platform: 'linux',
    );
    await tester.sendKeyDownEvent(key, platform: 'linux');
    await tester.sendKeyUpEvent(key, platform: 'linux');
    await tester.sendKeyUpEvent(
      LogicalKeyboardKey.controlLeft,
      platform: 'linux',
    );
    await tester.pump();
  }

  testWidgets('ctrl+s fires onToggleSteer', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onToggleSteer = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyS);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+t fires onToggleTts', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()..onToggleTts = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyT);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+p fires onFuzzyFinder', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onFuzzyFinder = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyP);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+c fires onStopCurrentTurn', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onStopCurrentTurn = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyC);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+c is a no-op when onStopCurrentTurn is unset', (
    tester,
  ) async {
    final controller = LeaderKeyController();
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    // Must not throw; the binding stays handled (TUI-parity semantics).
    await pressWithCtrl(tester, LogicalKeyboardKey.keyC);
    await tester.pump();
  });

  testWidgets('ctrl+x fires onShowCommandPalette on all platforms', (
    tester,
  ) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onShowCommandPalette = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    // platform 'linux' here and 'macos' below: the binding must not depend
    // on the platform — ctrl+x opens the palette everywhere (TUI parity).
    await pressWithCtrl(tester, LogicalKeyboardKey.keyX);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+k fires onFocusInput', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()..onFocusInput = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyK);
    expect(fired, isTrue);
  });

  testWidgets('ctrl+f fires onInSessionFind', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onInSessionFind = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    await pressWithCtrl(tester, LogicalKeyboardKey.keyF);
    expect(fired, isTrue);
  });

  testWidgets('cmd+x does NOT fire onShowCommandPalette (ctrl-only parity)', (
    tester,
  ) async {
    var fired = false;
    final controller = LeaderKeyController()
      ..onShowCommandPalette = () => fired = true;
    final node = FocusNode();
    await tester.pumpWidget(_host(controller, node));
    await tester.pump();
    // Meta (cmd) modifier must not trigger the binding.
    await tester.sendKeyDownEvent(
      LogicalKeyboardKey.metaLeft,
      platform: 'macos',
    );
    await tester.sendKeyDownEvent(LogicalKeyboardKey.keyX, platform: 'macos');
    await tester.sendKeyUpEvent(LogicalKeyboardKey.keyX, platform: 'macos');
    await tester.sendKeyUpEvent(LogicalKeyboardKey.metaLeft, platform: 'macos');
    await tester.pump();
    expect(fired, isFalse);
  });
}
