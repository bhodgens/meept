import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/features/chat/plan_notification_bubble.dart';

void main() {
  group('PlanNotificationData.fromFrame', () {
    test('parses a plan.failed frame via source_topic', () {
      final data = PlanNotificationData.fromFrame({
        'type': 'plan_update',
        'source_topic': 'plan.failed',
        'plan_id': 'plan-abc123',
        'session_id': 'sess-1',
        'title': 'refactor the ingest worker',
        'phase_count': 3,
        'step_count': 9,
        'reason': 'phase 2 compile error',
      });
      expect(data.state, 'failed');
      expect(data.planId, 'plan-abc123');
      expect(data.title, 'refactor the ingest worker');
      expect(data.reason, 'phase 2 compile error');
      expect(data.countLine, '3 phases - 9 steps');
    });

    test('parses every plan lifecycle state', () {
      for (final s in const [
        'submitting',
        'completed',
        'confirmed',
        'rejected',
        'failed',
      ]) {
        final data = PlanNotificationData.fromFrame({
          'source_topic': 'plan.$s',
        });
        expect(data.state, s, reason: 'source_topic plan.$s must map to state');
      }
    });

    test('empty session_id is an evolver sink frame (should not render)', () {
      final data = PlanNotificationData.fromFrame({
        'source_topic': 'plan.submitting',
        'session_id': '',
      });
      expect(data.shouldRenderIn('sess-active'), isFalse);
    });

    test('owning session renders; foreign session does not', () {
      final own = PlanNotificationData.fromFrame({
        'source_topic': 'plan.failed',
        'session_id': 'sess-1',
      });
      expect(own.shouldRenderIn('sess-1'), isTrue);
      expect(own.shouldRenderIn('sess-other'), isFalse);
    });

    test('missing session_id is a legacy frame and renders best-effort', () {
      final data = PlanNotificationData.fromFrame({
        'source_topic': 'plan.completed',
      });
      expect(data.shouldRenderIn('sess-1'), isTrue);
    });
  });

  group('document proposal wording', () {
    test('zero-count plan never reads 0 phases / 0 steps', () {
      final data = PlanNotificationData.fromFrame({
        'source_topic': 'plan.submitting',
        'session_id': 'sess-1',
        'title': 'Skill evolution: improve foo',
        'phase_count': 0,
        'step_count': 0,
      });
      expect(data.countLine, isNot(contains('0 phase')));
      expect(data.countLine, isNot(contains('0 step')));
      expect(data.countLine, contains('document proposal'));
    });

    test('singular counts', () {
      final data = PlanNotificationData.fromFrame({
        'source_topic': 'plan.completed',
        'phase_count': 1,
        'step_count': 1,
      });
      expect(data.countLine, '1 phase - 1 step');
    });
  });

  group('PlanNotificationBubble widget', () {
    Widget host(PlanNotificationData data) => MaterialApp(
      home: Scaffold(body: PlanNotificationBubble(data: data)),
    );

    testWidgets('failed state renders header, title, and reason', (
      tester,
    ) async {
      await tester.pumpWidget(
        host(
          PlanNotificationData.fromFrame({
            'source_topic': 'plan.failed',
            'plan_id': 'plan-abc123',
            'session_id': 'sess-1',
            'title': 'refactor the ingest worker',
            'reason': 'phase 2 compile error',
          }),
        ),
      );
      expect(find.text('plan failed'), findsOneWidget);
      expect(find.text('plan: "refactor the ingest worker"'), findsOneWidget);
      expect(find.text('reason: phase 2 compile error'), findsOneWidget);
    });

    testWidgets('failed state without reason shows the placeholder', (
      tester,
    ) async {
      await tester.pumpWidget(
        host(
          PlanNotificationData.fromFrame({
            'source_topic': 'plan.failed',
            'session_id': 'sess-1',
          }),
        ),
      );
      expect(find.text('plan failed'), findsOneWidget);
      expect(find.text('reason: no reason given'), findsOneWidget);
    });

    testWidgets('submitting state renders the document proposal wording for '
        'a zero-count evolver plan', (tester) async {
      await tester.pumpWidget(
        host(
          PlanNotificationData.fromFrame({
            'source_topic': 'plan.submitting',
            'session_id': 'sess-1',
            'title': 'Skill evolution: improve foo',
            'phase_count': 0,
            'step_count': 0,
          }),
        ),
      );
      expect(find.text('plan ready for review'), findsOneWidget);
      expect(
        find.text('document proposal - no executable steps'),
        findsOneWidget,
      );
      expect(find.textContaining('0 phase'), findsNothing);
      expect(find.textContaining('0 step'), findsNothing);
    });

    testWidgets('completed state renders the awaiting-confirmation line', (
      tester,
    ) async {
      await tester.pumpWidget(
        host(
          PlanNotificationData.fromFrame({
            'source_topic': 'plan.completed',
            'session_id': 'sess-1',
            'title': 'migrate the store',
            'phase_count': 2,
            'step_count': 5,
          }),
        ),
      );
      expect(find.text('plan completed'), findsOneWidget);
      expect(
        find.text('2 phases - 5 steps - awaiting confirmation'),
        findsOneWidget,
      );
    });
  });

  group('headerLine', () {
    test('lowercase headers per state', () {
      expect(
        PlanNotificationData(state: 'submitting').headerLine,
        'plan ready for review',
      );
      expect(
        PlanNotificationData(state: 'completed').headerLine,
        'plan completed',
      );
      expect(
        PlanNotificationData(state: 'confirmed').headerLine,
        'plan confirmed',
      );
      expect(
        PlanNotificationData(state: 'rejected').headerLine,
        'plan rejected',
      );
      expect(PlanNotificationData(state: 'failed').headerLine, 'plan failed');
      expect(PlanNotificationData(state: '').headerLine, 'plan update');
    });
  });
}
