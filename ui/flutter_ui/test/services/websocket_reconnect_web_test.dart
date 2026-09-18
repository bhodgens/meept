import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/websocket_service.dart';

/// Week bughunt F22 regression pin.
///
/// BUG: `_connectWithRetry`'s catch block called
/// `_diagnoseConnectFailure`, which constructed a `dart:io` HttpClient
/// OUTSIDE its try block and without a kIsWeb guard. On Flutter Web the
/// constructor throws synchronously (io.HttpClient does not exist on the
/// web platform), and that error escaped the reconnect loop's catch block —
/// aborting `_connectWithRetry` before its backoff/delay ran. A failed web
/// connect therefore never retried.
///
/// FIX (two layers):
///   1. `_diagnoseConnectFailure` returns null early when kIsWeb;
///   2. the loop additionally wraps the diagnosis call in its own
///      try/catch, so NO diagnoser failure can ever abort the retry cycle.
///
/// PIN (deterministic on every platform, no browser): inject a diagnoser
/// that throws exactly like the web platform did, connect to a guaranteed-
/// refused local port, and assert the retry loop survives multiple failure
/// cycles. Before the fix, the diagnosis error terminated the loop (the
/// connect future completed with the error).
///
/// NOTE: deliberately NOT using TestWidgetsFlutterBinding — its HTTP mock
/// turns every socket attempt into an unhandled zone error unrelated to
/// the loop under test. A plain test drives the real (refused) socket.
void main() {
  tearDownAll(() {
    WebSocketService.diagnoseConnectFailureOverride = null;
  });

  test(
    'F22: reconnect loop keeps retrying when the connect-failure '
    'diagnoser throws (web abort regression)',
    () async {
      var diagnoserCalls = 0;
      WebSocketService.diagnoseConnectFailureOverride =
          (int retryCount) async {
        diagnoserCalls++;
        // Throw like io.HttpClient() did on the web platform.
        throw UnsupportedError('Mocked web platform failure');
      };

      final service = WebSocketService(
        host: '127.0.0.1',
        port: 1, // reserved port: connection refused, no listener
      );

      Object? escaped;
      final connectFuture = service.connect().catchError((Object e) {
        escaped = e;
        return;
      });

      // First connect attempt fails -> catch block runs the diagnoser
      // (which throws) -> the loop MUST continue to the backoff anyway.
      await Future<void>.delayed(const Duration(milliseconds: 300));
      expect(
        diagnoserCalls,
        greaterThanOrEqualTo(1),
        reason: 'diagnoser ran for the first failed attempt',
      );

      // Past the 1s backoff: the SECOND attempt fails and the diagnoser
      // runs again — proof the loop is still alive after a throwing
      // diagnosis (the regression ended the loop right here).
      await Future<void>.delayed(const Duration(seconds: 1, milliseconds: 500));
      expect(
        diagnoserCalls,
        greaterThanOrEqualTo(2),
        reason: 'F22: retry loop aborted when the diagnoser threw',
      );
      expect(
        escaped,
        isNull,
        reason: 'F22: diagnoser error escaped the retry loop',
      );

      // Third cycle for good measure (backoff grows: 1s, 2s, 4s — the
      // third attempt may land past this window, so >= 2 is the contract;
      // the point is the loop is STILL alive, not dead).
      await Future<void>.delayed(const Duration(seconds: 2));
      expect(
        diagnoserCalls,
        greaterThanOrEqualTo(2),
        reason: 'F22: loop dead after two cycles',
      );
      expect(escaped, isNull);

      // Tear down: explicit disconnect ends the loop cleanly.
      WebSocketService.diagnoseConnectFailureOverride = null;
      service.disconnect();
      await connectFuture.timeout(const Duration(seconds: 5));
    },
    timeout: const Timeout(Duration(seconds: 30)),
  );
}
