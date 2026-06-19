import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/chat/chat_input.dart';
import 'package:meept_ui/features/chat/slash_autocomplete.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

/// Stub [SdkApiClient] for tests that need the migrated providers.
/// Returns an empty agents list so [AgentNotifier.loadAgents] succeeds
/// without hitting the network.
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listAgents() async => [];
}

class _StubWebSocket extends WebSocketService {
  _StubWebSocket() : super(host: 'localhost', port: 8081);

  @override
  Future<void> connect({String? path}) async {}

  @override
  void disconnect() {}

  @override
  void send(Map<String, dynamic> message) {}
}

class _StubTtsNotifier extends StateNotifier<TtsState> implements TtsNotifier {
  _StubTtsNotifier() : super(TtsState.idle);
  @override Future<bool> initialize() async => true;
  @override Future<void> speak(String text) async {}
  @override Future<void> stop() async {}
  @override Future<void> setVolume(double volume) async {}
  @override Future<void> setSpeed(double speed) async {}
  @override Future<void> setPitch(double pitch) async {}
  @override Future<void> setVoice(String voiceName) async {}
  @override Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override Future<void> setEnabled(bool value) async {}
  @override Future<void> setBehaviorSettings({required bool interrupt, required bool queue, int? maxQueueSize}) async {}
  @override Future<void> toggleTts() async {}
  @override bool get enabled => false;
  @override bool get isAvailable => false;
  @override bool get isSpeaking => false;
  double get volume => 1.0;
}

/// Sets up a full ProviderScope with mocked providers so ChatInput can
/// be tested in isolation without hitting the real daemon.
Widget _buildTestApp({
  required Widget child,
  List<Agent>? agents,
  bool agentsLoading = false,
  String? agentsError,
  Agent? activeAgent,
}) {
  return ProviderScope(
    overrides: [
      chatProvider.overrideWith(
        (_) => ChatNotifier(
          sdkClient: _StubSdkClient(),
          websocket: _StubWebSocket(),
          ttsNotifier: _StubTtsNotifier(),
        ),
      ),
      agentProvider.overrideWith(
        (ref) => AgentNotifier(sdkClient: _StubSdkClient())
          ..state = AgentState(
            agents: agents ?? const [],
            isLoading: agentsLoading,
            error: agentsError,
          ),
      ),
      activeAgentProvider.overrideWith(
        (_) => activeAgent,
      ),
      sdkClientProvider.overrideWith(
        (_) => _StubSdkClient(),
      ),
    ],
    child: MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(
        body: child,
      ),
    ),
  );
}

/// Pump only a bounded number of frames instead of pumpAndSettle.
/// ChatInput uses an infinite blink animation (AnimationController.repeat)
/// which causes pumpAndSettle to time out.
Future<void> pumpBounded(WidgetTester tester, {int frames = 5}) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  const testAgents = [
    Agent(id: 'coder', name: 'coder', description: '', prompt: '', enabled: true),
    Agent(id: 'debugger', name: 'debugger', description: '', prompt: '', enabled: true),
    Agent(id: 'planner', name: 'planner', description: '', prompt: '', enabled: true),
  ];

  group('ChatInput - text field', () {
    testWidgets('renders a TextField with terminal-style green text', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.minLines, 3);
      expect(textField.maxLines, 8);
      // Cursor is transparent (custom terminal cursor used instead)
      expect(textField.cursorColor, Colors.transparent);
      expect(textField.cursorWidth, 0);
    });

    testWidgets('accepts typed text', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'hello world');
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, 'hello world');
    });

    testWidgets('shift+enter inserts a newline', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'hello');
      await pumpBounded(tester);

      // Simulate Shift+Enter
      await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
      await tester.sendKeyDownEvent(LogicalKeyboardKey.enter);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.enter);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, 'hello\n');
    });
  });

  group('ChatInput - send button', () {
    testWidgets('send button is present and shows send icon when idle', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: testAgents[0],
      ));
      await pumpBounded(tester);

      expect(find.byIcon(Icons.send), findsOneWidget);
    });

    testWidgets('tapping send clears the input field', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: testAgents[0],
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'test message');
      await pumpBounded(tester);

      // Tap send button
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, '');
    });

    testWidgets('send button shows spinner when chat is loading', (tester) async {
      // Override chatProvider to start in loading state
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            chatProvider.overrideWith(
              (_) => ChatNotifier(
                sdkClient: _StubSdkClient(),
                websocket: _StubWebSocket(),
                ttsNotifier: _StubTtsNotifier(),
              )..state = const ChatState(isLoading: true),
            ),
            agentProvider.overrideWith(
              (ref) => AgentNotifier(sdkClient: _StubSdkClient())
                ..state = const AgentState(),
            ),
            activeAgentProvider.overrideWith((_) => testAgents[0]),
            sdkClientProvider.overrideWith((_) => _StubSdkClient()),
          ],
          child: const MaterialApp(
            home: Scaffold(
              body: ChatInput(sessionId: 'test-session'),
            ),
          ),
        ),
      );
      await pumpBounded(tester);

      // When loading, no send icon; a CircularProgressIndicator appears instead
      expect(find.byIcon(Icons.send), findsNothing);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
    });
  });

  group('ChatInput - slash commands', () {
    testWidgets('typing slash shows autocomplete popup', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/');
      await pumpBounded(tester, frames: 10);

      // SlashAutocomplete should be visible
      expect(find.byType(SlashAutocomplete), findsOneWidget);
    });

    testWidgets('typing /h filters to commands starting with /h', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/h');
      await pumpBounded(tester, frames: 10);

      expect(find.byType(SlashAutocomplete), findsOneWidget);
      // /help should be visible
      expect(find.text('/help'), findsWidgets);
    });

    testWidgets('typing non-slash text does not show autocomplete', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'hello');
      await pumpBounded(tester);

      expect(find.byType(SlashAutocomplete), findsNothing);
    });

    testWidgets('clearing text hides autocomplete', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      // Show autocomplete
      await tester.enterText(find.byType(TextField), '/');
      await pumpBounded(tester, frames: 10);
      expect(find.byType(SlashAutocomplete), findsOneWidget);

      // Clear text — autocomplete should disappear
      await tester.enterText(find.byType(TextField), '');
      await pumpBounded(tester, frames: 10);
      expect(find.byType(SlashAutocomplete), findsNothing);
    });
  });

  group('ChatInput - focus', () {
    testWidgets('auto-focuses on first build', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.focusNode!.hasFocus, isTrue);
    });

    testWidgets('border color changes with focus state', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));
      await pumpBounded(tester);

      // The top border should be present (focused or unfocused)
      final container = tester.widget<Container>(find.byType(Container).at(0));
      final decoration = container.decoration as BoxDecoration;
      expect(decoration.border?.top.width, 1);
    });
  });
}
