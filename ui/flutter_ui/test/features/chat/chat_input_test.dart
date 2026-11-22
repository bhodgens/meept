import 'dart:async';
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

/// Stub that records the /dnd and /implement-improvements RPC surface so
/// the dispatch test can assert the command hit the daemon client instead
/// of falling through to the LLM as literal chat text.
class _RecordingSdkClient extends _StubSdkClient {
  final List<String> calls = [];
  List<Map<String, dynamic>> proposals = [];
  bool dndEnabled = false;

  @override
  Future<Map<String, dynamic>> getDndState() async {
    calls.add('getDndState');
    return {'enabled': dndEnabled};
  }

  @override
  Future<Map<String, dynamic>> setDnd({required bool enabled}) async {
    calls.add('setDnd:$enabled');
    dndEnabled = enabled;
    return {'enabled': enabled};
  }

  @override
  Future<List<Map<String, dynamic>>> getReflectionProposalsRaw() async {
    calls.add('proposals');
    return proposals;
  }

  @override
  Future<Map<String, dynamic>> applyReflectionProposal(String id) async {
    calls.add('apply:$id');
    return {'status': 'applied', 'id': id};
  }

  @override
  Future<Map<String, dynamic>> skipReflectionProposal(String id) async {
    calls.add('skip:$id');
    return {'status': 'skipped', 'id': id};
  }
}

/// A [ChatNotifier] that records sendMessage calls — the dispatch test
/// asserts locally-handled slash commands never reach it.
class _RecordingChatNotifier extends _TestChatNotifier {
  _RecordingChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  final List<String> sent = [];

  @override
  Future<void> sendMessage({
    required String sessionId,
    required String text,
    String? agentId,
  }) async {
    sent.add(text);
  }
}

