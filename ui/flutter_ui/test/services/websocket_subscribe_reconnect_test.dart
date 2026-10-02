import 'dart:async';
import 'dart:convert';
import 'dart:io' as io;

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Contract gap fix pin: a subscribe issued while disconnected must be
/// delivered after reconnect.
///
/// BUG: `send()` dropped every frame with only an error event when
/// `isConnected` was false, and `_flushPendingSubscriptions` fired once per
/// connect without retrying frames that raced a disconnect. A subscribe sent
/// mid-reconnect was silently lost: the daemon-side session filter never
/// armed after the reconnect and the GUI showed no messages for that
/// session.
///
/// FIX: `send()` now queues frames while disconnected (bounded at 256,
/// oldest dropped); the queue is flushed (in order, through `send` so a
/// mid-flush disconnect re-queues) on every successful reconnect.
///
/// PIN: a real loopback WebSocket server accepts the client, drops the
/// socket, and records every received frame. The client is told to
/// subscribe while DISCONNECTED; after the reconnect the server must have
/// received the subscribe frame.
void main() {
  late RawServer server;

  setUp(() async {
    server = await RawServer.bind();
  });

  tearDown(() async {
    await server.close();
    WebSocketService.diagnoseConnectFailureOverride = null;
  });

  test(
    'subscribe issued while disconnected is delivered after reconnect',
    () async {
      WebSocketService.diagnoseConnectFailureOverride =
          (int retryCount) async => null;

      final service = WebSocketService(
        host: '127.0.0.1',
        port: server.port,
        apiKey: 'test-key',
        scheme: 'ws',
      );

      // First connection: connect() returns before the socket is up, so
      // wait for the connection state, then send a bootstrap subscribe and
      // prove the path delivers frames while connected.
      final firstConn = Completer<void>();
      final stateSub = service.connectionStream.listen((connected) {
        if (connected && !firstConn.isCompleted) firstConn.complete();
      });
      await service.connect();
      await firstConn.future.timeout(const Duration(seconds: 15));
      await server.waitForClient();

      // A connected send flows straight to the socket.
      service.send({
        'type': 'subscribe',
        'channel': 'chat',
        'session_id': 'bootstrap',
      });
      await server.waitForSubscribe('bootstrap');
      expect(service.isConnected, isTrue);

      // Drop the connection -> onDone -> reconnect loop starts.
      await server.dropClient();
      // Wait until the service notices the drop, then (in the disconnected
      // window, possibly mid-reconnect) issue the subscribe.
      final dropped = Completer<void>();
      final reconnected = Completer<void>();
      final sub = service.connectionStream.listen((connected) {
        if (!connected && !dropped.isCompleted) {
          dropped.complete();
        } else if (connected && dropped.isCompleted) {
          reconnected.complete();
        }
      });
      await dropped.future.timeout(const Duration(seconds: 15));

      // THE REGRESSION: subscribe issued while disconnected. Before the
      // fix this frame was dropped on the floor.
      expect(service.isConnected, isFalse);
      service.subscribeToChat('sess-1');

      // The reconnect succeeds (fresh accept) and the queued subscribe is
      // flushed.
      await reconnected.future.timeout(const Duration(seconds: 15));
      await server.waitForSubscribe(
        'sess-1',
        timeout: const Duration(seconds: 15),
      );

      await stateSub.cancel();
      await sub.cancel();
      service.disconnect();
    },
    timeout: const Timeout(Duration(seconds: 45)),
  );

  test(
    'send while connected still delivers immediately (no queue detour)',
    () async {
      final service = WebSocketService(
        host: '127.0.0.1',
        port: server.port,
        apiKey: 'test-key',
        scheme: 'ws',
      );

      await service.connect();
      await server.waitForClient();
      service.send({'type': 'ping', 'marker': 'direct'});
      await server.waitForFrame(
        (f) => f['type'] == 'ping' && f['marker'] == 'direct',
        timeout: const Duration(seconds: 5),
      );
      service.disconnect();
    },
  );
}

/// Minimal single-client WebSocket server that records every received JSON
/// frame. Uses dart:io directly; this file runs only on the VM (flutter
/// test), mirroring websocket_reconnect_web_test.dart's platform stance.
class RawServer {
  RawServer._(this._server, this.port);

  final io.HttpServer _server;
  final int port;

  io.WebSocket? _client;
  final _frames = <Map<String, dynamic>>[];
  final _clientConnected = Completer<void>();
  final _frameWaiters = <_FrameWaiter>[];

  static Future<RawServer> bind() async {
    final server = await io.HttpServer.bind('127.0.0.1', 0);
    final raw = RawServer._(server, server.port);
    server.listen(raw._onRequest, onError: (_) {});
    return raw;
  }

  void _onRequest(io.HttpRequest request) {
    if (io.WebSocketTransformer.isUpgradeRequest(request)) {
      io.WebSocketTransformer.upgrade(request)
          .then((ws) {
            _client = ws;
            if (!_clientConnected.isCompleted) _clientConnected.complete();
            ws.listen(
              (data) {
                try {
                  final frame =
                      jsonDecode(data as String) as Map<String, dynamic>;
                  _frames.add(frame);
                  _checkWaiters();
                } catch (_) {
                  /* ignore malformed */
                }
              },
              onDone: () {
                if (_client == ws) _client = null;
              },
              onError: (_) {
                if (_client == ws) _client = null;
              },
            );
            // Server-initiated welcome like the daemon's status frame.
            ws.add(
              jsonEncode({
                'type': 'status',
                'data': {'connected': true},
              }),
            );
          })
          .catchError((_) {});
    } else {
      request.response.statusCode = io.HttpStatus.notFound;
      request.response.close();
    }
  }

  Future<void> waitForClient() =>
      _clientConnected.future.timeout(const Duration(seconds: 10));

  Future<void> dropClient() async {
    final ws = _client;
    _client = null;
    await ws?.close();
  }

  /// Wait until a subscribe frame for [sessionId] arrives (any time since
  /// server start — queued frames replay after reconnect).
  Future<void> waitForSubscribe(
    String sessionId, {
    Duration timeout = const Duration(seconds: 10),
  }) => waitForFrame(
    (f) =>
        f['type'] == 'subscribe' &&
        f['channel'] == 'chat' &&
        f['session_id'] == sessionId,
    timeout: timeout,
  );

  /// Wait until [test] matches a received frame.
  Future<void> waitForFrame(
    bool Function(Map<String, dynamic>) test, {
    Duration timeout = const Duration(seconds: 10),
  }) {
    for (final f in _frames) {
      if (test(f)) return Future.value();
    }
    final waiter = _FrameWaiter(test);
    _frameWaiters.add(waiter);
    return waiter.completer.future.timeout(
      timeout,
      onTimeout: () {
        _frameWaiters.remove(waiter);
        throw TimeoutException(
          'frame not received; got: ${_frames.map(jsonEncode).toList()}',
        );
      },
    );
  }

  void _checkWaiters() {
    for (final waiter in List<_FrameWaiter>.of(_frameWaiters)) {
      for (final f in _frames) {
        if (!waiter.done && waiter.test(f)) {
          waiter.completer.complete();
          _frameWaiters.remove(waiter);
          break;
        }
      }
    }
  }

  Future<void> close() async {
    await _client?.close();
    await _server.close(force: true);
  }
}

class _FrameWaiter {
  _FrameWaiter(this.test);
  final bool Function(Map<String, dynamic>) test;
  final Completer<void> completer = Completer<void>();
  bool get done => completer.isCompleted;
}
