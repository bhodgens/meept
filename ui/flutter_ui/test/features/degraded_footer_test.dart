// Leaf 05 (degraded-signals) GUI footer gate tests.
//
// Pins: a reply whose classification_method provenance (classified_by) is a
// degraded (non-llm) lane renders a "routed by <method>" footer line in the
// chat bubble; an llm-served reply (or one without provenance) renders no
// footer line (byte-identical bubble).
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';

ChatMessage _msg(String classifiedBy) {
  return ChatMessage.fromJson({
    'id': 'msg-degraded-$classifiedBy',
    'role': 'assistant',
    'content': 'the work is done',
    'timestamp': DateTime.utc(2026, 11, 21).toIso8601String(),
    'classified_by': classifiedBy,
  });
}

Widget _buildBubble(ChatMessage message) {
  return ProviderScope(
    child: MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(
        body: Container(
          constraints: const BoxConstraints(maxWidth: 600),
          child: ChatMessageBubble(message: message),
        ),
      ),
    ),
  );
}

void main() {
  group('degraded classification footer (leaf 05)', () {
    testWidgets('heuristic_fallback reply renders the footer line', (
      tester,
    ) async {
      await tester.pumpWidget(_buildBubble(_msg('heuristic_fallback')));
      await tester.pump();
      expect(
        find.textContaining('routed by heuristic_fallback'),
        findsOneWidget,
        reason:
            'a degraded-classified reply must disclose the method '
            'in the bubble footer (leaf 05 degraded-signals)',
      );
    });

    testWidgets('fallback reply renders the footer line', (tester) async {
      await tester.pumpWidget(_buildBubble(_msg('fallback')));
      await tester.pump();
      expect(find.textContaining('routed by fallback'), findsOneWidget);
    });

    testWidgets('llm reply renders no footer line', (tester) async {
      await tester.pumpWidget(_buildBubble(_msg('llm')));
      await tester.pump();
      expect(find.textContaining('routed by'), findsNothing);
    });

    testWidgets('reply without provenance renders no footer line', (
      tester,
    ) async {
      await tester.pumpWidget(_buildBubble(_msg('')));
      await tester.pump();
      expect(find.textContaining('routed by'), findsNothing);
    });

    testWidgets('user bubbles never render the footer', (tester) async {
      final user = ChatMessage.fromJson({
        'id': 'msg-user',
        'role': 'user',
        'content': 'hello',
        'timestamp': DateTime.utc(2026, 11, 21).toIso8601String(),
        'classified_by': 'heuristic_fallback',
      });
      await tester.pumpWidget(_buildBubble(user));
      await tester.pump();
      expect(find.textContaining('routed by'), findsNothing);
    });
  });
}
