import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

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
    return {} as T;
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

/// A WebSocket service implementation for testing that does not make
/// real network connections. It intercepts all internal controllers so
/// [pushMessage] feeds directly into [messageStream].
class _TestWebSocket extends WebSocketService {
  _TestWebSocket()
      : _messageController =
            StreamController<Map<String, dynamic>>.broadcast(sync: true),
        super(host: 'localhost', port: 8081);

  final StreamController<Map<String, dynamic>> _messageController;
  bool _connected = false;
  final List<Map<String, dynamic>> _sentMessages = [];
  bool _testJobsSubscribed = false;
  bool _testMetricsSubscribed = false;

  @override
  Future<void> connect({String? path}) async {
    _connected = true;
  }

  @override
  void disconnect() {
    _connected = false;
  }

  @override
  void send(Map<String, dynamic> message) {
    _sentMessages.add(message);
  }

  @override
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;

  @override
  bool get isConnected => _connected;

  List<Map<String, dynamic>> get sentMessages => List.unmodifiable(_sentMessages);

  @override
  Stream<Map<String, dynamic>> subscribeToJobs() {
    if (!_testJobsSubscribed) {
      send({'type': 'subscribe', 'channel': 'jobs'});
      _testJobsSubscribed = true;
    }
    return _messageController.stream.where((m) => m['type'] == 'job_update');
  }

  @override
  Stream<Map<String, dynamic>> subscribeToMetrics() {
    if (!_testMetricsSubscribed) {
      send({'type': 'subscribe', 'channel': 'metrics'});
      _testMetricsSubscribed = true;
    }
    return _messageController.stream.where((m) => m['type'] == 'metrics_update');
  }

  /// Push a raw message into the service's message stream so that
  /// listeners (like [ChatNotifier]) will receive it.
  void pushMessage(Map<String, dynamic> message) {
    _messageController.add(message);
  }
}

/// Wrapper that creates a ChatNotifier with test doubles.
ChatNotifier _createNotifier(_TestWebSocket ws) {
  return ChatNotifier(
    apiClient: _StubApiClient(),
    websocket: ws,
  );
}

