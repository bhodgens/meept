import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'package:web_socket_channel/web_socket_channel.dart';
import '../core/constants.dart';

/// WebSocket service for real-time updates
///
/// Handles the Go backend's `{type, data}` message format by flattening
/// nested `data` fields onto the top-level map so consumers can access
/// `message['session_id']`, `message['job_id']`, etc. directly.
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

  // Channel subscription tracking
  final Map<String, SessionSubscription> _chatSubscriptions = {};
  bool _jobsSubscribed = false;
  bool _metricsSubscribed = false;

  /// Channels that have been requested via subscribe calls
  Set<String> get _activeChannels => {
        ..._chatSubscriptions.keys.map((_) => 'chat'),
        if (_jobsSubscribed) 'jobs',
        if (_metricsSubscribed) 'metrics',
      };

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

      final wsPath = path ?? '/ws';
      final uriBuilder = Uri.parse('ws://$_host:$_port$wsPath');
      final uri = _buildUriWithAuth(uriBuilder);

      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        (data) {
          try {
            final message = jsonDecode(data as String) as Map<String, dynamic>;

            // Flatten the Go backend's {type, data} format so that fields
            // nested inside `data` are promoted to the top level.
            // This allows consumers to access message['session_id'],
            // message['job_id'], message['role'] etc. directly.
            final flatMessage = _flattenWSMessage(message);
            _messageController.add(flatMessage);

            if (!_isConnOpen) {
              _isConnOpen = true;
              final type = flatMessage['type'] as String?;
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

  /// Flatten a Go backend `{type, data}` message into a flat map.
  ///
  /// If the message has a `data` field that is a map, promote all keys
  /// from `data` onto the top-level map alongside `type`.  If there
  /// is no `data` field, return the message unchanged.  Also converts
  /// Go map-key convention `session_id` -> `session_id` (no-op) and
  /// `job_id` etc. for consistency.
  Map<String, dynamic> _flattenWSMessage(Map<String, dynamic> msg) {
    final data = msg['data'];
    if (data is Map<String, dynamic>) {
      final flat = <String, dynamic>{
        'type': msg['type'],
      };
      flat.addAll(data);
      // Preserve timestamp from top level if data doesn't have one
      if (msg['timestamp'] != null && flat['timestamp'] == null) {
        flat['timestamp'] = msg['timestamp'];
      }
      return flat;
    }
    return msg..['timestamp'] = msg['timestamp'] ?? msg['timestamp'];
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
    _chatSubscriptions.clear();
    _jobsSubscribed = false;
    _metricsSubscribed = false;
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

  /// Session-scoped chat subscription.
  ///
  /// Returns a stream that emits only chat messages matching the given
  /// [sessionId]. The Flutter client manages the server-side subscription
  /// request internally.
  Stream<Map<String, dynamic>> subscribeToChat(String sessionId) {
    // Send subscribe request for this session
    send({
      'type': 'subscribe',
      'channel': 'chat',
      'session_id': sessionId,
    });
    _chatSubscriptions[sessionId] = SessionSubscription(sessionId);

    return _messageController.stream.where((m) {
      // Unflattened messages already have session_id promoted to top level
      final type = m['type'] as String?;
      final sid = m['session_id'] as String?;
      return type == 'chat_message' && sid == sessionId;
    });
  }

  /// Subscribe to job queue updates via WebSocket.
  ///
  /// Returns a stream emitting [Map] entries for all job_update messages.
  Stream<Map<String, dynamic>> subscribeToJobs() {
    if (!_jobsSubscribed) {
      send({'type': 'subscribe', 'channel': 'jobs'});
      _jobsSubscribed = true;
    }
    return _messageController.stream.where((m) {
      return m['type'] == 'job_update';
    });
  }

  /// Subscribe to metrics updates via WebSocket.
  ///
  /// Returns a stream emitting [Map] entries for all metrics_update messages.
  Stream<Map<String, dynamic>> subscribeToMetrics() {
    if (!_metricsSubscribed) {
      send({'type': 'subscribe', 'channel': 'metrics'});
      _metricsSubscribed = true;
    }
    return _messageController.stream.where((m) {
      return m['type'] == 'metrics_update';
    });
  }

  /// Dispose all resources
  void dispose() {
    disconnect();
  }
}

/// Tracks active per-session chat subscriptions.
/// Used to ensure only relevant messages are forwarded to a session.
class SessionSubscription {
  final String sessionId;
  const SessionSubscription(this.sessionId);
}
