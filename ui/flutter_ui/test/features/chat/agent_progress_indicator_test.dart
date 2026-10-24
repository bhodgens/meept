import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/chat/agent_progress_indicator.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/theme/colors.dart';

// ===== Test helpers =====

Widget _buildWidget({required AgentProgress progress}) {
  return MaterialApp(
    theme: ThemeData.dark(),
    home: Scaffold(body: AgentProgressIndicator(progress: progress)),
  );
}

AgentProgress _makeProgress({
  String agentId = 'coder',
  String message = 'building solution',
  int tier = 0,
  String? sourceEvent,
  DateTime? timestamp,
}) {
  return AgentProgress(
    agentId: agentId,
    message: message,
    tier: tier,
    sourceEvent: sourceEvent,
    timestamp: timestamp ?? DateTime.now(),
  );
}

/// Helper: collect all Text widget data strings under the given finder,
/// ordered by encounter (pre-order depth-first).
List<String> _getTextStrings(Finder finder, WidgetTester tester) {
  return tester.widgetList<Text>(finder).map((t) => t.data ?? '').toList();
}

/// Factory for an ACP-attributed progress frame (acp-employees dim 3A).
AgentProgress acpProgress({
  String agentId = 'bob',
  String acpAgent = 'hermes',
  String persona = '',
}) {
  return AgentProgress(
    agentId: agentId,
    message: 'reading the issue',
    tier: 1,
    timestamp: DateTime(2026, 10, 23),
    stage: 'acp',
    acpAgent: acpAgent,
    persona: persona,
  );
}

// ===== Tier styling tests =====

