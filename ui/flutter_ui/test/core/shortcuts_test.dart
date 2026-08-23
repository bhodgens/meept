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
  Future<void> pressWithCtrl(WidgetTester tester, LogicalKeyboardKey key) async {
    // The test framework's sendKeyDownEvent does not hold modifiers, so
    // simulate ctrl-down → key-down → ctrl-up manually.
    await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft,
        platform: 'linux');
    await tester.sendKeyDownEvent(key, platform: 'linux');
    await tester.sendKeyUpEvent(key, platform: 'linux');
    await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft,
        platform: 'linux');
    await tester.pump();
  }

  testWidgets('ctrl+s fires onToggleSteer', (tester) async {
    var fired = false;
    final controller = LeaderKeyController()..onToggleSteer = () => fired = true;
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
}
