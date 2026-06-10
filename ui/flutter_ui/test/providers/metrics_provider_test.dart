import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/metrics_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/api_client.dart';
import 'package:meept_ui/services/websocket_service.dart';
import 'package:meept_ui/models/api_models.dart';

// ===== Mock / Stub Classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    if (path == '/metrics/live') {
      return {
        'timestamp': DateTime.now().toIso8601String(),
        'active_agents': 2,
        'requests_per_sec': 1.5,
        'token_usage_rate': 50.0,
        'queue_depth': 3,
        'total_sessions': 5,
        'total_jobs': 10,
        'running_jobs': 2,
        'pending_jobs': 3,
        'version': '0.1.0',
      } as T;
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
}

void main() {
  group('MetricsNotifier', () {
    test('initially isLoading', () {
      final ws = _TestWebSocket();
      final notifier = MetricsNotifier(
        apiClient: _StubApiClient(),
        websocket: ws,
      );
      expect(notifier.state.isLoading, isTrue);
    });

    test('sets error when fetch fails', () async {
      // Use an API client that throws (connection refused to port 12345)
      final client = ApiClient(host: 'localhost', port: 12345);
      final ws = _TestWebSocket();
      final notifier = MetricsNotifier(
        apiClient: client,
        websocket: ws,
      );

      // Wait for async fetch to complete
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.error, isNotNull);
    });

    test('disposes subscriptions', () {
      final ws = _TestWebSocket();
      final notifier = MetricsNotifier(
        apiClient: _StubApiClient(),
        websocket: ws,
      );

      notifier.dispose();
      // Should not throw
    });
  });

  group('MetricsSnapshot', () {
    test('parses from API response', () {
      final json = {
        'timestamp': '2024-01-01T10:00:00Z',
        'active_agents': 3,
        'requests_per_sec': 2.5,
        'token_usage_rate': 100.0,
        'queue_depth': 5,
        'total_sessions': 10,
        'total_jobs': 20,
        'running_jobs': 2,
        'pending_jobs': 5,
        'version': '1.0.0',
      };

      final snapshot = MetricsSnapshot.fromJson(json);

      expect(snapshot.activeAgents, 3);
      expect(snapshot.queueDepth, 5);
      expect(snapshot.requestsPerSec, 2.5);
      expect(snapshot.version, '1.0.0');
    });

    test('defaults to zero for missing fields', () {
      final json = {
        'timestamp': '2024-01-01T10:00:00Z',
      };

      final snapshot = MetricsSnapshot.fromJson(json);

      expect(snapshot.activeAgents, 0);
      expect(snapshot.queueDepth, 0);
      expect(snapshot.requestsPerSec, 0.0);
    });
  });
}
