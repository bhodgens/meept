// Unit tests for VoiceEventsSubscription (bughunt C-12): the bounded
// per-call voice.local.events poller that mirrors the CLI's
// voiceEventTracker semantics (cursor advance by assignment, silent
// error/timeout ticks, truncated gap note, found=false only ends a feed
// that was ever seen). Driven by a scripted fake service against the
// real 500ms timer cadence; asserts observable onRows behavior only.
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/voice_service.dart';

class _ScriptedService implements VoiceServiceContract {
  _ScriptedService();

  final List<VoiceLocalEventsResult> batches = [];
  final List<int> requestedLimits = [];
  VoiceServiceException? error;

  /// When the script is exhausted, polls return an empty cursor echo
  /// with found set to this (mirrors a daemon answering no-new-events;
  /// set false to simulate the call being evicted/teared down).
  bool terminalFound = true;
  int _lastCursor = 0;

  @override
  Future<VoiceLocalOpenResult> open(String employeeId, String label) async {
    throw UnimplementedError();
  }

  @override
  Future<bool> end(String callId) async => true;

  @override
  Future<List<VoiceSessionRow>> status() async => const [];

  @override
  Future<VoiceLocalEventsResult> events(String callId, {int? limit}) async {
    requestedLimits.add(limit ?? -1);
    final err = error;
    if (err != null) throw err;
    if (batches.isNotEmpty) {
      final res = batches.removeAt(0);
      _lastCursor = res.cursor;
      return res;
    }
    // Cursor echo: no new events since _lastCursor.
    return VoiceLocalEventsResult(
      callId: callId,
      events: const [],
      cursor: _lastCursor,
      found: terminalFound,
    );
  }
}

/// Events service whose RPC hangs forever (a wedged connection): the
/// subscription must bound each round-trip itself and abandon it.
class _SlowEventsService implements VoiceServiceContract {
  int callsStarted = 0;

  @override
  Future<VoiceLocalOpenResult> open(String employeeId, String label) async {
    throw UnimplementedError();
  }

  @override
  Future<bool> end(String callId) async => true;

  @override
  Future<List<VoiceSessionRow>> status() async => const [];

  @override
  Future<VoiceLocalEventsResult> events(String callId, {int? limit}) async {
    callsStarted++;
    // Longer than voiceEventsCallTimeout: the subscription's own
    // .timeout is what unblocks the tick.
    await Future<void>.delayed(voiceEventsCallTimeout * 4);
    return VoiceLocalEventsResult(
      callId: callId,
      events: const [],
      cursor: 0,
      found: true,
    );
  }
}

VoiceCallEvent _ev(int seq, String kind, {String text = ''}) {
  return VoiceCallEvent(
    seq: seq,
    kind: kind,
    at: '2026-11-07T12:00:01Z',
    text: text,
  );
}

