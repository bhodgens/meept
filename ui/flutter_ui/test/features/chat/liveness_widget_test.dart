// F-NEW-14 GUI-path turn-liveness widget test (acceptance report
// "F-NEW-14 liveness widget test not added"; 05-navigation/02-REPORT.md,
// findings REPORT-GUI-RERUN.md recommendation 13: "GUI chat completion must
// be observable on the GUI's own transport ... add a GUI-path liveness
// test").
//
// The surface: a submitted GUI turn false-stalls at the per-turn liveness
// timer (PendingTurnStatus.stalled, "no progress for Ns -- task may still
// complete") unless progress/terminal traffic re-arms it. This pins both
// halves of the contract at the widget/provider seam:
//
//   1. submitTurn arms the timer: with NO progress traffic the pending
//      turn goes stalled after the effective timeout (kTurnLivenessTimeout,
//      120 s), and PendingTurnIndicator renders the honest stalled text --
//      stalled is NOT terminal and clears when the late turn.terminal
//      lands (the completion is observable, F-NEW-14's core requirement).
//   2. agent_progress frames RE-ARM the timer (the scopes-2 fix in
//      _ensureSubscriptions' progress subscription -> noteTurnProgress):
//      a turn that is visibly progressing must NOT false-stall while
//      progress keeps arriving within the window.
//
// fakeAsync drives the 120 s timer without real waiting; the progress
// frames are fed through the public noteTurnProgress path that the WS
// subscription calls, exactly as chat_provider.dart does.

import 'package:fake_async/fake_async.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/chat_provider.dart';
import 'package:meept_ui/providers/tts_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';
import 'package:meept_ui/services/websocket_service.dart';

import '../../mocks/mock_websocket_service.dart';

/// SDK stub with a canned accepted ack and a no-op client-config fetch
/// (empty body -> parseClientConfig gives no chat key -> the
/// kTurnLivenessTimeout fallback applies, which is what we drive).
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<ChatSubmitAck> submitTurn({
    required String message,
    String? conversationId,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    return const ChatSubmitAck(
      turnId: 'turn-1',
      conversationId: 'test-session',
      sessionId: 'test-session',
      accepted: true,
      note: 'accepted; result arrives via turn.terminal',
    );
  }

  @override
  Future<String> getClientConfig() async => '';
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

/// ChatNotifier with a no-op history load (clean state, no HTTP).
class _TestChatNotifier extends ChatNotifier {
  _TestChatNotifier({
    required super.sdkClient,
    required super.websocket,
    required super.ttsNotifier,
    required super.sessionId,
  });

  @override
  Future<void> loadMessages() async {}
}

ChatNotifier _notifier() {
  return _TestChatNotifier(
    sdkClient: _StubSdkClient(),
    websocket: _ConnectedWebSocket(),
    ttsNotifier: _NoopTts(),
    sessionId: 'test-session',
  );
}

/// Feed a terminal-shaped frame into the notifier through the same public
/// handler the WS subscription uses (chat_submit_flow_test precedent).
void _terminal(
  ChatNotifier notifier, {
  required String turnId,
  required String status,
  String reply = '',
}) {
  notifier.debugHandleTurnTerminal(
    TurnTerminalEvent(
      turnId: turnId,
      conversationId: 'test-session',
      sessionId: 'test-session',
      status: status,
      reply: reply,
      durationMs: 10,
    ),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('F-NEW-14 GUI turn liveness', () {
    test('silent turn stalls at the liveness timeout; late terminal still '
        'renders (stalled is NOT terminal)', () {
      fakeAsync((async) {
        final notifier = _notifier();
        addTearDown(notifier.dispose);
        final fut = notifier.submitTurn(
          sessionId: 'test-session',
          text: 'hello',
        );
        async.flushMicrotasks();
        async.elapse(const Duration(milliseconds: 100));

        // Just before the window closes the turn is still tracked as a
        // plain pending turn.
        async.elapse(kTurnLivenessTimeout - const Duration(seconds: 1));
        expect(
          notifier.state.pendingTurns['turn-1']!.status,
          PendingTurnStatus.pending,
          reason: 'a turn under the liveness window must not stall early',
        );

        // Crossing the window with no traffic false-stalls...
        async.elapse(const Duration(seconds: 2));
        expect(
          notifier.state.pendingTurns['turn-1']!.status,
          PendingTurnStatus.stalled,
        );
        expect(
          notifier.state.pendingTurns['turn-1']!.progressText,
          contains('no progress for'),
        );

        // ...but the turn is NOT dead: the late turn.terminal (F-NEW-14:
        // completion must be observable on the GUI's own transport) still
        // renders the reply and clears the stalled state.
        _terminal(
          notifier,
          turnId: 'turn-1',
          status: TurnTerminalEvent.statusCompleted,
          reply: 'the answer is 4',
        );
        expect(notifier.state.pendingTurns, isEmpty);
        expect(
          notifier.state.messages.any(
            (m) => m.role == 'assistant' && m.content == 'the answer is 4',
          ),
          isTrue,
          reason: 'the GUI completion surface must show the answer',
        );

        return fut;
      });
    });

    test('progress frames re-arm the timer: a visibly progressing turn does '
        'NOT false-stall', () {
      fakeAsync((async) {
        final notifier = _notifier();
        addTearDown(notifier.dispose);
        final fut = notifier.submitTurn(
          sessionId: 'test-session',
          text: 'hello',
        );
        async.flushMicrotasks();

        // Keep progress arriving well inside the 120 s window, past the
        // point where an unre-armed timer would have fired (this is the
        // scopes-2 noteTurnProgress re-arm path; a long turn must not
        // flip to stalled just because it outlives one window).
        for (var i = 0; i < 7; i++) {
          async.elapse(const Duration(seconds: 100));
          notifier.noteTurnProgress('turn-1', 'executing step $i');
        }
        // 700 s of progressing turn, 5.8x the window: still live.
        expect(
          notifier.state.pendingTurns['turn-1']!.status,
          isNot(PendingTurnStatus.stalled),
          reason:
              'progress traffic re-arms the liveness timer; a turn that '
              'keeps reporting progress must never false-stall',
        );
        expect(
          notifier.state.pendingTurns['turn-1']!.progressText,
          'executing step 6',
        );

        // The re-armed timer still fires once progress stops: silence
        // after the last frame eventually stalls (the honest "may still
        // complete" state).
        async.elapse(kTurnLivenessTimeout + const Duration(seconds: 1));
        expect(
          notifier.state.pendingTurns['turn-1']!.status,
          PendingTurnStatus.stalled,
        );

        return fut;
      });
    });
  });
}
