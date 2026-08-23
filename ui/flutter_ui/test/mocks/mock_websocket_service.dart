import 'package:meept_ui/services/websocket_service.dart';

/// Mock WebSocketService for unit tests.
/// Returns empty streams for all subscription methods.
class MockWebSocketService extends WebSocketService {
  MockWebSocketService() : super(host: 'localhost', port: 65440);

  @override
  Future<void> connect({String? path}) async {}

  @override
  Future<void> disconnect() async {}

  @override
  Future<void> dispose() async {}

  @override
  void pause() {}

  @override
  bool get isConnected => false;

  @override
  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) => Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToJobs() => Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToMetrics() => Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToPlans() => Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToAgentProgress(String sessionId) => Stream.empty();

  @override
  Stream<Map<String, dynamic>> subscribeToSessionTitles() => Stream.empty();
}
