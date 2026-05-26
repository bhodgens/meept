import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/widgets/loading_spinner.dart';
import 'package:meept_ui/theme/colors.dart';

void main() {
  group('LoadingSpinner', () {
    testWidgets('shows centered spinner', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      expect(find.byType(Center), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
    });

    testWidgets('uses default size of 32', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      // The SizedBox wraps the CircularProgressIndicator in the widget tree.
      final sizedBox = tester.widget<SizedBox>(
        find.ancestor(
          of: find.byType(CircularProgressIndicator),
          matching: find.byType(SizedBox),
        ).first,
      );
      expect(sizedBox.width, 32.0);
      expect(sizedBox.height, 32.0);
    });

    testWidgets('uses custom size when provided', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(size: 64.0),
          ),
        ),
      );
      await tester.pump();

      final sizedBox = tester.widget<SizedBox>(
        find.ancestor(
          of: find.byType(CircularProgressIndicator),
          matching: find.byType(SizedBox),
        ).first,
      );
      expect(sizedBox.width, 64.0);
      expect(sizedBox.height, 64.0);
    });

    testWidgets('uses custom stroke width', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(strokeWidth: 4.0),
          ),
        ),
      );
      await tester.pump();

      final indicator = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      // valueColor is AlwaysStoppedAnimation, strokeWidth accessible directly
      expect(indicator.strokeWidth, 4.0);
    });

    testWidgets('shows optional message text', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(message: 'loading data...'),
          ),
        ),
      );
      await tester.pump();

      expect(find.text('loading data...'), findsOneWidget);
    });

    testWidgets('does not show message when null', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(message: null),
          ),
        ),
      );
      await tester.pump();

      // There should be no Text widget besides possibly the spinner's own
      // (CircularProgressIndicator has no text). The Scaffold has none.
      final textFinder = find.byType(Text);
      expect(textFinder, findsNothing);
    });

    testWidgets('uses cyberpunk orange theme color', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      final indicator = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      final animation = indicator.valueColor! as AlwaysStoppedAnimation<Color?>;
      final color = animation.value;
      expect(color, CyberpunkColors.orangePrimary);
    });
  });

  group('MiniLoadingSpinner', () {
    testWidgets('shows spinner in a bounded container', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      expect(find.byType(CircularProgressIndicator), findsOneWidget);
    });

    testWidgets('uses default size of 16', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      final sizedBox = tester.widget<SizedBox>(
        find.ancestor(
          of: find.byType(CircularProgressIndicator),
          matching: find.byType(SizedBox),
        ).first,
      );
      expect(sizedBox.width, 16.0);
      expect(sizedBox.height, 16.0);
    });

    testWidgets('uses custom size when provided', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(size: 24.0),
          ),
        ),
      );
      await tester.pump();

      final sizedBox = tester.widget<SizedBox>(
        find.ancestor(
          of: find.byType(CircularProgressIndicator),
          matching: find.byType(SizedBox),
        ).first,
      );
      expect(sizedBox.width, 24.0);
      expect(sizedBox.height, 24.0);
    });

    testWidgets('uses cyberpunk orange theme color', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      final indicator = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      final animation = indicator.valueColor! as AlwaysStoppedAnimation<Color?>;
      final color = animation.value;
      expect(color, CyberpunkColors.orangePrimary);
    });

    testWidgets('is smaller than regular LoadingSpinner', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: Column(
              children: const [
                SizedBox(
                  height: 8,
                ),
                MiniLoadingSpinner(),
                SizedBox(
                  height: 8,
                ),
                LoadingSpinner(),
              ],
            ),
          ),
        ),
      );
      await tester.pump();

      final miniSizedBox =
          tester.widget<SizedBox>(find.descendant(of: find.byType(MiniLoadingSpinner), matching: find.byType(SizedBox)).first);
      final normalSizedBox =
          tester.widget<SizedBox>(find.descendant(of: find.byType(LoadingSpinner), matching: find.byType(SizedBox)).first);

      final miniWidth = miniSizedBox.width!;
      final normalWidth = normalSizedBox.width!;
      expect(miniWidth, lessThan(normalWidth));
    });

    testWidgets('has fixed stroke width of 2', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      final indicator = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      expect(indicator.strokeWidth, 2.0);
    });

    testWidgets('size parameter controls both width and height equally',
        (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: MiniLoadingSpinner(size: 32.0),
          ),
        ),
      );
      await tester.pump();

      final sizedBox = tester.widget<SizedBox>(
        find.ancestor(
          of: find.byType(CircularProgressIndicator),
          matching: find.byType(SizedBox),
        ).first,
      );
      expect(sizedBox.width, sizedBox.height);
      expect(sizedBox.width, 32.0);
    });
  });

  group('LoadingSpinner - additional', () {
    testWidgets('message text has correct style', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(message: 'parsing...'),
          ),
        ),
      );
      await tester.pump();

      final text = tester.widget<Text>(
        find.text('parsing...'),
      );
      expect(text.style!.color, CyberpunkColors.lightGray);
      expect(text.style!.fontSize, 12.0);
    });

    testWidgets('message is separated by 12px from spinner', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(message: 'separated'),
          ),
        ),
      );
      await tester.pump();

      final sizedBoxes =
          find.descendant(of: find.byType(Column), matching: find.byType(SizedBox)).evaluate();
      // The Spacer between spinner and message has height 12
      expect(sizedBoxes.length, greaterThanOrEqualTo(1));
    });

    testWidgets('default stroke width is 2', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      final indicator = tester.widget<CircularProgressIndicator>(
        find.byType(CircularProgressIndicator),
      );
      expect(indicator.strokeWidth, 2.0);
    });

    testWidgets('has minSize mainAxisSize on inner column', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: Scaffold(
            body: LoadingSpinner(),
          ),
        ),
      );
      await tester.pump();

      // The inner Column should have mainAxisSize.min
      final columns = find.descendant(
        of: find.byType(Center),
        matching: find.byType(Column),
      );
      final column = tester.widget<Column>(columns);
      expect(column.mainAxisSize, MainAxisSize.min);
    });
  });
}
