// M6 regression pin (bughunt 2026-11-22): the GUI verbosity filter must go
// live mid-session. Before the fix, ChatNotifier read _verbosityLevel only
// at construction (plus a best-effort client.json5 fetch), while Ctrl+V /
// the settings dropdown cycled verbosityProvider and PATCHed config. The
// status label changed but progress tier filtering kept the stale level
// until session swap / restart — unlike the TUI, where app.go:1109-1113
// applies verbosity immediately.
//
// This test drives the REAL chatProvider family inside a ProviderContainer
// so its `ref.listen(verbosityProvider)` wiring runs. It toggles the
// verbosity provider (the production toggle path) and asserts a previously
// filtered tier-2 frame is admitted WITHOUT recreating the notifier.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

import '../mocks/mock_websocket_service.dart';

/// SDK stub: no config persistence needed here; verbosity is driven by the
/// provider (the same object the Ctrl+V handler cycles). getMessagesPage is
/// stubbed so the real chatProvider's constructor fetch stays hermetic.
class _VerbosityStubSdk extends SdkApiClient {
  _VerbosityStubSdk() : super(host: 'localhost', port: 8081);

  @override
  Future<String> getClientConfig() async =>
      '{"chat": {"liveness_timeout_seconds": 120}}';

  @override
  Future<void> setClientConfig(Map<String, dynamic> patch) async {}

  @override
  Future<({List<Map<String, dynamic>> messages, int total})> getMessagesPage(
    String id, {
    int offset = 0,
    int limit = 200,
    bool includeToolCalls = false,
  }) async => (messages: <Map<String, dynamic>>[], total: 0);
}

class _ConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

class _NoopTts extends StateNotifier<TtsState> implements TtsNotifier {
  _NoopTts() : super(TtsState.idle);
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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'toggling verbosityProvider admits a previously-filtered progress frame '
    'on the SAME notifier instance (M6)',
    (tester) async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWith((ref) => _VerbosityStubSdk()),
          websocketProvider.overrideWith((ref) => _ConnectedWebSocket()),
          ttsProvider.overrideWith((ref) => _NoopTts()),
        ],
      );
      addTearDown(container.dispose);

      // Force construction so the provider's ref.listen is registered.
      final notifier = container.read(
        chatProvider('live-verbosity-session').notifier,
      );
      // Let the constructor's best-effort config fetch settle before
      // toggling, so the toggle is unambiguously the cause of any change.
      await tester.pump(const Duration(milliseconds: 10));

      final tierTwoFrame = <String, dynamic>{
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'tool started',
        'tier': VerbosityLevel.verbose,
        'session_id': 'live-verbosity-session',
      };

      // Default level is normal (1): the tier-2 frame is filtered out.
      notifier.debugHandleProgressFrame(tierTwoFrame);
      expect(
        notifier.state.currentProgress,
        isNull,
        reason: 'tier-2 frame must be filtered at normal verbosity',
      );

      // No notifier recreation: same instance, verbosity cycled through
      // the provider exactly as Ctrl+V does (verbosityProvider.notifier.cycle()
      // in home_screen.dart / sidebar_home_screen.dart). normal(1) -> verbose(2).
      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), VerbosityLevel.verbose);
      await tester.pump();

      notifier.debugHandleProgressFrame(tierTwoFrame);
      expect(
        notifier.state.currentProgress,
        isNotNull,
        reason:
            'tier-2 frame must be admitted after the live toggle, on the '
            'SAME notifier (no session swap / recreation)',
      );
      expect(notifier.state.currentProgress!.message, 'tool started');
    },
  );
}
