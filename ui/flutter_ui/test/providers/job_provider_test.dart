import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/job_provider.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

// ===== Mock / Stub Classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    if (path == '/queue/jobs') {
      return [] as T;
    }
    if (path == '/queue/stats') {
      return {'queue_depth': 0} as T;
    }
    return {} as T;
  }

  @override
  Future<T> post<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    throw UnimplementedError();
  }

  @override
  Future<T> delete<T>(String path) async {
    throw UnimplementedError();
  }
}

class _TestWebSocket extends WebSocketService {
  _TestWebSocket()
      : _messageController =
            StreamController<Map<String, dynamic>>.broadcast(sync: true),
        super(host: 'localhost', port: 8081);

  final StreamController<Map<String, dynamic>> _messageController;
  bool _connected = false;
  final List<Map<String, dynamic>> _sentMessages = [];
  bool _testJobsSubscribed = false;

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

  void pushMessage(Map<String, dynamic> message) {
    _messageController.add(message);
  }

  @override
  Stream<Map<String, dynamic>> subscribeToJobs() {
    if (!_testJobsSubscribed) {
      send({'type': 'subscribe', 'channel': 'jobs'});
      _testJobsSubscribed = true;
    }
    return _messageController.stream.where((m) => m['type'] == 'job_update');
  }
}

void main() {
  group('JobUpdate', () {
    test('parses from JSON', () {
      final json = {
        'job_id': 'job-123',
        'type': 'agent.chat',
        'status': 'running',
        'agent_id': 'coder',
        'timestamp': '2024-01-01T10:00:00Z',
      };

      final update = JobUpdate.fromJson(json);

      expect(update.jobId, 'job-123');
      expect(update.status, 'running');
      expect(update.agentId, 'coder');
    });

    test('handles missing fields gracefully', () {
      final json = <String, dynamic>{};

      final update = JobUpdate.fromJson(json);

      expect(update.jobId, '');
      expect(update.status, '');
    });

    test('maps id field to jobId', () {
      final json = {
        'id': 'job-456',
        'status': 'completed',
      };

      final update = JobUpdate.fromJson(json);

      expect(update.jobId, 'job-456');
    });
  });

  group('JobNotifier WebSocket subscription', () {
    test('subscribeToJobs sends correct subscribe message', () {
      final ws = _TestWebSocket();

      ws.subscribeToJobs();

      // Expect a subscribe message for jobs channel
      expect(
        ws.sentMessages,
        anyElement(
          equals({
            'type': 'subscribe',
            'channel': 'jobs',
          }),
        ),
      );
    });

    test('JobUpdate receives messages from WS stream', () {
      final ws = _TestWebSocket();
      final jobsStream = ws.subscribeToJobs();
      final received = <JobUpdate>[];

      final sub = jobsStream.listen((msg) {
        received.add(JobUpdate.fromJson(msg));
      });

      // Push a job update message (already flattened by WebSocketService)
      ws.pushMessage(<String, dynamic>{
        'type': 'job_update',
        'job_id': 'job-1',
        'job_type': 'agent.chat',
        'status': 'running',
        'timestamp': DateTime.now().toIso8601String(),
      });

      expect(received, hasLength(1));
      expect(received[0].jobId, 'job-1');
      expect(received[0].status, 'running');

      sub.cancel();
    });

    test('messages without job_update type are filtered out', () {
      final ws = _TestWebSocket();
      final jobsStream = ws.subscribeToJobs();
      final received = <JobUpdate>[];

      final sub = jobsStream.listen((msg) {
        received.add(JobUpdate.fromJson(msg));
      });

      ws.pushMessage({
        'type': 'chat_message',
        'role': 'assistant',
        'content': 'hello',
      });

      // Should still be empty because type is 'chat_message'
      expect(received, isEmpty);

      sub.cancel();
    });
  });
}
