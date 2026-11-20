// 06-planning TEST-GATE T4 (F13c): GUI mode chip delineates
// plan/quickplan/direct/recall turns from the C1 mode signal.
//
// RED today for missing-implementation reasons:
//   - ChatMessage carries NO typed suggestedMode field; the only mode
//     signal is the async-ack markdown "**mode:**" line (handler.go
//     FormatEnhancedAsyncTaskAck). chat_message_bubble.dart:130-135 has
//     the explicit TODO: render a dedicated mode chip once ChatMessage
//     carries a typed suggestedMode field.
//   - _consumeTurnTerminal builds the reply ChatMessage WITHOUT the
//     event's intentType/handlerCase, so the C1 mode signal never reaches
//     the bubble even though TurnTerminalEvent carries it.
//
// The chip must use a MAPPED user label (plan / quick plan / direct /
// recall), never the raw handler_case enum, and must be a dedicated chip
// rather than the markdown mode line.
//
// Compile-safe: the field is asserted through a JSON roundtrip and the
// widget is fed via ChatMessage.fromJson, so this file compiles today and
// fails on the assertion until the field + chip land (no direct reference
// to the not-yet-existing constructor param).
//
// Gate tests: do not weaken. GREEN after 06-planning/03 T2 (+05/02 chip).

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/chat_message_bubble.dart';
import 'package:meept_ui/models/api_models.dart';

ChatMessage _modeMessage(String mode) {
  // Feed the mode through JSON so the gate compiles before the typed
  // field exists; fromJson must round-trip suggested_mode once it lands.
  return ChatMessage.fromJson({
    'id': 'msg-mode-$mode',
    'role': 'assistant',
    'content': 'the work is done',
    'timestamp': DateTime.utc(2026, 11, 19).toIso8601String(),
    'suggested_mode': mode,
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
  group('ChatMessage mode field (C1 signal)', () {
    test('suggested_mode round-trips through ChatMessage JSON', () {
      final msg = _modeMessage('quick_plan');
      final json = msg.toJson();
      expect(
        json['suggested_mode'],
        'quick_plan',
        reason:
            'F13c: ChatMessage must carry a typed suggestedMode field '
            '(suggested_mode) so the bubble can render a mode chip; today '
            'the field is dropped (only the markdown **mode:** line exists)',
      );
    });
  });

  group('mode chip (widget pins)', () {
    // Each mode maps to a distinct user-facing label. The chip must show
    // the mapped label, not the raw enum, and not rely on the markdown
    // mode line (the reply content here carries NO **mode:** text).
    final cases = <String, String>{
      'plan': 'plan',
      'quick_plan': 'quick',
      'direct': 'direct',
      'recall': 'recall',
    };

    cases.forEach((mode, label) {
      testWidgets('$mode turn renders a dedicated mode chip', (tester) async {
        await tester.pumpWidget(_buildBubble(_modeMessage(mode)));
        await tester.pump();

        // A dedicated chip surfaces the mapped label somewhere in the
        // bubble. The reply content has no mode text, so a match can only
        // come from the chip (not the markdown line).
        final chip = find.textContaining(label);
        expect(
          chip,
          findsWidgets,
          reason:
              'F13c: a $mode turn must render a dedicated mode chip with '
              'the mapped label "$label" (chat_message_bubble.dart TODO: '
              'surface mode via typed suggestedMode, not markdown)',
        );
      });
    });

    testWidgets('mode chip never leaks the raw handler_case enum', (
      tester,
    ) async {
      // A message whose mode is a raw enum-like value must still render a
      // mapped label, never the raw token. Use quick_plan and assert the
      // raw underscored enum is not shown verbatim as the chip text.
      await tester.pumpWidget(_buildBubble(_modeMessage('quick_plan')));
      await tester.pump();

      expect(
        find.text('quick_plan'),
        findsNothing,
        reason:
            'F13c: the mode chip must use a mapped user label, never the '
            'raw suggested_mode/handler_case enum token',
      );
    });
  });
}
