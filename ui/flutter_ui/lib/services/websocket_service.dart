import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;
import 'dart:math';
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb, kReleaseMode;
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

  /// Completer for the current connection's stream-done signal.
  ///
  /// When `_cleanupChannel` is called from external paths (pause, pong
  /// timeout), this completer is completed explicitly so that
  /// `_openConnection`'s `await streamDone.future` unblocks and the
  /// `_connectWithRetry` loop can proceed.  Without this, calling
  /// `_cleanupChannel` before the sink closes prevents `onDone` from
  /// firing, permanently blocking reconnection.
  Completer<void>? _streamDone;

  // Channel subscription tracking
  final Map<String, SessionSubscription> _chatSubscriptions = {};
  final Map<String, SessionSubscription> _progressSubscriptions = {};
  bool _jobsSubscribed = false;
  bool _metricsSubscribed = false;

  bool _disposed = false;
  int _retryCount = 0;
  Timer? _pongTimeoutTimer;
  final _random = Random();

  /// Timestamp when the connection was last established (null when disconnected).
  DateTime? _connectedAt;

  /// Whether the current WebSocket connection uses TLS (always true in production).
  bool get usesTls => true;

  /// The host this service is connected to.
  String get host => _host;

  /// The port this service is connected to.
  int get port => _port;

  /// When the connection was established, or null if disconnected.
  DateTime? get connectedAt => _connectedAt;

  /// Format connection duration as a human-readable string.
  String get connectionDuration {
    final since = _connectedAt;
    if (since == null) return '—';
    final diff = DateTime.now().difference(since);
    final h = diff.inHours;
    final m = diff.inMinutes % 60;
    final s = diff.inSeconds % 60;
    if (h > 0) return '$h h $m m';
    if (m > 0) return '$m m $s s';
    return '$s s';
  }

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
    final stored = storage.getApiKey();
    // Resolve the effective API key:
    //  - If the user configured one in Settings, use it.
    //  - Else, in debug builds, fall back to `AppConstants.defaultApiKey`
    //    (populated via --dart-define=MEEPT_DEV_API_KEY=...).
    //  - In release builds, allow null/empty API key — the app will start
    //    and show a "configure API key" prompt in the UI. Connection attempts
    //    will fail gracefully until a real key is configured.
    final apiKey = (stored != null && stored.isNotEmpty)
        ? stored
        : AppConstants.defaultApiKey;
    if (apiKey.isEmpty) {
      // Do not throw — allow the app to start. Connection will fail later,
      // and the UI can show a "configure API key" prompt.
      debugPrint('[warn] No API key configured — will connect without auth. '
          'Configure a real key in Settings for production.');
    } else if (!kReleaseMode) {
      debugPrint('[warn] Using default dev API key — configure a real key for production');
    }
    return WebSocketService(
      host: storage.getApiHost(),
      port: storage.getApiPort(),
      apiKey: apiKey.isEmpty ? null : apiKey,
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


  /// Whether currently attempting to connect or reconnect.
  bool get isConnecting => _isConnecting;
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
  ///
  /// Circuit-breaker: after 5+ consecutive failures, retries occur every
  /// ~30 seconds (with jitter). On HTTP 401 errors, shows a helpful message
  /// prompting the user to configure an API key in Settings.
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
          // _isConnecting was set to false in _openConnection on success.
          if (_disposed || _wasExplicitlyDisconnected) return;
          _errorSubject.addSafe('Connection closed, reconnecting...');
          final delay = _nextReconnectDelay();
          await Future<void>.delayed(delay);
        } catch (e) {
          if (_disposed || _wasExplicitlyDisconnected) return;

          // Check if this is an HTTP 401 (unauthorized) error
          final is401 = e is io.WebSocketException &&
              e.toString().contains('401');
          if (is401) {
            _errorSubject.addSafe(
                'Authentication failed (401). Configure API key in Settings.');
          } else {
            _errorSubject.addSafe('Reconnecting: $e');
          }

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
    final jitter = Duration(milliseconds: _random.nextInt(1000));
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
      // Use custom HTTP client with certificate pinning for all non-web
      // connections. This ensures self-signed certs are accepted for localhost.
      if (!kIsWeb) {
        final ws = await io.WebSocket.connect(
          uri.toString(),
          headers: {
            if (_apiKey != null && _apiKey.isNotEmpty)
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
        // Web platform: use token query parameter (browser WebSocket API
        // doesn't support custom headers)
        var webUri = uri;
        if (_apiKey != null && _apiKey.isNotEmpty) {
          webUri = uri.replace(
              queryParameters: {...uri.queryParameters, 'token': _apiKey});
        }
        _channel = WebSocketChannel.connect(webUri);
      }

      // Completer that resolves once the connection is confirmed.
      // We mark as connected immediately when the socket opens (not waiting
      // for first message) so the UI shows "connected" right away.
      final ready = Completer<void>();
      _streamDone = Completer<void>();
      final streamDone = _streamDone!;

      // Mark as connected as soon as the socket is established
      // This fixes the "connecting..." stuck status issue
      if (!isConnected) {
        _isConnecting = false; // Stop showing "connecting..." once we're connected
        _connectedAt = DateTime.now();
        _connectionSubject.add(true);
        _startPingTimer();
        _retryCount = 0;
        _flushPendingSubscriptions();
        if (!ready.isCompleted) ready.complete();
      }

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
            _isConnecting = false;
            _connectedAt = DateTime.now();
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
  ///
  /// Completes [_streamDone] explicitly so that any caller awaiting the
  /// stream-done future in [_openConnection] unblocks, even if the
  /// `onDone` callback was prevented from firing (which happens when the
  /// subscription is cancelled before the sink closes).
  void _cleanupChannel() {
    _wsSubscription?.cancel();
    _wsSubscription = null;
    _pingTimer?.cancel();
    _pingTimer = null;
    _pongTimeoutTimer?.cancel();
    _pongTimeoutTimer = null;
    _channel?.sink.close();
    _channel = null;
    _connectedAt = null;
    // Explicitly complete streamDone for callers like pause() and pong
    // timeout that bypass the natural onDone path.
    if (_streamDone != null && !_streamDone!.isCompleted) {
      _streamDone!.complete();
    }
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

  /// Session-scoped agent progress subscription.
  ///
  /// Returns a stream that emits only [Map] entries matching the given
  /// [sessionId] and having type `agent_progress`.  The caller is
  /// responsible for managing the server-side subscription (typically
  /// by sending a `subscribe` message with `channel: 'progress'`).
  Stream<Map<String, dynamic>> subscribeToAgentProgress(String sessionId) {
    // Track the subscription so it can be flushed once connected.
    _progressSubscriptions[sessionId] = SessionSubscription(sessionId);
    if (isConnected) {
      send({
        'type': 'subscribe',
        'channel': 'progress',
        'session_id': sessionId,
      });
    }

    return _messageSubject.stream.where((m) {
      final type = m['type'] as String?;
      final sid = m['session_id'] as String?;
      return type == 'agent_progress' && sid == sessionId;
    });
  }

  /// Unsubscribe from agent progress updates for a session.
  void unsubscribeFromAgentProgress(String sessionId) {
    _progressSubscriptions.remove(sessionId);
    if (isConnected) {
      send({
        'type': 'unsubscribe',
        'channel': 'progress',
        'session_id': sessionId,
      });
    }
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