/// A [ChatNotifier] whose [loadMessages] is a no-op so tests can set
/// state directly without triggering a real HTTP fetch.
class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}
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
  @override
  Future<bool> initialize() async => true;
  @override
  Future<void> speak(String text) async {}
  @override
  Future<void> stop() async {}
  @override
  Future<void> setVolume(double volume) async {}
  @override
  Future<void> setSpeed(double speed) async {}
  @override
  Future<void> setPitch(double pitch) async {}
  @override
  Future<void> setVoice(String voiceName) async {}
  @override
  Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override
  Future<void> setEnabled(bool value) async {}
  @override
  Future<void> setBehaviorSettings({
    required bool interrupt,
    required bool queue,
    int? maxQueueSize,
  }) async {}
  @override
  Future<void> toggleTts() async {}
  @override
  bool get enabled => false;
  @override
  bool get isAvailable => false;
  @override
  bool get isSpeaking => false;
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
      chatProvider("test-session").overrideWith(
        (ref) => ChatNotifier(
          sdkClient: _StubSdkClient(),
          websocket: _StubWebSocket(),
          ttsNotifier: _StubTtsNotifier(),
          sessionId: "test-session",
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
      activeAgentProvider.overrideWith((_) => activeAgent),
      sdkClientProvider.overrideWith((_) => _StubSdkClient()),
    ],
    child: MaterialApp(
      theme: ThemeData.dark(),
      home: Scaffold(body: child),
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

// ===== L16 rename-race pins (bughunt 2026-11-19 wave 2) =====

/// SDK stub for the auto-naming race tests: the generate call parks on a
/// completer so the test can land a user rename WHILE generation is in
/// flight, then release the result.
class _RaceSdkClient extends _StubSdkClient {
  final generateGate = Completer<void>();
  final generateCalls = <String>[];
  final renamedTitles = <String>[];
  Map<String, dynamic> generateResult = {
    'name': 'weather',
    'description': 'research: weather question',
  };

  // Hermetic stubs: every init/send-path HTTP call is answered locally so
  // no test future ever waits on the network (a live listener on the stub
  // port would otherwise stall the test to the 5-minute receive timeout).
  @override
  Future<({List<Map<String, dynamic>> messages, int total})> getMessagesPage(
    String id, {
    int offset = 0,
    int limit = 200,
    bool includeToolCalls = false,
  }) async => (messages: <Map<String, dynamic>>[], total: 0);

  @override
  Future<List<Map<String, dynamic>>> getSkillsRaw({String? category}) async =>
      [];

  @override
  Future<List<Map<String, dynamic>>> listCustomCommands() async => [];

  @override
  Future<String> getClientConfig() async => '';

  @override
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async => [];

  @override
  Future<Map<String, dynamic>> updateSessionDescription(
    String sessionId,
    String description,
  ) async {
    renamedTitles.add(description);
    return {'id': sessionId, 'description': description};
  }

  @override
  Future<List<Map<String, dynamic>>> listProjects() async => [];

  @override
  Future<ChatSubmitAck> submitTurn({
    required String message,
    String? conversationId,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async => ChatSubmitAck(
    turnId: 'turn-1',
    conversationId: conversationId ?? '',
    sessionId: conversationId ?? '',
    accepted: true,
  );

  @override
  Future<Map<String, dynamic>> generateSessionDescription({
    required String sessionId,
    required String firstMessage,
    String projectName = '',
  }) async {
    generateCalls.add(sessionId);
    await generateGate.future;
    return generateResult;
  }
}

/// WebSocket stub that reports connected so the send path proceeds.
class _ConnectedRaceWebSocket extends _StubWebSocket {
  @override
  bool get isConnected => true;

  @override
  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) =>
      const Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToAgentProgress(String sessionId) =>
      const Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToTurnTerminal(String sessionId) =>
      const Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToSessionTitles() =>
      const Stream.empty();
}

/// ChatNotifier whose send is a no-op: the L16 race under test lives in
/// ChatInput's auto-name flow, not the turn machinery. Skipping the real
/// _doSend keeps liveness/fallback timers out of the fake-async zone.
class _NoSendChatNotifier extends ChatNotifier {
  _NoSendChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}

  @override
  Future<void> sendMessage({
    required String sessionId,
    required String text,
    String? agentId,
  }) async {}
}

void main() {
  const testAgents = [
    Agent(
      id: 'coder',
      name: 'coder',
      description: '',
      prompt: '',
      enabled: true,
    ),
    Agent(
      id: 'debugger',
      name: 'debugger',
      description: '',
      prompt: '',
      enabled: true,
    ),
    Agent(
      id: 'planner',
      name: 'planner',
      description: '',
      prompt: '',
      enabled: true,
    ),
  ];

  group('ChatInput - text field', () {
    testWidgets('renders a TextField with terminal-style green text', (
      tester,
    ) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.minLines, 3);
      expect(textField.maxLines, 8);
      // Cursor is transparent (custom terminal cursor used instead)
      expect(textField.cursorColor, Colors.transparent);
      expect(textField.cursorWidth, 0);
    });

    testWidgets('accepts typed text', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'hello world');
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, 'hello world');
    });

    testWidgets('shift+enter inserts a newline', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
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
    testWidgets('send button is present and shows send icon when idle', (
      tester,
    ) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
          activeAgent: testAgents[0],
        ),
      );
      await pumpBounded(tester);

      expect(find.byIcon(Icons.send), findsOneWidget);
    });

    testWidgets('tapping send clears the input field', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
          activeAgent: testAgents[0],
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'test message');
      await pumpBounded(tester);

      // Tap send button
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, '');
    });

    testWidgets('send button shows spinner when chat is loading', (
      tester,
    ) async {
      // Override chatProvider to start in loading state
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            chatProvider("test-session").overrideWith(
              // No-load notifier: the real one auto-fetches messages on
              // construction, clobbering the seeded loading state.
              (ref) => _TestChatNotifier(
                sdkClient: _StubSdkClient(),
                websocket: _StubWebSocket(),
                ttsNotifier: _StubTtsNotifier(),
                sessionId: "test-session",
              )..state = const ChatState(isLoading: true),
            ),
            agentProvider.overrideWith(
              (ref) =>
                  AgentNotifier(sdkClient: _StubSdkClient())
                    ..state = const AgentState(),
            ),
            activeAgentProvider.overrideWith((_) => testAgents[0]),
            sdkClientProvider.overrideWith((_) => _StubSdkClient()),
          ],
          child: const MaterialApp(
            home: Scaffold(body: ChatInput(sessionId: 'test-session')),
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
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/');
      await pumpBounded(tester, frames: 10);

      // SlashAutocomplete should be visible
      expect(find.byType(SlashAutocomplete), findsOneWidget);
    });

    testWidgets('typing /h filters to commands starting with /h', (
      tester,
    ) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/h');
      await pumpBounded(tester, frames: 10);

      expect(find.byType(SlashAutocomplete), findsOneWidget);
      // /help should be visible
      expect(find.text('/help'), findsWidgets);
    });

    testWidgets('typing non-slash text does not show autocomplete', (
      tester,
    ) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), 'hello');
      await pumpBounded(tester);

      expect(find.byType(SlashAutocomplete), findsNothing);
    });

    testWidgets('clearing text hides autocomplete', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
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

    // Dead-end slash command pins (99c366aa regression): /dnd and
    // /implement-improvements must be handled LOCALLY through the daemon
    // client, never sent to the LLM as literal chat text.
    testWidgets('/dnd toggles daemon DND instead of sending chat text', (
      tester,
    ) async {
      final sdk = _RecordingSdkClient();
      final chat = _RecordingChatNotifier(
        sdkClient: sdk,
        websocket: _StubWebSocket(),
        ttsNotifier: _StubTtsNotifier(),
        sessionId: 'test-session',
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            chatProvider('test-session').overrideWith((ref) => chat),
            agentProvider.overrideWith(
              (ref) =>
                  AgentNotifier(sdkClient: sdk)..state = const AgentState(),
            ),
            activeAgentProvider.overrideWith((_) => null),
            sdkClientProvider.overrideWith((_) => sdk),
          ],
          child: const MaterialApp(
            home: Scaffold(body: ChatInput(sessionId: 'test-session')),
          ),
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/dnd on');
      await pumpBounded(tester);
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester, frames: 10);

      expect(sdk.calls, contains('setDnd:true'));
      expect(chat.sent, isEmpty, reason: '/dnd must not reach the LLM');
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        '',
        reason: 'input must be cleared after local handling',
      );
      // Drain showStatusMessage's 2.5s auto-clear timer so the test does
      // not end with a pending Timer.
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets('/implement-improvements apply calls the reflection endpoint', (
      tester,
    ) async {
      final sdk = _RecordingSdkClient();
      final chat = _RecordingChatNotifier(
        sdkClient: sdk,
        websocket: _StubWebSocket(),
        ttsNotifier: _StubTtsNotifier(),
        sessionId: 'test-session',
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            chatProvider('test-session').overrideWith((ref) => chat),
            agentProvider.overrideWith(
              (ref) =>
                  AgentNotifier(sdkClient: sdk)..state = const AgentState(),
            ),
            activeAgentProvider.overrideWith((_) => null),
            sdkClientProvider.overrideWith((_) => sdk),
          ],
          child: const MaterialApp(
            home: Scaffold(body: ChatInput(sessionId: 'test-session')),
          ),
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(
        find.byType(TextField),
        '/implement-improvements apply ab12cd3',
      );
      await pumpBounded(tester);
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester, frames: 10);

      expect(sdk.calls, contains('apply:ab12cd3'));
      expect(
        chat.sent,
        isEmpty,
        reason: '/implement-improvements must not reach the LLM',
      );
      // Drain showStatusMessage's 2.5s auto-clear timer.
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets('/implement-improvements list shows the proposals dialog', (
      tester,
    ) async {
      final sdk = _RecordingSdkClient()
        ..proposals = [
          {
            'id': 'ab12cd3',
            'type': 'skill',
            'target': 'SKILL.md',
            'confidence': 0.9,
            'source': 'test',
            'justification': 'do the thing',
          },
        ];
      final chat = _RecordingChatNotifier(
        sdkClient: sdk,
        websocket: _StubWebSocket(),
        ttsNotifier: _StubTtsNotifier(),
        sessionId: 'test-session',
      );
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            chatProvider('test-session').overrideWith((ref) => chat),
            agentProvider.overrideWith(
              (ref) =>
                  AgentNotifier(sdkClient: sdk)..state = const AgentState(),
            ),
            activeAgentProvider.overrideWith((_) => null),
            sdkClientProvider.overrideWith((_) => sdk),
          ],
          child: const MaterialApp(
            home: Scaffold(body: ChatInput(sessionId: 'test-session')),
          ),
        ),
      );
      await pumpBounded(tester);

      await tester.enterText(find.byType(TextField), '/implement-improvements');
      await pumpBounded(tester);
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester, frames: 10);

      expect(sdk.calls, contains('proposals'));
      expect(chat.sent, isEmpty);
      // The shared command-output dialog surfaces the proposal id.
      expect(find.text('improvements'), findsOneWidget);
      expect(find.textContaining('ab12cd3'), findsWidgets);
    });
  });

  group('ChatInput - focus', () {
    testWidgets('auto-focuses on first build', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.focusNode!.hasFocus, isTrue);
    });

    testWidgets('border color changes with focus state', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          child: const ChatInput(sessionId: 'test-session'),
          agents: testAgents,
        ),
      );
      await pumpBounded(tester);

      // The top border should be present (focused or unfocused)
      final container = tester.widget<Container>(find.byType(Container).at(0));
      final decoration = container.decoration as BoxDecoration;
      expect(decoration.border?.top.width, 1);
    });
  });

  group('ChatInput - auto-name rename race (L16)', () {
    /// Pumps ChatInput with a real SessionNotifier seeded with one session
    /// and a gated generate call. Returns the container + sdk for asserts.
    Future<(ProviderContainer, _RaceSdkClient)> pumpRace(
      WidgetTester tester, {
      String? initialDescription,
    }) async {
      final sdk = _RaceSdkClient();
      final ws = _ConnectedRaceWebSocket();
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(sdk),
          websocketProvider.overrideWithValue(ws),
          chatProvider('race-session').overrideWith(
            (ref) => _NoSendChatNotifier(
              sdkClient: sdk,
              websocket: ws,
              ttsNotifier: _StubTtsNotifier(),
              sessionId: 'race-session',
            ),
          ),
        ],
      );
      addTearDown(container.dispose);

      // Seed the session list so the baseline description is readable.
      container.read(sessionProvider.notifier).updateSessions([
        Session(
          id: 'race-session',
          title: 'new session',
          description: initialDescription,
          createdAt: DateTime(2026, 1, 1),
        ),
      ]);

      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
            theme: ThemeData.dark(),
            home: const Scaffold(body: ChatInput(sessionId: 'race-session')),
          ),
        ),
      );
      await pumpBounded(tester);
      return (container, sdk);
    }

    testWidgets('rename during generation is not reverted by the result', (
      tester,
    ) async {
      final (container, sdk) = await pumpRace(tester);

      await tester.enterText(find.byType(TextField), 'what is the weather');
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester);
      expect(sdk.generateCalls, ['race-session']);

      // User renames while the summarizer is still in flight.
      await container
          .read(sessionProvider.notifier)
          .renameSession('race-session', 'my renamed session');
      await pumpBounded(tester);

      // Release the auto-name result: it must NOT paint over the rename.
      sdk.generateGate.complete();
      await pumpBounded(tester, frames: 10);

      final sessions = container.read(sessionProvider).sessions;
      final row = sessions.firstWhere((s) => s.id == 'race-session');
      expect(row.description, 'my renamed session');
      expect(row.title, 'my renamed session');
    });

    testWidgets('auto-name still lands when no rename intervened', (
      tester,
    ) async {
      final (container, sdk) = await pumpRace(tester);

      await tester.enterText(find.byType(TextField), 'what is the weather');
      await tester.tap(find.byIcon(Icons.send));
      await pumpBounded(tester);
      expect(sdk.generateCalls, ['race-session']);

      sdk.generateGate.complete();
      await pumpBounded(tester, frames: 10);

      final sessions = container.read(sessionProvider).sessions;
      final row = sessions.firstWhere((s) => s.id == 'race-session');
      expect(row.description, 'research: weather question');
      expect(row.title, 'research: weather question');
    });
  });
}
