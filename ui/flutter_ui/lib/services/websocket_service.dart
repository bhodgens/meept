import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;
import 'dart:math';
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;
import 'package:rxdart/rxdart.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart' show IOWebSocketChannel;
import '../core/constants.dart';
import 'storage_service.dart';
import 'daemon_cert_pinner.dart';

/// WebSocket service for real-time updates
///
/// Handles the Go backend's `{type, data}` message format by flattening
/// nested `data` fields onto the top-level map so consumers can access
/// `message['session_id']`, `message['job_id']`, etc. directly.
///
/// Uses rxdart [BehaviorSubject] and [PublishSubject] for stream management,
/// and a manual reconnect loop with exponential backoff (1 s base, doubles
/// each attempt, 30 s cap).
class WebSocketService {
  WebSocketChannel? _channel;
  final String _host;
  final int _port;
  final String? _apiKey;

  // rxdart subjects replace manual StreamControllers
  final PublishSubject<Map<String, dynamic>> _messageSubject =
      PublishSubject<Map<String, dynamic>>();
  final PublishSubject<String> _errorSubject = PublishSubject<String>();
  final BehaviorSubject<bool> _connectionSubject =
      BehaviorSubject<bool>.seeded(false);

  bool _isConnecting = false;
  bool _wasExplicitlyDisconnected = false;
  Timer? _pingTimer;
  StreamSubscription? _wsSubscription;

  // Channel subscription tracking
  final Map<String, SessionSubscription> _chatSubscriptions = {};
  bool _jobsSubscribed = false;
  bool _metricsSubscribed = false;

  bool _disposed = false;
  int _retryCount = 0;
  Timer? _pongTimeoutTimer;

  WebSocketService({
    String? host,
    int? port,
    String? apiKey,
  })  : _host = host ?? AppConstants.defaultApiHost,
        _port = port ?? AppConstants.defaultApiPort,
        _apiKey = apiKey;

  /// Create a WebSocketService using persisted host/port/API key from
  /// [storage].
  ///
  /// Note: Storage must be initialized before calling this.
  factory WebSocketService.fromStorage(StorageService storage) {
    return WebSocketService(
      host: storage.getApiHost(),
      port: storage.getApiPort(),
      apiKey: storage.getApiKey(), // Sync read from SharedPreferences
    );
  }

  /// Connection state stream (BehaviorSubject replays last value on listen).
  Stream<bool> get connectionStream => _connectionSubject.stream;

  /// Incoming messages stream (PublishSubject — no replay).
  Stream<Map<String, dynamic>> get messageStream => _messageSubject.stream;

  /// Error stream (PublishSubject — no replay).
  Stream<String> get errorStream => _errorSubject.stream;

  bool get isConnected => _connectionSubject.value;

  /// Whether disconnect() was called (subjects closed, no reconnect).
  bool get _isDisposed => _disposed;

  /// Connect to WebSocket.
  ///
  /// Uses a manual reconnect loop with exponential backoff (1 s base,
  /// doubles each failure, 30 s cap, resets on success).  On each
  /// successful connection the backoff resets to 1 s.  When the
  /// WebSocket closes, the `onDone` handler triggers a new reconnection
  /// attempt automatically.
  Future<void> connect({String? path}) async {
    _wasExplicitlyDisconnected = false;
    if (_isDisposed || isConnected || _isConnecting) return;
    _isConnecting = true;

    // Capture path for the connection loop.
    final wsPath = path ?? '/ws';

    // Kick off the manual reconnect loop.  _connectWithRetry runs
    // asynchronously and handles its own retry scheduling.
    _connectWithRetry(wsPath);
  }

  /// Manual reconnect loop: try [wsPath], on failure wait with exponential
  /// backoff and retry.  Exits only when [_disposed] or
  /// [_wasExplicitlyDisconnected] is set.
  Future<void> _connectWithRetry(String wsPath) async {
    // try/finally ensures _isConnecting is reset on EVERY exit path,
    // including the early `return` statements when pause()/disconnect()
    // is called mid-loop. Without this, the next connect() call would
    // see _isConnecting stuck true and refuse to reconnect.
    try {
      while (!_disposed && !_wasExplicitlyDisconnected) {
        try {
          await _openConnection(wsPath);
          // Connection succeeded and the WebSocket stream ended (onDone).
          // Reset retry count (already done in _openConnection on first
          // message) and loop back to reconnect.
          if (_disposed || _wasExplicitlyDisconnected) return;
          _errorSubject.addSafe('Connection closed, reconnecting...');
          final delay = _nextReconnectDelay();
          await Future<void>.delayed(delay);
        } catch (e) {
          if (_disposed || _wasExplicitlyDisconnected) return;
          _errorSubject.addSafe('Reconnecting: $e');
          final delay = _nextReconnectDelay();
          await Future<void>.delayed(delay);
        }
      }
    } finally {
      _isConnecting = false;
    }
  }

