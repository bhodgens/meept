import 'dart:async';
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';
import '../core/constants.dart';

/// WebSocket service for real-time updates
class WebSocketService {
  WebSocketChannel? _channel;
  final String _host;
  final int _port;
  final StreamController<Map<String, dynamic>> _messageController =
      StreamController<Map<String, dynamic>>.broadcast();
  final StreamController<String> _errorController =
      StreamController<String>.broadcast();
  final StreamController<bool> _connectionController =
      StreamController<bool>.broadcast();

  bool _isConnected = false;
  Timer? _pingTimer;

  WebSocketService({
    String? host,
    int? port,
  })  : _host = host ?? AppConstants.defaultApiHost,
        _port = port ?? AppConstants.defaultApiPort;

  /// Connection state stream
  Stream<bool> get connectionStream => _connectionController.stream;

  /// Incoming messages stream
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;

  /// Error stream
  Stream<String> get errorStream => _errorController.stream;

  bool get isConnected => _isConnected;

  /// Connect to WebSocket
  Future<void> connect({String? path}) async {
    if (_isConnected) return;

    try {
      final wsPath = path ?? '/ws';
      final uri = Uri('ws://$_host:$_port$wsPath');

      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        (data) {
          try {
            final message = jsonDecode(data as String) as Map<String, dynamic>;
            _messageController.add(message);
          } catch (e) {
            _errorController.add('Failed to parse message: $e');
          }
        },
        onError: (error) {
          _isConnected = false;
          _connectionController.add(false);
          _errorController.add('WebSocket error: $error');
        },
        onDone: () {
          _isConnected = false;
          _connectionController.add(false);
          _startReconnectTimer();
        },
      );

      _isConnected = true;
      _connectionController.add(true);
      _startPingTimer();
    } catch (e) {
      _isConnected = false;
      _connectionController.add(false);
      _errorController.add('Connection failed: $e');
    }
  }

  /// Disconnect from WebSocket
  void disconnect() {
    _pingTimer?.cancel();
    _channel?.sink.close();
    _isConnected = false;
    _connectionController.add(false);
  }

  /// Send message
  void send(Map<String, dynamic> message) {
    if (!_isConnected) {
      _errorController.add('Cannot send: not connected');
      return;
    }
    _channel?.sink.add(jsonEncode(message));
  }

  void _startPingTimer() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(AppConstants.pingInterval, (_) {
      send({'type': 'ping', 'timestamp': DateTime.now().toIso8601String()});
    });
  }

  void _startReconnectTimer() {
    Timer(Duration(seconds: 5), () {
      connect();
    });
  }

  /// Subscribe to chat messages
  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) {
    send({'type': 'subscribe', 'channel': 'chat', 'session_id': sessionId});
    return _messageController.stream
        .where((m) => m['type'] == 'chat_message' && m['session_id'] == sessionId);
  }

  /// Subscribe to job updates
  Stream<Map<String, dynamic>> subscribeToJobs() {
    send({'type': 'subscribe', 'channel': 'jobs'});
    return _messageController.stream
        .where((m) => m['type'] == 'job_update');
  }

  /// Subscribe to metrics updates
  Stream<Map<String, dynamic>> subscribeToMetrics() {
    send({'type': 'subscribe', 'channel': 'metrics'});
    return _messageController.stream
        .where((m) => m['type'] == 'metrics_update');
  }
}
