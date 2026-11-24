// Leaf 08 (20261121-interactive-usability) GUI gates:
//
// (a) the verbosity tier filter actually filters progress frames by tier
//     (quiet drops tier > 0 frames), mirroring TUI app.go
//     `tier <= a.verbosity`;
// (b) mid-turn task.error events render a transient error line in chat
//     (mirror of TUI app.go task.error handler);
// (c) review_completed rejections/needs-info render feedback in chat.
//
// The frames arrive classified as agent_progress on the chat notifier's
// progress subscription; tests drive the notifier through the same
// public seam the subscription uses.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/providers/verbosity_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../mocks/mock_websocket_service.dart';

class _StubSdk extends SdkApiClient {
  _StubSdk() : super(host: 'localhost', port: 8081);

  String? persistedVerbosity;

  @override
  Future<String> getClientConfig() async =>
      '''
{
  "chat": {
    "liveness_timeout_seconds": 90${persistedVerbosity != null ? ',' : ''}
    ${persistedVerbosity != null ? '"verbosity": "$persistedVerbosity"' : ''}
  }
}
''';
}

class _ConnectedWebSocket extends MockWebSocketService {
  @override
  bool get isConnected => true;
}

/// A WebSocketService with a no-op connect but the REAL
/// subscribeToAgentProgress filter and message subject: the live-path tests
/// below inject wire-shaped frames via debugInjectFrame and need the
/// production type/session filter to run, without opening a socket.
class _RealFilterWebSocket extends WebSocketService {
  _RealFilterWebSocket() : super(host: 'localhost', port: 65440);

  @override
  Future<void> connect({String? path}) async {}

  @override
  Future<void> disconnect() async {}
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
  @override
  double get volume => 1.0;
}

/// ChatNotifier with a no-op loadMessages (clean state, no HTTP).
class _L08ChatNotifier extends ChatNotifier {
  _L08ChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}
}

_L08ChatNotifier _notifier(_StubSdk sdk) {
  return _L08ChatNotifier(
    sdkClient: sdk,
    websocket: _ConnectedWebSocket(),
    ttsNotifier: _NoopTts(),
    sessionId: 'test-session',
  );
}

/// Feed a frame through the progress subscription's handler path: the
/// same payload-shape discrimination the live subscription performs
/// (confirmation check, task-event check, tier filter, indicator state).
void _progressFrame(ChatNotifier n, Map<String, dynamic> frame) {
  // The subscription is private; drive the same logic through the
  // notifier's exported debug seam: feed the frame via the shared
  // handleProgressFrame test entry point.
  n.debugHandleProgressFrame(frame);
}

