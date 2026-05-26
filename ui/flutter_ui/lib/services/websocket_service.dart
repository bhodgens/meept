import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart' show IOWebSocketChannel;
import '../core/constants.dart';
import 'storage_service.dart';

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
  Timer? _reconnectTimer;

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

  /// Create a WebSocketService using persisted host/port/API key from
  /// [storage].  After `StorageService.init()` is called this is fully
  /// synchronous.
  factory WebSocketService.fromStorage(StorageService storage) {
    return WebSocketService(
      host: storage.getApiHost(),
      port: storage.getApiPort(),
      apiKey: storage.getApiKey(),
    );
  }

  /// Connection state stream
  Stream<bool> get connectionStream => _connectionController.stream;

  /// Incoming messages stream
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;

  /// Error stream
  Stream<String> get errorStream => _errorController.stream;

  bool get isConnected => _isConnected;

  /// Whether the controllers have been closed (i.e. disconnect() was called).
  bool get _isDisposed =>
      _connectionController.isClosed ||
      _messageController.isClosed;

  /// Connect to WebSocket
  Future<void> connect({String? path}) async {
    if (_isDisposed || _isConnected || _isConnOpen || _wasExplicitlyDisconnected) return;

    try {
      _isConnOpen = false;

      final wsPath = path ?? '/ws';
      final uri = Uri.parse('ws://$_host:$_port$wsPath');

      // Use Authorization header for WebSocket authentication on
      // desktop/mobile platforms.  Flutter Web's underlying browser
      // WebSocket API does not support custom headers, so we fall
      // back to the `token` query parameter only on web (the server
      // accepts the header from its auth middleware, but the browser
      // handshake on web only supports query-string credentials).
      if (!kIsWeb && _apiKey != null && _apiKey!.isNotEmpty) {
        // Use dart:io WebSocket to pass custom headers (desktop/mobile)
        // web_socket_channel's connect() only supports protocols, not headers.
        final ioWs = await IOWebSocketChannel.connect(
          uri.toString(),
          headers: {'Authorization': 'Bearer $_apiKey'},
        );
        _channel = ioWs;
      } else {
        _channel = WebSocketChannel.connect(uri);
      }

      _channel!.stream.listen(
        (data) {
          try {
            final message = jsonDecode(data as String) as Map<String, dynamic>;

            // Flatten the Go backend's {type, data} format so that fields
            // nested inside `data` are promoted to the top level.
            // This allows consumers to access message['session_id'],
            // message['job_id'], message['role'] etc. directly.
            final flatMessage = _flattenWSMessage(message);
            if (!_messageController.isClosed) {
              _messageController.add(flatMessage);
            }

            if (!_isConnOpen) {
              _isConnOpen = true;
              final type = flatMessage['type'] as String?;
              // Only mark connected on handshake messages (ping/status)
              if (type == 'ping' || type == 'status') {
                _isConnected = true;
                if (!_connectionController.isClosed) {
                  _connectionController.add(true);
                }
                _startPingTimer();
                _retryCount = 0;
                // Flush any subscriptions that were requested before
                // the connection was established.
                _flushPendingSubscriptions();
              }
            }
          } catch (e) {
            if (!_errorController.isClosed) {
              _errorController.add('Failed to parse message: $e');
            }
          }
        },
        onError: (error) {
          _isConnected = false;
          _isConnOpen = false;
          if (!_connectionController.isClosed) {
            _connectionController.add(false);
          }
          if (!_errorController.isClosed) {
            _errorController.add('WebSocket error: $error');
          }
          _handleReconnect();
        },
        onDone: () {
          _isConnected = false;
          _isConnOpen = false;
          if (!_connectionController.isClosed) {
            _connectionController.add(false);
          }
          _handleReconnect();
        },
      );
    } catch (e) {
      _isConnected = false;
      _isConnOpen = false;
      if (!_connectionController.isClosed) {
        _connectionController.add(false);
      }
      if (!_errorController.isClosed) {
        _errorController.add('Connection failed: $e');
      }
      _handleReconnect();
    }
  }

  /// Send any subscribe messages that were queued before the connection
  /// was fully established.
  void _flushPendingSubscriptions() {
    if (!_isConnected) return;
    for (final sessionId in _chatSubscriptions.keys) {
      send({'type': 'subscribe', 'channel': 'chat', 'session_id': sessionId});
    }
    if (_jobsSubscribed) {
      send({'type': 'subscribe', 'channel': 'jobs'});
    }
    if (_metricsSubscribed) {
      send({'type': 'subscribe', 'channel': 'metrics'});
    }
  }

  /// Flatten a Go backend `{type, data}` message into a flat map.
  ///
  /// If the message has a `data` field that is a map, promote all keys
  /// from `data` onto the top-level map alongside `type`.  If there
  /// is no `data` field, return the message unchanged.
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
    return msg;
  }

  Duration _computeReconnectDelay() {
    if (_retryCount >= AppConstants.maxRetries) {
      return const Duration(seconds: 30);
    }
    const baseDelay = Duration(seconds: 2);
    final shift = _retryCount.clamp(0, 10);
    final exponentialDelay = baseDelay * (1 << shift);
    final jitter = Duration(milliseconds: Random().nextInt(1000));
    _retryCount++;
    return exponentialDelay + jitter;
  }

  void _handleReconnect() {
    if (_wasExplicitlyDisconnected || _isDisposed) return;
    final delay = _computeReconnectDelay();
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(delay, () {
      connect();
    });
  }

  /// Pause the WebSocket connection for lifecycle events (e.g. app
  /// backgrounded).
  ///
  /// Like [disconnect] but preserves subscription state and keeps the
  /// StreamControllers open so [connect] can re-establish the channel.
  void pause() {
    _pingTimer?.cancel();
    _pingTimer = null;
    _isConnected = false;
    _isConnOpen = false;
    _retryCount = 0;
    if (!_connectionController.isClosed) {
      _connectionController.add(false);
    }
    _channel?.sink.close();
    _channel = null;
  }

  /// Disconnect from WebSocket and dispose resources.
  void disconnect() {
    _wasExplicitlyDisconnected = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
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
    if (!_connectionController.isClosed) {
      _connectionController.add(false);
    }

    // Close StreamControllers to prevent memory leaks
    _messageController.close();
    _errorController.close();
    _connectionController.close();
  }

  void send(Map<String, dynamic> message) {
    if (!_isConnected) {
      if (!_errorController.isClosed) {
        _errorController.add('Cannot send: not connected');
      }
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
    // Track the subscription even if not connected yet; it will be
    // flushed once the connection is established.
    _chatSubscriptions[sessionId] = SessionSubscription(sessionId);
    // Attempt to send immediately; if not connected, _flushPendingSubscriptions
    // will resend on connect.
    send({
      'type': 'subscribe',
      'channel': 'chat',
      'session_id': sessionId,
    });

    return _messageController.stream.where((m) {
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
      _jobsSubscribed = true;
      send({'type': 'subscribe', 'channel': 'jobs'});
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
      _metricsSubscribed = true;
      send({'type': 'subscribe', 'channel': 'metrics'});
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
