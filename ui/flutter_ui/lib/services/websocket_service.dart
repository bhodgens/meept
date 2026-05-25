import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'package:web_socket_channel/web_socket_channel.dart';
import '../core/constants.dart';

/// WebSocket service for real-time updates
class WebSocketService {
  WebSocketChannel? _channel;
  final String _host;
  final int _port;
  final String? _apiKey;
  final StreamController<Map<String, dynamic>> _messageController =
      StreamController<Map<String, dynamic>>.broadcast();
  final StreamController<String> _errorController =
      StreamController<String>.broadcast();
  final StreamController<bool> _connectionController =
      StreamController<bool>.broadcast();

  bool _isConnected = false;
  bool _isConnOpen = false;
  bool _wasExplicitlyDisconnected = false;
  Timer? _pingTimer;

  // Reconnect tracking
  int _retryCount = 0;

  WebSocketService({
    String? host,
    int? port,
    String? apiKey,
  })  : _host = host ?? AppConstants.defaultApiHost,
        _port = port ?? AppConstants.defaultApiPort,
        _apiKey = apiKey;

  /// Connection state stream
  Stream<bool> get connectionStream => _connectionController.stream;

  /// Incoming messages stream
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;

  /// Error stream
  Stream<String> get errorStream => _errorController.stream;

  bool get isConnected => _isConnected;

  /// Connect to WebSocket
  Future<void> connect({String? path}) async {
    if (_isConnected || _isConnOpen || _wasExplicitlyDisconnected) return;

    try {
      _isConnOpen = false;

      final wsPath = path ?? '/api/v1/ws';
      final uriBuilder = Uri.parse('ws://$_host:$_port$wsPath');
      final uri = _buildUriWithAuth(uriBuilder);

      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        (data) {
          try {
            final message = jsonDecode(data as String) as Map<String, dynamic>;
            _messageController.add(message);

            if (!_isConnOpen) {
              _isConnOpen = true;
              final type = message['type'] as String?;
              if (type == 'ping' || type == 'status' || type != null) {
                _isConnected = true;
                _connectionController.add(true);
                _startPingTimer();
                _retryCount = 0;
              }
            }
          } catch (e) {
            _errorController.add('Failed to parse message: \$e');
          }
        },
        onError: (error) {
          _isConnected = false;
          _isConnOpen = false;
          _connectionController.add(false);
          _errorController.add('WebSocket error: \$error');
          _handleReconnect();
        },
        onDone: () {
          _isConnected = false;
          _isConnOpen = false;
          _connectionController.add(false);
          _handleReconnect();
        },
      );
    } catch (e) {
      _isConnected = false;
      _isConnOpen = false;
      _connectionController.add(false);
      _errorController.add('Connection failed: \$e');
      _handleReconnect();
    }
  }

  Uri _buildUriWithAuth(Uri baseUri) {
    if (_apiKey != null && _apiKey!.isNotEmpty) {
      final queryParameters = Map<String, dynamic>.from(baseUri.queryParameters)
        ..['token'] = _apiKey!;
      return baseUri.replace(queryParameters: queryParameters);
    }
    return baseUri;
  }

  Duration _computeReconnectDelay() {
    if (_retryCount >= AppConstants.maxRetries) {
      return const Duration(seconds: 30);
    }
    const baseDelay = Duration(seconds: 2);
    final exponentialDelay = baseDelay * (1 << _retryCount);
    final jitter = Duration(milliseconds: Random().nextInt(1000));
    _retryCount++;
    return exponentialDelay + jitter;
  }

  void _handleReconnect() {
    final delay = _computeReconnectDelay();
    Timer(delay, () {
      connect();
    });
  }

  /// Disconnect from WebSocket and dispose resources
  void disconnect() {
    _wasExplicitlyDisconnected = true;
    _pingTimer?.cancel();
    _pingTimer = null;
    _channel?.sink.close();
    _channel = null;
    _isConnected = false;
    _isConnOpen = false;
    _retryCount = 0;
    _connectionController.add(false);

    // Close StreamControllers to prevent memory leaks
    _messageController.close();
    _errorController.close();
    _connectionController.close();
  }

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

  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) {
    send({'type': 'subscribe', 'channel': 'chat', 'session_id': sessionId});
    return _messageController.stream
        .where((m) => m['type'] == 'chat_message' && m['session_id'] == sessionId);
  }

  Stream<Map<String, dynamic>> subscribeToJobs() {
    send({'type': 'subscribe', 'channel': 'jobs'});
    return _messageController.stream
        .where((m) => m['type'] == 'job_update');
  }

  Stream<Map<String, dynamic>> subscribeToMetrics() {
    send({'type': 'subscribe', 'channel': 'metrics'});
    return _messageController.stream
        .where((m) => m['type'] == 'metrics_update');
  }

  /// Dispose all resources
  void dispose() {
    disconnect();
  }
}