int _systemBubbles(ChatNotifier n, String needle) => n.state.messages
    .where((m) => m.role == 'system' && m.content.contains(needle))
    .length;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('GUI verbosity tier filter (leaf 08)', () {
    test('shouldEmitAgentEvent mirrors TUI tier <= verbosity', () {
      expect(
        shouldEmitAgentEvent(
          currentVerbosity: VerbosityLevel.quiet,
          eventTier: 0,
        ),
        isTrue,
      );
      expect(
        shouldEmitAgentEvent(
          currentVerbosity: VerbosityLevel.quiet,
          eventTier: 1,
        ),
        isFalse,
      );
      expect(
        shouldEmitAgentEvent(
          currentVerbosity: VerbosityLevel.verbose,
          eventTier: 2,
        ),
        isTrue,
      );
    });

    test('quiet drops tier-1 progress frames from the indicator state', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);
      n.setVerbosityLevel(VerbosityLevel.quiet);

      _progressFrame(n, {
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'reading files',
        'tier': 1,
        'session_id': 'test-session',
      });

      expect(n.state.currentProgress, isNull);
    });

    test('quiet keeps tier-0 progress frames', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);
      n.setVerbosityLevel(VerbosityLevel.quiet);

      _progressFrame(n, {
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'completed',
        'tier': 0,
        'session_id': 'test-session',
      });

      expect(n.state.currentProgress, isNotNull);
      expect(n.state.currentProgress!.message, 'completed');
    });

    test('verbose keeps tier-2 progress frames', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);
      n.setVerbosityLevel(VerbosityLevel.verbose);

      _progressFrame(n, {
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'tool started',
        'tier': 2,
        'session_id': 'test-session',
      });

      expect(n.state.currentProgress, isNotNull);
    });

    test('persisted verbosity is restored from client config', () async {
      final sdk = _StubSdk();
      sdk.persistedVerbosity = 'quiet';
      final n = _notifier(sdk);
      // Constructor kicked off the config fetch; let it resolve.
      await Future<void>.delayed(Duration.zero);

      _progressFrame(n, {
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'normal-tier frame',
        'tier': 1,
        'session_id': 'test-session',
      });

      expect(n.state.currentProgress, isNull);
    });
  });

  group('GUI mid-turn task.error (leaf 08)', () {
    test('task.error frame renders a transient error bubble', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'task.error',
        'task_id': 'task-1',
        'step_id': 'step-3',
        'error': 'tool exited 1: permission denied',
        'chat_visible': true,
        'session_id': 'test-session',
      });

      expect(
        _systemBubbles(n, 'step step-3 failed: tool exited 1'),
        1,
        reason:
            'mid-turn step errors must surface in chat, not vanish '
            'into the spinner',
      );
    });

    test('task.error with chat_visible false is consumed silently', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'task.error',
        'task_id': 'task-1',
        'step_id': 'step-3',
        'error': 'hidden failure',
        'chat_visible': false,
        'session_id': 'test-session',
      });

      expect(n.state.messages, isEmpty);
    });

    test('foreign-session task.error does not render here', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'task.error',
        'task_id': 'task-1',
        'step_id': 'step-3',
        'error': 'other session failure',
        'session_id': 'other-session',
      });

      expect(n.state.messages, isEmpty);
    });

    test('plain progress frames without source_topic pass through', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);
      n.setVerbosityLevel(VerbosityLevel.verbose);

      _progressFrame(n, {
        'type': 'agent_progress',
        'agent_id': 'coder',
        'message': 'thinking',
        'tier': 1,
        'session_id': 'test-session',
      });

      expect(n.state.currentProgress, isNotNull);
      expect(n.state.messages, isEmpty);
    });
  });

  group('GUI review feedback (leaf 08)', () {
    test('review rejection renders feedback with revision count', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'task.review_completed',
        'task_id': 'task-1',
        'step_id': 'step-2',
        'status': 'rejected',
        'feedback': 'missing error handling',
        'revision_count': 2,
        'session_id': 'test-session',
      });

      expect(
        _systemBubbles(
          n,
          'step step-2: review rejected (revision #2) -- missing error handling',
        ),
        1,
      );
    });

    test('needs_info renders the feedback request', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'step.review_completed',
        'task_id': 'task-1',
        'step_id': 'step-2',
        'status': 'needs_info',
        'feedback': 'which file?',
        'session_id': 'test-session',
      });

      expect(
        _systemBubbles(n, 'step step-2: review needs more info -- which file?'),
        1,
      );
    });

    test('approved is a quiet progress note, not a bubble', () {
      final sdk = _StubSdk();
      final n = _notifier(sdk);

      _progressFrame(n, {
        'type': 'agent_progress',
        'source_topic': 'task.review_completed',
        'task_id': 'task-1',
        'step_id': 'step-2',
        'status': 'approved',
        'session_id': 'test-session',
      });

      expect(n.state.messages, isEmpty);
      expect(n.state.currentProgress?.stage, 'review approved');
    });
  });

  /// Live-filter-path gate (bughunt wave2, finding 1): the frames above ride
  /// the debugHandleProgressFrame seam, which bypasses the real
  /// subscribeToAgentProgress filter (websocket_service.dart). These tests
  /// inject wire-shaped frames into a REAL WebSocketService message subject
  /// so the production type/session filter must pass them before the
  /// notifier's handler chain runs — proving the daemon's new
  /// task.error/review_completed agent_progress classification is actually
  /// reachable end-to-end in the GUI.
  group('GUI mid-turn task events through the LIVE stream filter (bughunt wave2)', () {
    test(
      'task.error agent_progress frame survives the filter and renders',
      () async {
        final sdk = _StubSdk();
        final ws = _RealFilterWebSocket();
        // Plain ChatNotifier: _L08ChatNotifier's no-op loadMessages would skip
        // _ensureSubscriptions — these tests need the REAL subscription armed.
        final n = ChatNotifier(
          sdkClient: sdk,
          websocket: ws,
          ttsNotifier: _NoopTts(),
          sessionId: 'test-session',
        );
        await pumpEventQueue(times: 20);

        // Wire shape as transformBusEventToWS emits it post-fix.
        ws.debugInjectFrame({
          'type': 'agent_progress',
          'source_topic': 'task.error',
          'task_id': 'task-1',
          'step_id': 'step-3',
          'error': 'tool exited 1: permission denied',
          'chat_visible': true,
          'session_id': 'test-session',
        });
        await pumpEventQueue(times: 5);

        expect(
          _systemBubbles(n, 'step step-3 failed: tool exited 1'),
          1,
          reason:
              'the live subscribeToAgentProgress filter must pass the '
              'relay-classified task.error frame to _handleTaskEventFrame',
        );
      },
    );

    test(
      'step.review_completed rejected frame survives the filter and renders',
      () async {
        final sdk = _StubSdk();
        final ws = _RealFilterWebSocket();
        // Plain ChatNotifier: _L08ChatNotifier's no-op loadMessages would skip
        // _ensureSubscriptions — these tests need the REAL subscription armed.
        final n = ChatNotifier(
          sdkClient: sdk,
          websocket: ws,
          ttsNotifier: _NoopTts(),
          sessionId: 'test-session',
        );
        await pumpEventQueue(times: 20);

        ws.debugInjectFrame({
          'type': 'agent_progress',
          'source_topic': 'step.review_completed',
          'task_id': 'task-1',
          'step_id': 'step-2',
          'status': 'rejected',
          'feedback': 'missing error handling',
          'revision_count': 2,
          'session_id': 'test-session',
        });
        await pumpEventQueue(times: 5);

        expect(
          _systemBubbles(
            n,
            'step step-2: review rejected (revision #2) -- missing error handling',
          ),
          1,
        );
      },
    );

    test(
      'session-less task.error (the real publisher shape) renders',
      () async {
        // Publishers (tactical.go / review_manager.go) stamp no
        // session_id: the relay broadcasts, and the progress filter must
        // pass these three topics on source_topic alone.
        final sdk = _StubSdk();
        final ws = _RealFilterWebSocket();
        final n = ChatNotifier(
          sdkClient: sdk,
          websocket: ws,
          ttsNotifier: _NoopTts(),
          sessionId: 'test-session',
        );
        await pumpEventQueue(times: 20);

        ws.debugInjectFrame({
          'type': 'agent_progress',
          'source_topic': 'task.error',
          'task_id': 'task-1',
          'step_id': 'step-9',
          'error': 'session-less publisher',
          'chat_visible': true,
        });
        await pumpEventQueue(times: 5);

        expect(_systemBubbles(n, 'step step-9 failed: session-less'), 1);
      },
    );

    test('session-less NON-task agent_progress frame still drops', () async {
      final sdk = _StubSdk();
      final ws = _RealFilterWebSocket();
      final n = ChatNotifier(
        sdkClient: sdk,
        websocket: ws,
        ttsNotifier: _NoopTts(),
        sessionId: 'test-session',
      );
      await pumpEventQueue(times: 20);

      ws.debugInjectFrame({
        'type': 'agent_progress',
        'source_topic': 'agent.progress',
        'agent_id': 'coder',
        'message': 'session-less progress',
        'tier': 0,
      });
      await pumpEventQueue(times: 5);

      expect(n.state.currentProgress, isNull);
    });

    test(
      'pre-fix job_update classification would be dropped by the filter',
      () async {
        // Pin the regression: a frame still classified job_update (old daemon)
        // never reaches the handler — this is the bug the daemon fix closes.
        final sdk = _StubSdk();
        final ws = _RealFilterWebSocket();
        // Plain ChatNotifier: _L08ChatNotifier's no-op loadMessages would skip
        // _ensureSubscriptions — these tests need the REAL subscription armed.
        final n = ChatNotifier(
          sdkClient: sdk,
          websocket: ws,
          ttsNotifier: _NoopTts(),
          sessionId: 'test-session',
        );
        await pumpEventQueue(times: 20);

        ws.debugInjectFrame({
          'type': 'job_update',
          'source_topic': 'task.error',
          'task_id': 'task-1',
          'step_id': 'step-3',
          'error': 'old-daemon classification',
          'session_id': 'test-session',
        });
        await pumpEventQueue(times: 5);

        expect(n.state.messages, isEmpty);
      },
    );
  });
}
