import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/widgets/error_banner.dart';
import 'package:meept_ui/theme/colors.dart';

void main() {
  group('ErrorBanner', () {
    testWidgets('shows error message', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'something went wrong')),
        ),
      );
      await tester.pump();
      expect(find.text('something went wrong'), findsOneWidget);
    });

    testWidgets('shows error icon', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(body: ErrorBanner(message: 'test error', onDismiss: () {})),
        ),
      );
      await tester.pump();
      expect(find.byIcon(Icons.error_outline), findsOneWidget);
    });

    testWidgets('shows dismiss button when onDismiss is provided', (tester) async {
      var dismissed = false;
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: ErrorBanner(message: 'test error', onDismiss: () => dismissed = true),
          ),
        ),
      );
      await tester.pump();
      expect(find.byIcon(Icons.close), findsOneWidget);
      await tester.tap(find.byIcon(Icons.close));
      await tester.pump();
      expect(dismissed, isTrue);
    });

    testWidgets('does not show dismiss button when onDismiss is null', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'no dismiss')),
        ),
      );
      await tester.pump();
      expect(find.byIcon(Icons.close), findsNothing);
      expect(find.text('no dismiss'), findsOneWidget);
    });

    testWidgets('uses red alert color scheme', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: ErrorBanner(message: 'color test', onDismiss: () {}),
          ),
        ),
      );
      await tester.pump();
      expect(find.byIcon(Icons.error_outline), findsOneWidget);
      expect(find.byIcon(Icons.close), findsOneWidget);
    });

    testWidgets('truncates long messages', (tester) async {
      const longMessage = 'This is a very long error message that should be truncated';
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: longMessage)),
        ),
      );
      await tester.pump();
      expect(find.text(longMessage), findsOneWidget);
      final text = tester.widget<Text>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byType(Text),
        ),
      );
      expect(text.overflow, TextOverflow.ellipsis);
    });

    testWidgets('error icon color is redAlert', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'color test')),
        ),
      );
      await tester.pump();
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.color, CyberpunkColors.redAlert);
    });

    testWidgets('close icon color is redAlert', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: ErrorBanner(message: 'color test', onDismiss: () {}),
          ),
        ),
      );
      await tester.pump();
      final iconButton = tester.widget<IconButton>(find.byType(IconButton).first);
      expect(iconButton.color, CyberpunkColors.redAlert);
    });

    testWidgets('error text color is redAlert', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'text color test')),
        ),
      );
      await tester.pump();
      final text = tester.widget<Text>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byType(Text),
        ),
      );
      expect(text.style!.color, CyberpunkColors.redAlert);
    });

    testWidgets('has semi-transparent red background', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'bg test')),
        ),
      );
      await tester.pump();
      final containers = find.descendant(
        of: find.byType(ErrorBanner),
        matching: find.byType(Container),
      );
      final container = tester.widget<Container>(containers.first);
      expect(container.color, CyberpunkColors.redAlert.withValues(alpha: 0.2));
    });

    testWidgets('error icon size is 20', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'icon size')),
        ),
      );
      await tester.pump();
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.size, 20.0);
    });

    testWidgets('close icon size is 16', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: ErrorBanner(message: 'close size', onDismiss: () {}),
          ),
        ),
      );
      await tester.pump();
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byIcon(Icons.close),
        ),
      );
      expect(icon.size, 16.0);
    });

    testWidgets('has 12 padding on the container', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'padding test')),
        ),
      );
      await tester.pump();
      final container = tester.widget<Container>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byType(Container),
        ),
      );
      expect(container.padding, const EdgeInsets.all(12));
    });

    testWidgets('message has maxLines 2', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'multiline test')),
        ),
      );
      await tester.pump();
      final text = tester.widget<Text>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byType(Text),
        ),
      );
      expect(text.maxLines, 2);
    });

    testWidgets('message has ellipsis overflow', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorBanner(message: 'overflow test')),
        ),
      );
      await tester.pump();
      final text = tester.widget<Text>(
        find.descendant(
          of: find.byType(ErrorBanner),
          matching: find.byType(Text),
        ),
      );
      expect(text.overflow, TextOverflow.ellipsis);
    });
  });

  group('ErrorText', () {
    testWidgets('displays error message in standard format', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'disk full')),
        ),
      );
      await tester.pump();
      expect(find.text('error: disk full'), findsOneWidget);
    });

    testWidgets('displays error icon', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'disk full')),
        ),
      );
      await tester.pump();
      expect(find.byIcon(Icons.error_outline), findsOneWidget);
    });

    testWidgets('is center-aligned', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'test')),
        ),
      );
      await tester.pump();
      // ErrorText root widget is a Center
      expect(find.byType(ErrorText), findsOneWidget);
    });

    testWidgets('error icon size is 48', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'disk full')),
        ),
      );
      await tester.pump();
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(ErrorText),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.size, 48.0);
    });

    testWidgets('error text shows "error:" prefix', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'timeout')),
        ),
      );
      await tester.pump();
      expect(find.text('error: timeout'), findsOneWidget);
    });

    testWidgets('error text has redAlert color', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'test')),
        ),
      );
      await tester.pump();
      final text = tester.widget<Text>(
        find.descendant(
          of: find.byType(ErrorText),
          matching: find.byType(Text),
        ),
      );
      expect(text.style!.color, CyberpunkColors.redAlert);
    });

    testWidgets('has padding around the content', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'test')),
        ),
      );
      await tester.pump();
      final padding = tester.widget<Padding>(
        find.descendant(
          of: find.byType(ErrorText),
          matching: find.byType(Padding),
        ),
      );
      expect(padding.padding, const EdgeInsets.all(16));
    });

    testWidgets('error icon is redAlert colored', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: const Scaffold(body: ErrorText(message: 'test')),
        ),
      );
      await tester.pump();
      final icon = tester.widget<Icon>(
        find.descendant(
          of: find.byType(ErrorText),
          matching: find.byIcon(Icons.error_outline),
        ),
      );
      expect(icon.color, CyberpunkColors.redAlert);
    });
  });
}