void main() {
  test('fromEvent renders CLI-shaped lines (voiceEventRender parity)', () {
    String line(
      String kind, {
      String text = '',
      int words = 0,
      int ms = 0,
      String reason = '',
    }) {
      return VoiceEventRow.fromEvent(
        VoiceCallEvent(
          seq: 1,
          kind: kind,
          at: '2026-11-07T12:34:56.789Z',
          text: text,
          words: words,
          ms: ms,
          reason: reason,
        ),
      ).line;
    }

    expect(line('heard', text: 'hi'), '12:34:56 heard: hi');
    expect(
      line('prompt_dispatch', text: 'ok', words: 3),
      '12:34:56 thinking (3 words): ok',
    );
    expect(
      line('turn_final', words: 12, ms: 850),
      '12:34:56 replied (12 words, 850ms)',
    );
    expect(line('barge_in'), '12:34:56 >> barge-in');
    expect(
      line('call_ended', reason: 'user hangup'),
      '12:34:56 call ended: user hangup',
    );
    expect(line('prompt_held', text: 'x'), '12:34:56 held (peer speaking): x');
    expect(line('prompt_released', text: 'x'), '12:34:56 released: x');
    expect(
      line('prompt_dropped', reason: 'superseded', text: 'x'),
      '12:34:56 dropped (superseded): x',
    );
    // Unknown kinds render the raw kind.
    expect(line('floor_watchdog'), '12:34:56 floor_watchdog');
  });

  test('barge-in rows expose the attention marker; synthetic kinds do not', () {
    final barge = VoiceEventRow.fromEvent(_ev(1, 'barge_in'));
    expect(barge.isBargeIn, isTrue);
    const gap = VoiceEventRow(kind: VoiceEventRow.gapKind, line: 'x');
    expect(gap.isBargeIn, isFalse);
    const ended = VoiceEventRow(kind: VoiceEventRow.endedKind, line: 'x');
    expect(ended.isBargeIn, isFalse);
  });

  test('wire decode: fromJson tolerates omitted kind-dependent fields', () {
    final res = VoiceLocalEventsResult.fromJson({
      'call_id': 'c1',
      'events': [
        {
          'seq': 7,
          'kind': 'heard',
          'at': '2026-11-07T12:00:00.123456789Z',
          'text': 'hey',
        },
        {'seq': 8, 'kind': 'turn_final', 'at': 'x', 'words': 4, 'ms': 300},
      ],
      'cursor': 8,
      'found': true,
    });
    expect(res.callId, 'c1');
    expect(res.cursor, 8);
    expect(res.found, isTrue);
    expect(res.truncated, isFalse);
    expect(res.hasMore, isFalse);
    expect(res.events, hasLength(2));
    expect(res.events[0].seq, 7);
    expect(res.events[0].text, 'hey');
    expect(res.events[0].words, 0);
    expect(res.events[1].words, 4);
    expect(res.events[1].ms, 300);
  });

  test('subscription: appends rows once, advances cursor by assignment, '
      'never re-appends an empty poll', () async {
    final service = _ScriptedService()
      ..batches.add(
        VoiceLocalEventsResult(
          callId: 'c1',
          events: [_ev(1, 'heard', text: 'a')],
          cursor: 1,
          found: true,
        ),
      );
    final seen = <VoiceEventRow>[];
    final sub = VoiceEventsSubscription(
      voiceService: service,
      callId: 'c1',
      onRows: seen.addAll,
    )..start();
    addTearDown(sub.stop);

    // First tick consumes the batch; subsequent ticks see only the
    // echoed batch with no new events and append nothing. The wide
    // window rides the final gap check (>=2 ticks) rather than the
    // first completion, so one lost scheduler slice cannot flake it.
    await Future<void>.delayed(voiceEventsPollInterval * 6);
    final heardRows = seen.where((r) => r.line.contains('heard: a')).length;
    expect(heardRows, 1);
    expect(sub.cursor, 1);
    expect(sub.ended, isFalse);
    // limit stays unset by default (the CLI leaves it unset too).
    expect(service.requestedLimits.first, -1);
  });

  test('subscription: truncated batch surfaces a gap note row', () async {
    final service = _ScriptedService()
      ..batches.add(
        VoiceLocalEventsResult(
          callId: 'c1',
          events: [_ev(2, 'heard', text: 'b')],
          cursor: 2,
          truncated: true,
          found: true,
        ),
      );
    final seen = <VoiceEventRow>[];
    final sub = VoiceEventsSubscription(
      voiceService: service,
      callId: 'c1',
      onRows: seen.addAll,
    )..start();
    addTearDown(sub.stop);

    await Future<void>.delayed(voiceEventsPollInterval * 3);
    expect(seen.where((r) => r.kind == VoiceEventRow.gapKind), hasLength(1));
    expect(seen.any((r) => r.line.contains('backlog truncated')), isTrue);
  });

  test('subscription: one RPC round-trip per tick, bounded by '
      'voiceEventsCallTimeout', () async {
    final service = _SlowEventsService();
    final seen = <VoiceEventRow>[];
    final sub = VoiceEventsSubscription(
      voiceService: service,
      callId: 'c1',
      onRows: seen.addAll,
    )..start();
    addTearDown(sub.stop);

    // Give the tick time to start one call and hit the 5s bound.
    await Future<void>.delayed(voiceEventsPollInterval * 2);
    expect(service.callsStarted, 1);
    expect(seen, isEmpty); // nothing printed while the call is in flight
    // The bound fires ~5s after the tick began; the abandoned future's
    // late completion prints nothing even after the tick moved on.
    await Future<void>.delayed(
      voiceEventsCallTimeout + const Duration(milliseconds: 2500),
    );
    expect(seen, isEmpty); // abandoned window: never prints
    expect(sub.ended, isFalse);
    expect(service.callsStarted, greaterThanOrEqualTo(2)); // retry next tick
  });

  test('subscription: found=false before any event keeps ticking '
      '(lazy ring), found=false after events ends the feed', () async {
    // Batch 1: found=false at cursor 0 (ring not yet created) - must
    // NOT end the feed. Batch 2: real events. Then every later poll
    // returns the evicted envelope (found=false) - with events
    // consumed, the feed must end with one ended row.
    final service = _ScriptedService()
      ..terminalFound = false
      ..batches.addAll([
        const VoiceLocalEventsResult(
          callId: 'c1',
          events: [],
          cursor: 0,
          found: false,
        ),
        VoiceLocalEventsResult(
          callId: 'c1',
          events: [_ev(1, 'heard', text: 'c')],
          cursor: 1,
          found: true,
        ),
      ]);
    final seen = <VoiceEventRow>[];
    final sub = VoiceEventsSubscription(
      voiceService: service,
      callId: 'c1',
      onRows: seen.addAll,
    )..start();
    addTearDown(sub.stop);

    // Poll until the feed ends (or fail loudly after a generous window).
    final deadline = DateTime.now().add(const Duration(seconds: 5));
    while (!sub.ended && DateTime.now().isBefore(deadline)) {
      await Future<void>.delayed(const Duration(milliseconds: 100));
    }
    expect(sub.ended, isTrue);
    expect(seen.where((r) => r.kind == VoiceEventRow.endedKind), hasLength(1));
    expect(seen.any((r) => r.line.contains('heard: c')), isTrue);
    // The ended subscription makes no further requests.
    final callsAtEnd = service.requestedLimits.length;
    await Future<void>.delayed(voiceEventsPollInterval * 2);
    expect(service.requestedLimits.length, callsAtEnd);
  });

  test(
    'subscription: errored ticks are silent and retried next cycle',
    () async {
      final service = _ScriptedService()
        ..error = const VoiceServiceException(message: 'boom', statusCode: 500);
      final seen = <VoiceEventRow>[];
      final sub = VoiceEventsSubscription(
        voiceService: service,
        callId: 'c1',
        onRows: seen.addAll,
      )..start();
      addTearDown(sub.stop);

      await Future<void>.delayed(voiceEventsPollInterval * 6);
      expect(seen, isEmpty);
      expect(sub.ended, isFalse);
      // The error path never kills the poller: ticks kept coming. The
      // floor stays well under the windows above so a slow CI runner
      // cannot flake on scheduler slices (ticks can only be MORE).
      expect(service.requestedLimits.length, greaterThanOrEqualTo(2));
    },
  );
}
