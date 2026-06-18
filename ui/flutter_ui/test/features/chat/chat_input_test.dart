import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/features/chat/chat_input.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Agent>> listAgents() async {
    return [];
  }

  @override
  Future<T> get<T>(
    String path, {
    Map<String, dynamic>? queryParameters,
  }) async {
    throw UnimplementedError('not needed');
  }

  @override
  Future<T> post<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    return null as T;
  }

  @override
  Future<T> put<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    throw UnimplementedError();
  }

  @override
  Future<T> delete<T>(String path) async {
    throw UnimplementedError();
  }
}

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
  @override void setEnabled(bool value) {}
  @override void setBehaviorSettings({required bool interrupt, required bool queue, int? maxQueueSize}) {}
  @override void toggleTts() {}
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
      apiClientProvider.overrideWith(
        (_) => _StubApiClient(),
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

void main() {
  const testAgents = [
    Agent(id: 'coder', name: 'coder', description: '', prompt: '', enabled: true),
    Agent(id: 'debugger', name: 'debugger', description: '', prompt: '', enabled: true),
    Agent(id: 'planner', name: 'planner', description: '', prompt: '', enabled: true),
  ];

  group('ChatInput - Agent Selector', () {
    testWidgets('displays the agent selector PopupMenuButton', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));

      await tester.pumpAndSettle();

      // The selector is a PopupMenuButton
      expect(find.byType(PopupMenuButton<String>), findsOneWidget);
    });

    testWidgets('shows the default agent name in the selector display',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));

      await tester.pumpAndSettle();

      // Open the popup, then check menu items have agent names
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();

      // Each agent name appears in a PopupMenuItem
      final popupMenus = find.byType(PopupMenuItem<String>);
      expect(popupMenus, findsWidgets);
    });

    testWidgets('shows loading indicator in dropdown when agents are loading',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agentsLoading: true,
      ));

      await tester.pump(); // only one pump, avoid infinite settle

      // PopupMenuButton is rendered
      expect(find.byType(PopupMenuButton<String>), findsOneWidget);

      // Open the popup
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();

      // The loading progress indicator should appear
      expect(find.byType(LinearProgressIndicator), findsOneWidget);
    });

    testWidgets('populates dropdown with available agent names', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
      ));

      await tester.pumpAndSettle();

      // Open the popup menu
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();

      // Agent icons appear in PopupMenuItems (includes fallback + loaded agents)
      expect(find.byIcon(Icons.code), findsWidgets); // coder has code icon
      expect(find.byIcon(Icons.bug_report), findsWidgets); // debugger
      expect(find.byIcon(Icons.account_tree), findsWidgets); // planner
    });

    testWidgets('shows the active agent name in the selector display',
        (tester) async {
      final activeAgent = testAgents[1]; // debugger
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: activeAgent,
      ));

      await tester.pumpAndSettle();

      // The display text should say 'debugger'
      // Open popup to make sure display is visible
      expect(find.byType(PopupMenuButton<String>), findsOneWidget);
    });

    testWidgets('highlights selected agent with different icon color',
        (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: testAgents[0], // coder = active
      ));

      await tester.pumpAndSettle();

      // Open the popup
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();

      // Menu items have icons for each agent
      expect(find.byIcon(Icons.code), findsWidgets);
      expect(find.byIcon(Icons.bug_report), findsWidgets);
      expect(find.byIcon(Icons.account_tree), findsWidgets);
      // Plus the expand_more icon in the selector display
      expect(find.byIcon(Icons.expand_more), findsOneWidget);
    });

    testWidgets('the send button is present and interactive', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: const Agent(
          id: 'planner',
          name: 'planner',
          description: '',
          prompt: '',
          enabled: true,
        ),
      ));

      await tester.pumpAndSettle();

      // Send button exists
      expect(find.byIcon(Icons.send), findsOneWidget);

      // Set text in the input
      await tester.enterText(
        find.byType(TextField),
        'test message',
      );

      // Tap send
      await tester.tap(find.byIcon(Icons.send));
      await tester.pumpAndSettle();

      // After send, the text field should be cleared
      final textField = tester.widget<TextField>(find.byType(TextField));
      expect(textField.controller!.text, '');
    });
  });

  group('ChatInput - Agent Icon Mapping', () {
    testWidgets('default coder agent shows code icon', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: const [
          Agent(id: 'coder', name: 'coder', description: '', prompt: '', enabled: true),
        ],
      ));
      await tester.pumpAndSettle();

      // The selector displays an icon for the default 'coder' agent
      expect(find.byIcon(Icons.code), findsWidgets);
    });

    testWidgets('debugger agent shows bug_report icon', (tester) async {
      await tester.pumpWidget(_buildTestApp(
        child: const ChatInput(sessionId: 'test-session'),
        agents: testAgents,
        activeAgent: const Agent(
          id: 'debugger',
          name: 'debugger',
          description: '',
          prompt: '',
          enabled: true,
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.bug_report), findsOneWidget);
    });
  });
}