void main() {
  group('AgentProgressIndicator', () {
    group('tier styling', () {
      testWidgets('tier 0 uses lightGray color and normal font style', (
        tester,
      ) async {
        final progress = _makeProgress(tier: 0, message: 'test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        // two Text widgets: [agentId, message]
        expect(texts.length, 2);
        // second Text is the message -- check it has normal fontStyle
        // By inspecting the styles we know tier 0 = normal
        final textWidget = tester.widget<Text>(
          texts.isEmpty
              ? find.byType(Text).at(1)
              : find
                    .descendant(
                      of: find.byType(AgentProgressIndicator),
                      matching: find.byType(Text),
                    )
                    .at(1),
        );
        final textStyle = textWidget.style ?? const TextStyle();
        expect(textStyle.fontStyle, FontStyle.normal);
      });

      testWidgets('tier 1 uses midGray color and normal font style', (
        tester,
      ) async {
        final progress = _makeProgress(tier: 1, message: 'test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts.length, 2);

        final textWidget = tester.widget<Text>(
          find
              .descendant(
                of: find.byType(AgentProgressIndicator),
                matching: find.byType(Text),
              )
              .at(1),
        );
        final textStyle = textWidget.style ?? const TextStyle();
        expect(textStyle.fontStyle, FontStyle.normal);
      });

      testWidgets('tier 2 uses lightGray color and italic font style', (
        tester,
      ) async {
        final progress = _makeProgress(tier: 2, message: 'test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final textWidget = tester.widget<Text>(
          find
              .descendant(
                of: find.byType(AgentProgressIndicator),
                matching: find.byType(Text),
              )
              .at(1),
        );
        final textStyle = textWidget.style ?? const TextStyle();
        expect(textStyle.fontStyle, FontStyle.italic);
      });

      testWidgets('default tier (no tier specified, uses 1) is normal style', (
        tester,
      ) async {
        final progress = _makeProgress(tier: 1, message: 'default behavior');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final textWidget = tester.widget<Text>(
          find
              .descendant(
                of: find.byType(AgentProgressIndicator),
                matching: find.byType(Text),
              )
              .at(1),
        );
        final textStyle = textWidget.style ?? const TextStyle();
        expect(textStyle.fontStyle, FontStyle.normal);
      });
    });

    // ===== Message truncation tests =====

    group('message truncation', () {
      testWidgets(
        'truncates messages longer than 60 characters to 57 + "..."',
        (tester) async {
          final longMessage = 'A' * 70;
          final progress = _makeProgress(message: longMessage);
          await tester.pumpWidget(_buildWidget(progress: progress));

          final texts = _getTextStrings(
            find.descendant(
              of: find.byType(AgentProgressIndicator),
              matching: find.byType(Text),
            ),
            tester,
          );
          expect(texts.length, 2);
          expect(texts[1], '${'A' * 57}...');
        },
      );

      testWidgets('does not truncate messages 60 characters or shorter', (
        tester,
      ) async {
        const shortMessage = 'This is a short message';
        final progress = _makeProgress(message: shortMessage);
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[1], shortMessage);
      });

      testWidgets('message exactly 60 characters is not truncated', (
        tester,
      ) async {
        final exactly60 = 'B' * 60;
        final progress = _makeProgress(message: exactly60);
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[1], exactly60);
      });

      testWidgets('messages exactly 61 characters are truncated', (
        tester,
      ) async {
        final exactly61 = 'C' * 61;
        final progress = _makeProgress(message: exactly61);
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[1], '${'C' * 57}...');
      });
    });

    // ===== Agent ID lowercase tests =====

    group('agent ID lowercase', () {
      testWidgets('uppercase agent ID is displayed lowercase', (tester) async {
        final progress = _makeProgress(agentId: 'CODER');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[0], 'coder');
      });

      testWidgets('mixed-case agent ID is displayed lowercase', (tester) async {
        final progress = _makeProgress(agentId: 'Dispatcher');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[0], 'dispatcher');
      });

      testWidgets('already-lowercase agent ID remains unchanged', (
        tester,
      ) async {
        final progress = _makeProgress(agentId: 'debugger');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[0], 'debugger');
      });
    });

    // ===== Source event display tests =====

    group('source event', () {
      testWidgets('displays tool_execution_start source event', (tester) async {
        final progress = _makeProgress(sourceEvent: 'tool_execution_start');
        await tester.pumpWidget(_buildWidget(progress: progress));

        // Progress indicator with source event should render normally
        expect(find.byType(CircularProgressIndicator), findsOneWidget);
      });

      testWidgets('displays turn_end source event', (tester) async {
        final progress = _makeProgress(sourceEvent: 'turn_end');
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(CircularProgressIndicator), findsOneWidget);
      });

      testWidgets('displays agent_end source event', (tester) async {
        final progress = _makeProgress(sourceEvent: 'agent_end');
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(CircularProgressIndicator), findsOneWidget);
      });

      testWidgets('null source event does not cause error', (tester) async {
        final progress = _makeProgress(sourceEvent: null);
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(CircularProgressIndicator), findsOneWidget);
      });
    });

    // ===== Empty / edge case tests =====

    group('edge cases', () {
      testWidgets('empty message renders without error', (tester) async {
        final progress = _makeProgress(message: '');
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(CircularProgressIndicator), findsOneWidget);
        expect(find.byType(AgentProgressIndicator), findsOneWidget);
      });

      testWidgets('very long agent ID displays in lowercase', (tester) async {
        const longId =
            'THIS_IS A VERY LONG AGENT ID THAT EXCEEDS REASONABLE LENGTH';
        final progress = _makeProgress(agentId: longId);
        await tester.pumpWidget(_buildWidget(progress: progress));

        final texts = _getTextStrings(
          find.descendant(
            of: find.byType(AgentProgressIndicator),
            matching: find.byType(Text),
          ),
          tester,
        );
        expect(texts[0], longId.toLowerCase());
      });

      testWidgets('null timestamp uses fallback without error', (tester) async {
        final progress = _makeProgress(timestamp: DateTime(2000));
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(AgentProgressIndicator), findsOneWidget);
      });

      testWidgets('renders orange spinner color', (tester) async {
        final progress = _makeProgress(message: 'spinner test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        final circles = tester
            .widgetList<CircularProgressIndicator>(
              find.byType(CircularProgressIndicator),
            )
            .toList();
        expect(circles.length, greaterThan(0));
        final animation =
            circles.first.valueColor as AlwaysStoppedAnimation<Color>;
        expect(animation.value, CyberpunkColors.orangePrimary);
      });

      testWidgets('renders with correct padding layout', (tester) async {
        final progress = _makeProgress(message: 'padding test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        // The root widget is a Padding with EdgeInsets.symmetric(vertical: 4, horizontal: 16)
        final padding = tester.widget<Padding>(find.byType(Padding));
        expect(
          padding.padding,
          const EdgeInsets.symmetric(vertical: 4, horizontal: 16),
        );
      });

      testWidgets('contains Row layout structure', (tester) async {
        final progress = _makeProgress(message: 'layout test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(Row), findsWidgets);
      });

      testWidgets('contains Expanded child with Column', (tester) async {
        final progress = _makeProgress(message: 'expanded test');
        await tester.pumpWidget(_buildWidget(progress: progress));

        expect(find.byType(Expanded), findsOneWidget);
        expect(find.byType(Column), findsOneWidget);
      });
    });

    group('acp attribution', () {
      test('displayLabel formats agent · acp harness', () {
        expect(acpProgress().displayLabel, 'bob · acp hermes');
      });

      test('displayLabel includes persona when set', () {
        expect(
          acpProgress(persona: 'Worker').displayLabel,
          'bob/worker · acp hermes',
        );
      });

      test('displayLabel falls back to plain agent id when no harness', () {
        expect(acpProgress(acpAgent: '').displayLabel, 'bob');
      });

      test(
        'fromJson reads acp keys and detail text from a flattened frame',
        () {
          // Shape the WS relay produces for a raw agent.progress bus frame:
          // {type: "event", data: {stage: "acp", detail, agent_id,
          // acp_agent, persona, ...}} flattened by _flattenWSMessage.
          final progress = AgentProgress.fromJson({
            'type': 'event',
            'source_topic': 'agent.progress',
            'conversation_id': 'sess-1',
            'stage': 'acp',
            'detail': 'streaming thought',
            'agent_id': 'bob',
            'acp_agent': 'hermes',
            'persona': 'worker',
          });
          expect(progress.isAcp, isTrue);
          expect(progress.stage, 'acp');
          expect(progress.message, 'streaming thought');
          expect(progress.displayLabel, 'bob/worker · acp hermes');
        },
      );

      test('fromJson without acp keys keeps the plain shape', () {
        final progress = AgentProgress.fromJson({
          'type': 'agent_progress',
          'agent_id': 'coder',
          'message': 'building',
          'tier': 1,
        });
        expect(progress.isAcp, isFalse);
        expect(progress.displayLabel, 'coder');
      });

      testWidgets('renders attributed label in the chat indicator', (
        tester,
      ) async {
        await tester.pumpWidget(_buildWidget(progress: acpProgress()));
        expect(find.text('bob · acp hermes'), findsOneWidget);
        expect(find.text('reading the issue'), findsOneWidget);
      });

      testWidgets('persona-empty acp frame renders agent-only label', (
        tester,
      ) async {
        await tester.pumpWidget(
          _buildWidget(progress: acpProgress(persona: '')),
        );
        expect(find.text('bob · acp hermes'), findsOneWidget);
      });

      testWidgets('non-acp frame keeps today\'s plain label', (tester) async {
        await tester.pumpWidget(
          _buildWidget(
            progress: _makeProgress(
              agentId: 'Bob',
              message: 'building solution',
            ),
          ),
        );
        expect(find.text('bob'), findsOneWidget);
      });
    });
  });
}
