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

/// WebSocket service for real-time updates
///
/// Handles the Go backend's `{type, data}` message format by flattening
/// nested `data` fields onto the top-level map so consumers can access
/// `message['session_id']`, `message['job_id']`, etc. directly.
///
/// Uses rxdart [BehaviorSubject] and [PublishSubject] for stream management,
/// and rxdart [Rx.retryWhen] for automatic exponential-backoff
/// reconnection (1 s base, doubles each attempt, 30 s cap).
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
  StreamSubscription? _reconnectSubscription;

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

  /// Whether the subjects have been closed (i.e. disconnect() was called).
  bool get _isDisposed => _connectionSubject.isClosed;

  /// Connect to WebSocket.
  ///
  /// Uses rxdart's [Rx.retryWhen] to automatically reconnect with exponential
  /// backoff (1 s base, doubles each attempt, 30 s cap).  On each
  /// successful connection the backoff resets to 1 s.
  Future<void> connect({String? path}) async {
    _wasExplicitlyDisconnected = false;
    if (_isDisposed || isConnected || _isConnecting) return;
    _isConnecting = true;

    // Cancel any previous reconnect subscription before starting fresh.
    _reconnectSubscription?.cancel();
    _reconnectSubscription = null;

    // Capture path for the factory closure.
    final wsPath = path ?? '/ws';

    // Use rxdart Rx.retryWhen for automatic reconnection with exponential
    // backoff.  The stream factory is called on the first attempt and on
    // every retry.  Starts at 1 s, doubles each failure, caps at 30 s,
    // resets on success.
    _reconnectSubscription = Rx.retryWhen<void>(
      () => _openConnection(wsPath).asStream(),
      (Object error, StackTrace stackTrace) {
        if (_wasExplicitlyDisconnected || _isDisposed) {
          // Returning an error stream stops retryWhen permanently.
          return Stream<void>.error(error, stackTrace);
        }
        _errorSubject.addSafe('Reconnecting: $error');
        // Exponential backoff: 1, 2, 4, 8, 16, 30, 30, ...
        final delay = _nextReconnectDelay();
        return TimerStream<void>(null, delay);
      },
    ).listen(
      null,
      onError: (Object e) {
        _errorSubject.addSafe('Connection error: $e');
        _isConnecting = false;
      },
      onDone: () {
        _isConnecting = false;
      },
    );
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
  /// Completes successfully when the connection is established and the
  /// first message is received.  Throws on connection failure or if
  /// explicitly disconnected during the handshake.
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
      if (!kIsWeb && _apiKey != null && _apiKey!.isNotEmpty) {
        final ws = await io.WebSocket.connect(
          uri.toString(),
          headers: {'Authorization': 'Bearer $_apiKey'},
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
        if (_apiKey != null && _apiKey!.isNotEmpty) {
          webUri = uri.replace(
              queryParameters: {...uri.queryParameters, 'token': _apiKey!});
        }
        _channel = WebSocketChannel.connect(webUri);
      }

      // Listen to the raw stream.
      final done = Completer<void>();
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
              // Complete the singleConnect future so retryWhen knows
              // the attempt succeeded and resets its backoff.
              if (!done.isCompleted) done.complete();
            }
          } catch (e) {
            _errorSubject.addSafe('Failed to parse message: $e');
          }
        },
        onError: (error) {
          _connectionSubject.add(false);
          _errorSubject.addSafe('WebSocket error: $error');
          _cleanupChannel();
          if (!done.isCompleted) done.completeError(error);
        },
        onDone: () {
          _connectionSubject.add(false);
          _cleanupChannel();
          if (!done.isCompleted) {
            done.completeError(StateError('WebSocket closed'));
          }
        },
      );

      // Wait for the connection to be confirmed by the first message, or
      // for an error / done event.  A timeout prevents hanging forever if
      // the server accepts the socket but never sends data.
      await done.future.timeout(
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
    _reconnectSubscription?.cancel();
    _reconnectSubscription = null;
    _cleanupChannel();
    _retryCount = 0;
    _connectionSubject.add(false);
  }

  /// Disconnect from WebSocket and dispose resources.
  void disconnect() {
    if (_disposed) return;
    _disposed = true;
    _wasExplicitlyDisconnected = true;
    _reconnectSubscription?.cancel();
    _reconnectSubscription = null;
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
          // Let retryWhen handle the reconnection if still active.
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

  /// Create an HttpClient that accepts self-signed TLS certificates
  /// for localhost connections.
  /// TODO: pin the specific certificate fingerprint instead of blanket
  /// hostname acceptance. Accepting any self-signed cert for localhost
  /// exposes the connection to active local MITM attacks.
  io.HttpClient _createHttpClient() {
    final client = io.HttpClient();
    client.badCertificateCallback =
        (io.X509Certificate cert, String host, int port) =>
            host == 'localhost' || host == '127.0.0.1' || host == '::1';
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
