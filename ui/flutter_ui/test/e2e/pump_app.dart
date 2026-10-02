// E2E bootstrap: pump the REAL provider tree (real ChatProvider — no
// service mocks) against the StubDaemon's baseUrl.
//
// The provider graph is untouched production code; the test overrides only
// the two DI seams the app itself wires (sdkClientProvider /
// websocketProvider) plus ttsProvider (a Flutter platform-channel service,
// unavailable under the VM test binding — the existing mock suite does the
// same). Riverpod's ProviderContainer override mechanism IS the injection
// seam: no production base-url parameters were added.
//
// DO NOT import from production code — test/e2e only.
import 'package:flutter/material.dart' show SizedBox;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/providers.dart' as app;
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import 'stub_daemon.dart';

/// No-op TTS: TtsNotifier is constructed with a real TtsService whose
/// Flutter terminal/TTS channel does not exist under the test binding.
/// Swallowing every method keeps the terminal path real-but-silent
/// (the same idiom the mock suite uses in test/providers/).
class _NoopTtsNotifier extends StateNotifier<TtsState> implements TtsNotifier {
  _NoopTtsNotifier() : super(TtsState.idle);
  @override
  Future<bool> initialize() async => true;
  @override
  Future<void> speak(String text) async {}
  @override
  Future<void> stop() async {}
  @override
  Future<void> setVolume(double volume) async {}
  @override
  Future<void> setSpeed(double speed) async {}
  @override
  Future<void> setPitch(double pitch) async {}
  @override
  Future<void> setVoice(String voiceName) async {}
  @override
  Future<List<Map<String, dynamic>>> getVoices() async => [];
  @override
  Future<void> setEnabled(bool value) async {}
  @override
  Future<void> setBehaviorSettings({
    required bool interrupt,
    required bool queue,
    int? maxQueueSize,
  }) async {}
  @override
  Future<void> toggleTts() async {}
  @override
  bool get enabled => false;
  @override
  bool get isAvailable => false;
  @override
  bool get isSpeaking => false;
  double get volume => 1.0;
}

/// Overrides for the app provider graph that re-point every real client at
/// the stub daemon's http://127.0.0.1:<port> base URL.
List<Override> stubOverrides(StubDaemon daemon) {
  // Parse the stub's http:// base URL apart: the production defaults are
  // https/wss (real daemon serves TLS); the e2e stub is a plain HTTP/WS
  // loopback listener, so the real client classes are constructed with
  // scheme 'http'/'ws'. Everything else (Dio wiring, auth header, WS
  // reconnect loop, event classification) is the unmodified production
  // code path.
  final uri = Uri.parse(daemon.baseUrl);
  final host = uri.host;
  final port = uri.port;

  return [
    app.sdkClientProvider.overrideWith(
      (ref) => SdkApiClient(
        host: host,
        port: port,
        apiKey: 'e2e-stub-key',
        scheme: 'http',
      ),
    ),
    app.websocketProvider.overrideWith(
      (ref) => WebSocketService(
        host: host,
        port: port,
        apiKey: 'e2e-stub-key',
        scheme: 'ws',
      ),
    ),
    app.ttsProvider.overrideWith((ref) => _NoopTtsNotifier()),
  ];
}

/// Pump a minimal widget inside a ProviderScope wired with the REAL
/// provider graph (real [chatProvider] family entry for [sessionId]) whose
/// HTTP/WS clients point at [daemon].
///
/// Watching the real chatProvider here is deliberate: it constructs the real
/// ChatNotifier, which starts its WebSocket connect loop and auto-loads
/// message history from the stub — the same surface a real session sees.
///
/// Returns the [ProviderContainer] for reads/writes from the test.
(ProviderContainer, StubDaemon) pumpRealApp(
  WidgetTester tester, {
  required StubDaemon daemon,
  String sessionId = 'e2e-session',
}) {
  final container = ProviderContainer(overrides: stubOverrides(daemon));
  tester.pumpWidget(
    UncontrolledProviderScope(container: container, child: const SizedBox()),
  );
  // Construct the real provider family entry (real ChatNotifier).
  container.read(chatProvider(sessionId).notifier);
  return (container, daemon);
}