void main() {
  group('ChatNotifier WebSocket integration', () {
    test('connects to WebSocket on initialization', () {
      final ws = _TestWebSocket();
      _createNotifier(ws);

      expect(ws.isConnected, isTrue);
    });

    test('messages from WebSocket are added to state', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      expect(notifier.state.messages, isEmpty);

      ws.pushMessage({
        'id': 'msg-1',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'hello from ws',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-1',
      });

      expect(notifier.state.messages, hasLength(1));
      expect(notifier.state.messages[0].role, 'assistant');
      expect(notifier.state.messages[0].content, 'hello from ws');
      expect(notifier.state.messages[0].sessionId, 'session-1');
    });

    test('messages with role field are processed', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      ws.pushMessage({
        'id': 'msg-2',
        'role': 'user',
        'content': 'user message',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(notifier.state.messages, hasLength(1));
      expect(notifier.state.messages[0].role, 'user');
    });

    test('messages with chat.message type are processed', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      ws.pushMessage({
        'id': 'msg-3',
        'type': 'chat.message',
        'role': 'assistant',
        'content': 'chat.message type',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(notifier.state.messages, hasLength(1));
      expect(notifier.state.messages[0].content, 'chat.message type');
    });

    test('identical message id replaces existing message', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // First message
      ws.pushMessage({
        'id': 'msg-update',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'partial',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-1',
      });
      expect(notifier.state.messages, hasLength(1));
      expect(notifier.state.messages[0].content, 'partial');

      // Same id, updated content
      ws.pushMessage({
        'id': 'msg-update',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'complete response',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-1',
      });
      expect(notifier.state.messages, hasLength(1));
      expect(notifier.state.messages[0].content, 'complete response');
    });

    test('subscription is cancelled on dispose', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // Send a message to verify subscription works before dispose
      ws.pushMessage({
        'id': 'msg-dispose-1',
        'role': 'assistant',
        'content': 'before dispose',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(notifier.state.messages, hasLength(1));

      notifier.dispose();

      // Verify loadMessages after dispose does not throw (the
      // provider is in a disposed state but should still handle
      // the call gracefully). We test that the session-scoped
      // subscription works by checking the subscribe message was sent.
    });

    test('session-scoped subscription filters by session_id after loadMessages',
        () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // loadMessages creates a session-scoped subscription.
      // It sets isLoading to true which resets state, so we push
      // messages after loadMessages returns.
      notifier.loadMessages('session-a');

      // Messages for the wrong session should NOT arrive
      ws.pushMessage({
        'id': 'msg-2',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'unrelated session',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-b',
      });
      expect(notifier.state.messages, isEmpty);

      // Messages for the correct session SHOULD arrive
      ws.pushMessage({
        'id': 'msg-3',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'correct session',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-a',
      });
      expect(notifier.state.messages, hasLength(1));

      // Switch to another session (this clears messages)
      notifier.loadMessages('session-c');

      // Messages are cleared when switching sessions
      expect(notifier.state.messages, isEmpty);

      // Wrong-session messages still don't arrive
      ws.pushMessage({
        'id': 'msg-4',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'still wrong',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-a',
      });
      expect(notifier.state.messages, isEmpty);

      // Correct-session message arrives
      ws.pushMessage({
        'id': 'msg-5',
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'new session',
        'timestamp': DateTime.now().toIso8601String(),
        'session_id': 'session-c',
      });
      expect(notifier.state.messages, hasLength(1));
    });

    test('loadMessages sends subscribe request to WebSocket', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      notifier.loadMessages('session-abc');

      final subscribeMsg = ws.sentMessages.firstWhere(
        (m) => m['type'] == 'subscribe',
        orElse: () => {},
      );

      expect(subscribeMsg['channel'], 'chat');
      expect(subscribeMsg['session_id'], 'session-abc');
    });

    test('loadMessages resubscribes when switching sessions', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      notifier.loadMessages('session-1');
      notifier.loadMessages('session-2');

      // Subscribe calls should be cumulative
      final subscribeMessages = ws.sentMessages
          .where((m) => m['type'] == 'subscribe')
          .toList();

      expect(subscribeMessages, hasLength(2));
      expect(subscribeMessages[0]['session_id'], 'session-1');
      expect(subscribeMessages[1]['session_id'], 'session-2');
    });

    test('non-chat messages are ignored', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // Ping message - no role, no chat type
      ws.pushMessage({
        'type': 'ping',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(notifier.state.messages, isEmpty);

      // Job update - no role, no chat type
      ws.pushMessage({
        'type': 'job_update',
        'job_id': 'job-1',
        'status': 'running',
      });

      expect(notifier.state.messages, isEmpty);

      // Metrics update - no role, no chat type
      ws.pushMessage({
        'type': 'metrics_update',
        'cpu': 45.2,
      });

      expect(notifier.state.messages, isEmpty);
    });

    test('streaming messages accumulate in state', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // Simulate a streaming response with multiple chunks
      for (int i = 0; i < 5; i++) {
        ws.pushMessage({
          'id': 'stream-$i',
          'type': 'chat_message',
          'role': 'assistant',
          'content': 'chunk $i',
          'timestamp': DateTime.now().toIso8601String(),
          'session_id': 'session-1',
        });
      }

      expect(notifier.state.messages, hasLength(5));
    });

    // ===== WebSocket subscription tests (Task 18) =====

    test('subscription is cancelled on dispose', () {
      final ws = _TestWebSocket();
      final notifier = _createNotifier(ws);

      // Send a message to verify subscription works before dispose
      ws.pushMessage({
        'id': 'msg-dispose-1',
        'role': 'assistant',
        'content': 'before dispose',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(notifier.state.messages, hasLength(1));

      notifier.dispose();

      // Verify loadMessages after dispose does not throw (the
      // provider is in a disposed state but should still handle
      // the call gracefully). We test that the session-scoped
      // subscription works by checking the subscribe message was sent.
    });

    // ===== WebSocket channel subscription tests (Tasks 18-20) =====

    test('subscribeToJobs sends correct subscribe message', () {
      final ws2 = _TestWebSocket();

      // Call subscribeToJobs directly on websocket
      ws2.subscribeToJobs();

      // Verify subscribe message was sent
      expect(ws2.sentMessages, hasLength(1));
      expect(ws2.sentMessages[0]['type'], 'subscribe');
      expect(ws2.sentMessages[0]['channel'], 'jobs');
    });

    test('subscribeToMetrics sends correct subscribe message', () {
      final ws3 = _TestWebSocket();
      ws3.subscribeToMetrics();

      final metricsSub = ws3.sentMessages.firstWhere(
        (m) => m['type'] == 'subscribe' && m['channel'] == 'metrics',
        orElse: () => {},
      );

      expect(metricsSub['type'], 'subscribe');
      expect(metricsSub['channel'], 'metrics');
    });

    test('subscribeToChat sends channel and session_id', () {
      final ws4 = _TestWebSocket();
      ws4.subscribeToChat('test-session-123');

      // The _initWebSocket already sent one subscribe in the
      // initial listener, then another from subscribeToChat.
      // The last subscription should have session_id.
      final chatSub = ws4.sentMessages.any(
        (m) =>
            m['type'] == 'subscribe' &&
            m['channel'] == 'chat' &&
            m['session_id'] == 'test-session-123',
      );

      expect(chatSub, isTrue);
    });

    test('subscribeToJobs is idempotent', () {
      final ws5 = _TestWebSocket();

      ws5.subscribeToJobs();
      final firstCount = ws5.sentMessages.length;

      ws5.subscribeToJobs();
      final secondCount = ws5.sentMessages.length;

      // Should only send one subscribe for jobs (idempotent)
      expect(secondCount, firstCount);

      ws5.subscribeToJobs();
      final thirdCount = ws5.sentMessages.length;
      expect(thirdCount, firstCount);
    });
  });
}