  /// Send any subscribe messages that were queued before the connection
  /// was fully established.
  void _flushPendingSubscriptions() {
    if (!isConnected) return;
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

  /// Exponential backoff delay: 1 s, 2 s, 4 s, 8 s, 16 s, 30 s, 30 s, ...
  ///
  /// Jitter is added to avoid thundering-herd effects.
  Duration _nextReconnectDelay() {
    const baseDelay = Duration(seconds: 1);
    const maxDelay = Duration(seconds: 30);
    final shift = _retryCount.clamp(0, 10);
    final exponentialDelay = baseDelay * (1 << shift);
    final capped = exponentialDelay > maxDelay ? maxDelay : exponentialDelay;
    final jitter = Duration(milliseconds: Random().nextInt(1000));
    _retryCount++;
    return capped + jitter;
  }

  /// Open a single WebSocket connection (no retry logic).
  ///
  /// Establishes the WebSocket, starts listening for messages, and
  /// returns a [Future] that completes when the connection is confirmed
  /// (first message received) and the stream subsequently ends (onDone).
  /// Throws on connection failure or if explicitly disconnected during
  /// the handshake.  The caller ([_connectWithRetry]) is responsible for
  /// re-invoking this method after the future completes.
  Future<void> _openConnection(String wsPath) async {
    if (_isDisposed || _wasExplicitlyDisconnected) {
      throw StateError('Service disposed');
    }

    try {
      final uri = Uri.parse('wss://$_host:$_port$wsPath');

      // Use Authorization header for WebSocket authentication on
      // desktop/mobile platforms.  Flutter Web's underlying browser
      // WebSocket API does not support custom headers, so we fall
      // back to the `token` query parameter only on web.
      if (!kIsWeb && _apiKey != null && _apiKey.isNotEmpty) {
        final ws = await io.WebSocket.connect(
          uri.toString(),
          headers: {
            'Authorization': 'Bearer $_apiKey',
            'Origin': 'http://localhost:$_port',
          },
          customClient: _createHttpClient(),
        );
        final channel = IOWebSocketChannel(ws);
        if (_isDisposed || _wasExplicitlyDisconnected) {
          await channel.sink.close();
          throw StateError('Service disposed');
        }
        _channel = channel;
      } else {
        var webUri = uri;
        if (_apiKey != null && _apiKey.isNotEmpty) {
          webUri = uri.replace(
              queryParameters: {...uri.queryParameters, 'token': _apiKey});
        }
        _channel = WebSocketChannel.connect(webUri);
      }

      // Completer that resolves once the connection is confirmed (first
      // message received).  After that the stream listener continues
      // running until onDone fires, which completes the returned future.
      final ready = Completer<void>();
      final streamDone = Completer<void>();

      _wsSubscription = _channel!.stream.listen(
        (data) {
          try {
            final message =
                jsonDecode(data as String) as Map<String, dynamic>;
            final flatMessage = _flattenWSMessage(message);
            final type = flatMessage['type'] as String?;

            if (type == 'error') {
              _errorSubject.addSafe(
                  flatMessage['message'] ?? 'Server error');
            }

            if (type == 'pong') {
              _pongTimeoutTimer?.cancel();
              _pongTimeoutTimer = null;
            }

            if (type == 'subscribed') {
              debugPrint('WebSocket subscribed: ${flatMessage['channel']}');
            }

            _messageSubject.addSafe(flatMessage);

            // First message means the connection is live.
            if (!isConnected) {
              _connectionSubject.add(true);
              _startPingTimer();
              _retryCount = 0;
              _flushPendingSubscriptions();
              if (!ready.isCompleted) ready.complete();
            }
          } catch (e) {
            _errorSubject.addSafe('Failed to parse message: $e');
          }
        },
        onError: (error) {
          _connectionSubject.add(false);
          _cleanupChannel();
          if (!ready.isCompleted) ready.completeError(error);
          if (!streamDone.isCompleted) streamDone.completeError(error);
        },
        onDone: () {
          _connectionSubject.add(false);
          _cleanupChannel();
          // If we never received a first message, signal the handshake
          // as failed so _connectWithRetry retries.
          if (!ready.isCompleted) {
            ready.completeError(StateError('WebSocket closed before ready'));
          }
          // Signal the stream is done so _connectWithRetry re-enters
          // its loop and reconnects.
          if (!streamDone.isCompleted) streamDone.complete();
        },
      );

      // Wait for the connection to be confirmed by the first message, or
      // for an error during the handshake.  A timeout prevents hanging
      // forever if the server accepts the socket but never sends data.
      await ready.future.timeout(
        const Duration(seconds: 30),
        onTimeout: () {
          // Timeout is not fatal — the connection may still be usable.
          // Mark as connected so the caller proceeds.
          if (!isConnected) {
            _connectionSubject.add(true);
            _startPingTimer();
            _retryCount = 0;
            _flushPendingSubscriptions();
          }
        },
      );

      // Now wait for the stream to end (onDone).  This blocks
      // _connectWithRetry until the connection drops, at which point
      // it will loop back and reconnect.
      await streamDone.future;
    } catch (e) {
      _connectionSubject.add(false);
      _cleanupChannel();
      _errorSubject.addSafe('Connection failed: $e');
      rethrow;
    }
  }

  /// Tear down the current channel and subscription without disposing
  /// the subjects.  Used between reconnect attempts.
  void _cleanupChannel() {
    _wsSubscription?.cancel();
    _wsSubscription = null;
    _pingTimer?.cancel();
    _pingTimer = null;
    _pongTimeoutTimer?.cancel();
    _pongTimeoutTimer = null;
    _channel?.sink.close();
    _channel = null;
  }

  /// Pause the WebSocket connection for lifecycle events (e.g. app
  /// backgrounded).
  ///
  /// Like [disconnect] but preserves subscription state and keeps the
  /// rxdart subjects open so [connect] can re-establish the channel.
  void pause() {
    _wasExplicitlyDisconnected = true;
    _cleanupChannel();
    _retryCount = 0;
    _connectionSubject.add(false);
  }

  /// Disconnect from WebSocket and dispose resources.
  void disconnect() {
    if (_disposed) return;
    _disposed = true;
    _wasExplicitlyDisconnected = true;
    _cleanupChannel();
    _chatSubscriptions.clear();
    _jobsSubscribed = false;
    _metricsSubscribed = false;
    _retryCount = 0;
    _connectionSubject.add(false);

    // Close rxdart subjects to prevent memory leaks.
    _messageSubject.close();
    _errorSubject.close();
    _connectionSubject.close();
  }

  void send(Map<String, dynamic> message) {
    if (!isConnected) {
      _errorSubject.addSafe('Cannot send: not connected');
      return;
    }
    _channel?.sink.add(jsonEncode(message));
  }

  void _startPingTimer() {
    _pingTimer?.cancel();
    _pongTimeoutTimer?.cancel();
    _pingTimer = Timer.periodic(AppConstants.pingInterval, (_) {
      send({'type': 'ping', 'timestamp': DateTime.now().toIso8601String()});
      _pongTimeoutTimer = Timer(const Duration(seconds: 10), () {
        if (isConnected) {
          _connectionSubject.add(false);
          _cleanupChannel();
          // onDone in _openConnection will fire, completing streamDone,
          // which causes _connectWithRetry to reconnect.
        }
      });
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
    if (isConnected) {
      send({
        'type': 'subscribe',
        'channel': 'chat',
        'session_id': sessionId,
      });
    }

    return _messageSubject.stream.where((m) {
      final type = m['type'] as String?;
      final sid = m['session_id'] as String?;
      return type == 'chat_message' && sid == sessionId;
    });
  }

  /// Unsubscribe from a chat session.
  ///
  /// Removes the entry from [_chatSubscriptions] and sends an unsubscribe
  /// message if currently connected.
  void unsubscribeFromChat(String sessionId) {
    _chatSubscriptions.remove(sessionId);
    if (isConnected) {
      send({
        'type': 'unsubscribe',
        'channel': 'chat',
        'session_id': sessionId,
      });
    }
  }

  /// Subscribe to job queue updates via WebSocket.
  ///
  /// Returns a stream emitting [Map] entries for all job_update messages.
  Stream<Map<String, dynamic>> subscribeToJobs() {
    if (!_jobsSubscribed) {
      _jobsSubscribed = true;
      send({'type': 'subscribe', 'channel': 'jobs'});
    }
    return _messageSubject.stream.where((m) {
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
    return _messageSubject.stream.where((m) {
      return m['type'] == 'metrics_update';
    });
  }

  /// Dispose all resources
  void dispose() {
    disconnect();
  }

  /// Create an HttpClient with certificate pinning for the daemon's
  /// self-signed TLS cert.
  io.HttpClient _createHttpClient() {
    final client = io.HttpClient();
    client.badCertificateCallback =
        (io.X509Certificate cert, String host, int port) =>
            DaemonCertPinner.validateCert(cert, host);
    return client;
  }
}

/// Extension to add a value to a [Subject] only when it has not been
/// closed, avoiding the need for isClosed guards at every call site.
extension _SafeAdd<T> on Subject<T> {
  void addSafe(T value) {
    if (!isClosed) add(value);
  }
}

/// Tracks active per-session chat subscriptions.
/// Used to ensure only relevant messages are forwarded to a session.
class SessionSubscription {
  final String sessionId;
  const SessionSubscription(this.sessionId);
}
