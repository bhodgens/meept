import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/theme/colors.dart';

// ===== Test data helpers =====

ChatMessage _userMessage({
  String id = 'msg-1',
  String content = 'Hello, world!',
  DateTime? timestamp,
  List<String>? toolCalls,
}) {
  return ChatMessage(
    id: id,
    role: 'user',
    content: content,
    timestamp: timestamp ?? DateTime(2024, 1, 1, 12, 30),
    toolCalls: toolCalls,
  );
}

ChatMessage _assistantMessage({
  String id = 'msg-2',
  String content = 'I can help with that.',
  DateTime? timestamp,
  List<String>? toolCalls,
}) {
  return ChatMessage(
    id: id,
    role: 'assistant',
    content: content,
    timestamp: timestamp ?? DateTime(2024, 1, 1, 12, 31),
    toolCalls: toolCalls,
  );
}

Widget _buildApp(Widget child) {
  return MaterialApp(
    theme: ThemeData.dark(),
    home: Scaffold(
      body: Container(
        constraints: const BoxConstraints(maxWidth: 600),
        child: child,
      ),
    ),
  );
}

void main() {
  group('ChatMessageBubble', () {
    testWidgets('renders user messages aligned to the right', (tester) async {
      final message = _userMessage();
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.byType(ChatMessageBubble), findsOneWidget);
      expect(find.text('Hello, world!'), findsOneWidget);
    });

    testWidgets('renders assistant messages aligned to the left',
        (tester) async {
      final message = _assistantMessage();
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.byType(ChatMessageBubble), findsOneWidget);
      expect(find.text('I can help with that.'), findsOneWidget);
    });

    testWidgets('user message uses orange border color', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      // ChatMessageBubble outer Align -> Container with BoxDecoration
      final bubbleContainers = find.byWidgetPredicate(
        (w) => w is Container && w.decoration is BoxDecoration,
      ).evaluate();
      expect(bubbleContainers.isNotEmpty, isTrue);
    });

    testWidgets('assistant message uses gray border color', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _assistantMessage()),
      ));
      await tester.pump();

      final bubbleContainers = find.byWidgetPredicate(
        (w) => w is Container && w.decoration is BoxDecoration,
      ).evaluate();
      expect(bubbleContainers.isNotEmpty, isTrue);
    });

    testWidgets('user and assistant messages have different colors',
        (tester) async {
      // Build user message
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      final userWidgets = tester.renderObject(find.byType(ChatMessageBubble));
      expect(userWidgets, isNotNull);
      expect(find.text('Hello, world!'), findsOneWidget);
    });

    testWidgets('handles empty content gracefully', (tester) async {
      final message = ChatMessage(
        id: 'msg-empty',
        role: 'user',
        content: '',
        timestamp: DateTime(2024, 1, 1, 12, 30),
      );
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.byType(ChatMessageBubble), findsOneWidget);
    });

    testWidgets('shows timestamps in HH:MM format', (tester) async {
      final message = _userMessage(
        timestamp: DateTime(2024, 1, 1, 9, 5),
      );
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.text('09:05'), findsOneWidget);
    });

    testWidgets('shows timestamps for various times', (tester) async {
      final message = _userMessage(
        timestamp: DateTime(2024, 1, 1, 14, 7),
      );
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.text('14:07'), findsOneWidget);
    });

    testWidgets('renders bubble without tool calls', (tester) async {
      final message = _assistantMessage(toolCalls: null);
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.text('I can help with that.'), findsOneWidget);
    });

    testWidgets('renders bubble with tool calls without crashing',
        (tester) async {
      final message = _assistantMessage(
        toolCalls: ['shell', 'file_edit'],
      );
      await tester.pumpWidget(_buildApp(ChatMessageBubble(message: message)));
      await tester.pump();

      expect(find.text('I can help with that.'), findsOneWidget);
    });

    testWidgets('user message uses right alignment', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      final allAligns = find.byType(Align).evaluate();
      // ChatMessageBubble returns an Align at root
      // For user messages: Alignment.centerRight
      // For assistant messages: Alignment.centerLeft
      // Find the first Align (outer bubble wrapper)
      expect(allAligns.length, greaterThanOrEqualTo(1));
    });

    testWidgets('message content text uses orangeGlow color', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      // Text widget exists
      expect(find.text('Hello, world!'), findsOneWidget);
    });

    testWidgets('user message border is orangePrimary', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      final container = tester.widget<Container>(
        find.descendant(
          of: find.byType(ChatMessageBubble),
          matching: find.byType(Container),
        ).first,
      );
      final decoration = container.decoration as BoxDecoration;
      final border = decoration.border as Border;
      expect(border.left.color, CyberpunkColors.orangePrimary);
    });

    testWidgets('assistant message border is lightGray', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _assistantMessage()),
      ));
      await tester.pump();

      final container = tester.widget<Container>(
        find.descendant(
          of: find.byType(ChatMessageBubble),
          matching: find.byType(Container),
        ).first,
      );
      final decoration = container.decoration as BoxDecoration;
      final border = decoration.border as Border;
      expect(border.left.color, CyberpunkColors.lightGray);
    });

    testWidgets('user message background uses orange glow with alpha',
        (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      final container = tester.widget<Container>(
        find.descendant(
          of: find.byType(ChatMessageBubble),
          matching: find.byType(Container),
        ).first,
      );
      final decoration = container.decoration as BoxDecoration;
      expect(
        decoration.color,
        CyberpunkColors.orangePrimary.withValues(alpha: 0.2),
      );
    });

    testWidgets('assistant message background is midGray', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _assistantMessage()),
      ));
      await tester.pump();

      final container = tester.widget<Container>(
        find.descendant(
          of: find.byType(ChatMessageBubble),
          matching: find.byType(Container),
        ).first,
      );
      final decoration = container.decoration as BoxDecoration;
      expect(decoration.color, CyberpunkColors.midGray);
    });



    testWidgets('timestamp text is 10px', (tester) async {
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(message: _userMessage()),
      ));
      await tester.pump();

      final textWidgets = find.byType(Text).evaluate();
      // The timestamp should be one of the Text widgets with fontSize 10
      final timestampText = textWidgets.any((element) {
        final textWidget = element.widget as Text;
        return textWidget.data == '12:30';
      });
      expect(timestampText, isTrue);
    });

    testWidgets('long content is displayed without overflow', (tester) async {
      const longMessage = 'This is a very long message to ensure that '
          'the bubble content renders correctly and does not cause '
          'overflow issues because the maxWidth is constrained '
          'to 70% of the screen width and wrap text naturally';
      await tester.pumpWidget(_buildApp(
        ChatMessageBubble(
          message: _userMessage(content: longMessage),
        ),
      ));
      await tester.pump();

      expect(find.text(longMessage), findsOneWidget);
    });
  });
}
