import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/widgets/command_palette.dart';

void main() {
  testWidgets('shows all 9 items with labels', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    expect(find.text('chat'), findsOneWidget);
    expect(find.text('sessions'), findsOneWidget);
    expect(find.text('agents'), findsOneWidget);
    expect(find.text('new session'), findsOneWidget);
    expect(find.text('find…'), findsOneWidget);
  });

  testWidgets('arrow down moves selection; enter activates', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump();
    // Index 1 = sessions.
    expect(selected?.label, 'sessions');
  });

  testWidgets('click activates the tapped item', (tester) async {
    CommandPaletteItem? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: CommandPalette.defaultItems,
          onSelected: (item) => selected = item,
        ),
      ),
    ));
    await tester.pump();
    await tester.tap(find.text('tasks'));
    await tester.pump();
    expect(selected?.label, 'tasks');
  });

  testWidgets('empty items does not crash on key events', (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: CommandPalette(
          items: const [],
          onSelected: (_) {},
        ),
      ),
    ));
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump();
    // No exception thrown — test passes if we reach here.
  });

  testWidgets('shrinking items list clamps selection without crashing',
      (tester) async {
    CommandPaletteItem? selected;
    late StateSetter setStateOuter;
    List<CommandPaletteItem> items = CommandPalette.defaultItems;

    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: StatefulBuilder(
          builder: (context, setState) {
            setStateOuter = setState;
            return CommandPalette(
              items: items,
              onSelected: (item) => selected = item,
            );
          },
        ),
      ),
    ));
    await tester.pump();
    // Move selection down a few times to reach index 5.
    for (int i = 0; i < 5; i++) {
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pump();
    }
    // Shrink the list to 3 items.
    setStateOuter(() {
      items = CommandPalette.defaultItems.take(3).toList();
    });
    await tester.pump();
    // Now press enter — should not RangeError.
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pump();
    expect(selected, isNotNull);
  });
}
