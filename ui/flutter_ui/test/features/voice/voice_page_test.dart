// Widget tests for the voice page (voice-local-leg leaf 13b, T2+T3).
//
// The page runs against a FAKE VoiceService (the notifier takes the
// service as a constructor seam) and a fake employee lister, so no
// network is touched. Covers: start -> open flow, the 2s live status
// poll rendering rows from both legs, end per session, and the VERBATIM
// disabled-leg envelope surfacing.
//
// NOTE on timers: the page starts a periodic 2s poll. Each test ends by
// disposing the ProviderScope (pumping a fresh empty widget tree), which
// cancels the notifier's timer, so no pending-timer flakes.
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/features/voice/voice_page.dart';
import 'package:meept_ui/services/voice_audio.dart';
import 'package:meept_ui/services/voice_service.dart';

class _FakeVoiceService implements VoiceServiceContract {
  _FakeVoiceService();

  List<VoiceSessionRow> statusRows = const [];
  VoiceServiceException? openError;
  VoiceLocalOpenResult? openResult;

  // Telemetry (bughunt C-12): batches returned per call, in order; the
  // fake pops one batch per events() call, then repeats the last entry.
  final Map<String, List<VoiceLocalEventsResult>> eventBatches = {};
  int openCalls = 0;
  int endCalls = 0;
  int statusCalls = 0;
  int eventsCalls = 0;
  String? lastOpenedEmployee;
  String? lastOpenedLabel;
  final List<String> endedCallIds = <String>[];
  final List<String> eventsCallIds = <String>[];

  @override
  Future<VoiceLocalOpenResult> open(String employeeId, String label) async {
    openCalls++;
    lastOpenedEmployee = employeeId;
    lastOpenedLabel = label;
    final err = openError;
    if (err != null) throw err;
    return openResult ??
        const VoiceLocalOpenResult(
          callId: 'c1',
          inPath: '/sock/c1.in',
          outPath: '/sock/c1.out',
          sessionId: 's1',
        );
  }

  @override
  Future<bool> end(String callId) async {
    endCalls++;
    endedCallIds.add(callId);
    return true;
  }

  @override
  Future<List<VoiceSessionRow>> status() async {
    statusCalls++;
    return statusRows;
  }

  @override
  Future<VoiceLocalEventsResult> events(String callId, {int? limit}) async {
    eventsCalls++;
    eventsCallIds.add(callId);
    final batches = eventBatches[callId];
    if (batches == null || batches.isEmpty) {
      return VoiceLocalEventsResult(
        callId: callId,
        events: const [],
        cursor: 0,
        found: false,
      );
    }
    if (batches.length == 1) return batches.first;
    return batches.removeAt(0);
  }
}

Widget _host({
  required _FakeVoiceService service,
  Future<List<Map<String, dynamic>>> Function()? employeeLister,
  VoiceAudio? audio,
}) {
  return ProviderScope(
    overrides: [
      voiceProvider.overrideWith(
        (ref) => VoicePageNotifier(
          voiceService: service,
          employeeLister: employeeLister,
        ),
      ),
      if (audio != null) voiceAudioProvider.overrideWithValue(audio),
    ],
    child: const MaterialApp(home: Scaffold(body: VoicePage())),
  );
}

VoiceSessionRow _row(
  String callId, {
  String employeeId = 'eve',
  String target = 'desk',
  String? targetName,
  String? targetDisplay,
  String leg = 'local',
}) {
  return VoiceSessionRow(
    callId: callId,
    employeeId: employeeId,
    targetName: targetName,
    targetDisplay: targetDisplay,
    target: target,
    sinceRfc3339: '2026-11-07T12:00:00Z',
    leg: leg,
  );
}

/// Pump past the initial employee/status fetches.
Future<void> _settle(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 20));
}

void main() {
  testWidgets('renders empty status and no live calls on first build', (
    tester,
  ) async {
    final service = _FakeVoiceService();
    await tester.pumpWidget(_host(service: service));
    await _settle(tester);

    expect(find.text('live calls (both legs, 2s poll)'), findsOneWidget);
    expect(find.text('no live calls'), findsOneWidget);
    expect(find.text('start a local call'), findsOneWidget);

    // Dispose the poll timer.
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('start button opens a call via voice.local.open', (tester) async {
    final service = _FakeVoiceService();
    await tester.pumpWidget(
      _host(
        service: service,
        employeeLister: () async => [
          {'id': 'eve', 'name': 'eve'},
        ],
      ),
    );
    await _settle(tester);

    await tester.enterText(find.byType(TextField).first, 'eve');
    await tester.tap(find.text('start'));
    await _settle(tester);

    expect(service.openCalls, 1);
    expect(service.lastOpenedEmployee, 'eve');
    expect(find.textContaining('call c1 open'), findsOneWidget);

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('live sessions render from the status poll with an end button '
      'per row; end calls voice.local.end', (tester) async {
    final service = _FakeVoiceService()
      ..statusRows = [
        // Legacy-fallback pin (contact-display leaf 03, O1): a row from
        // an OLD server carries only raw `target` -> renders verbatim.
        _row('c1'),
        // New wire shape (leaf 02): the display pair renders, no raw
        // target on the row.
        _row(
          'c2',
          employeeId: 'bob',
          target: '',
          targetName: 'Jane Doe',
          targetDisplay: 'Jane Doe (•••5567)',
          leg: 'phone',
        ),
      ];
    await tester.pumpWidget(_host(service: service));
    await _settle(tester);

    expect(find.text('eve -> desk'), findsOneWidget);
    expect(find.text('bob -> Jane Doe (•••5567)'), findsOneWidget);
    expect(find.text('local - since 2026-11-07T12:00:00Z'), findsOneWidget);
    expect(find.text('phone - since 2026-11-07T12:00:00Z'), findsOneWidget);
    // end button per row
    expect(find.widgetWithText(TextButton, 'end'), findsNWidgets(2));

    // End the first row.
    await tester.tap(find.widgetWithText(TextButton, 'end').first);
    await _settle(tester);

    expect(service.endCalls, 1);
    expect(service.endedCallIds, ['c1']);
    expect(find.textContaining('call c1 ended'), findsOneWidget);

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('the disabled-leg envelope surfaces VERBATIM', (tester) async {
    // ErrVoiceLocalDisabled (voice.local.enabled=false), exactly as the
    // daemon's error envelope words it. The GUI must not re-map it.
    final service = _FakeVoiceService()
      ..openError = const VoiceServiceException(
        message: 'voice: local leg is disabled',
        statusCode: 500,
      );
    await tester.pumpWidget(_host(service: service));
    await _settle(tester);

    await tester.enterText(find.byType(TextField).first, 'eve');
    await tester.tap(find.text('start'));
    await _settle(tester);

    expect(find.text('voice: local leg is disabled'), findsOneWidget);

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('status poll refreshes on its 2s cadence', (tester) async {
    final service = _FakeVoiceService();
    await tester.pumpWidget(_host(service: service));
    await _settle(tester);

    final callsAfterInit = service.statusCalls;
    expect(callsAfterInit, greaterThanOrEqualTo(1));

    // Advance one poll interval; the periodic timer fires.
    await tester.pump(voiceStatusPollInterval);
    expect(service.statusCalls, greaterThan(callsAfterInit));

    // A new live row appears on the next snapshot without any action.
    service.statusRows = [_row('c9')];
    await tester.pump(voiceStatusPollInterval);
    await tester.pump(const Duration(milliseconds: 50));
    expect(find.textContaining('c9'), findsNothing); // rows show employee
    service.statusRows = [_row('c9', employeeId: 'zoe', target: 'lab')];
    await tester.pump(voiceStatusPollInterval);
    await tester.pump(const Duration(milliseconds: 50));
    expect(find.text('zoe -> lab'), findsOneWidget);

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('empty roster degrades to the manual-id hint', (tester) async {
    final service = _FakeVoiceService();
    await tester.pumpWidget(
      _host(service: service, employeeLister: () async => []),
    );
    await _settle(tester);

    // Picker renders with no items; the manual id field is present.
    expect(find.text('employee...'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, 'eve');
    await tester.tap(find.text('start'));
    await _settle(tester);
    expect(service.lastOpenedEmployee, 'eve');

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 3));
  });

  group('voice telemetry feed (bughunt C-12)', () {
    testWidgets('live call renders telemetry lines from the events poll', (
      tester,
    ) async {
      final service = _FakeVoiceService()
        ..statusRows = [_row('c1')]
        ..eventBatches['c1'] = [
          const VoiceLocalEventsResult(
            callId: 'c1',
            events: [
              VoiceCallEvent(
                seq: 1,
                kind: 'heard',
                at: '2026-11-07T12:00:01.5Z',
                text: 'hello there',
              ),
              VoiceCallEvent(
                seq: 2,
                kind: 'turn_final',
                at: '2026-11-07T12:00:02.5Z',
                words: 12,
                ms: 850,
              ),
            ],
            cursor: 2,
            found: true,
          ),
        ];
      await tester.pumpWidget(_host(service: service));
      await _settle(tester);
      // Let the 500ms telemetry timer fire and its future complete
      // (fake async: each await needs its own pump).
      await tester.pump(voiceEventsPollInterval);
      await tester.pump();

      // One subscription started for the live call; the batch rendered.
      expect(service.eventsCalls, greaterThanOrEqualTo(1));
      expect(service.eventsCallIds, contains('c1'));
      // CLI-shaped lines (voiceEventRender parity).
      expect(find.text('12:00:01 heard: hello there'), findsOneWidget);
      expect(find.text('12:00:02 replied (12 words, 850ms)'), findsOneWidget);

      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets('barge-in renders as an attention row', (tester) async {
      final service = _FakeVoiceService()
        ..statusRows = [_row('c1')]
        ..eventBatches['c1'] = [
          const VoiceLocalEventsResult(
            callId: 'c1',
            events: [
              VoiceCallEvent(
                seq: 1,
                kind: 'barge_in',
                at: '2026-11-07T12:00:03Z',
              ),
            ],
            cursor: 1,
            found: true,
          ),
        ];
      await tester.pumpWidget(_host(service: service));
      await _settle(tester);
      await tester.pump(voiceEventsPollInterval);
      await tester.pump();

      expect(find.text('12:00:03 >> barge-in'), findsOneWidget);
      // The attention marker rides the row: a bolt icon next to it.
      expect(find.byIcon(Icons.bolt), findsOneWidget);

      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets('call leaving the status snapshot drops its feed', (
      tester,
    ) async {
      final service = _FakeVoiceService()
        ..statusRows = [_row('c1')]
        ..eventBatches['c1'] = [
          const VoiceLocalEventsResult(
            callId: 'c1',
            events: [
              VoiceCallEvent(
                seq: 1,
                kind: 'heard',
                at: '2026-11-07T12:00:01Z',
                text: 'hi',
              ),
            ],
            cursor: 1,
            found: true,
          ),
        ];
      await tester.pumpWidget(_host(service: service));
      await _settle(tester);
      await tester.pump(voiceEventsPollInterval);
      await tester.pump();
      expect(find.textContaining('heard: hi'), findsOneWidget);

      // Call ends -> status no longer lists it -> feed row disappears
      // and the poller stops (no further events() calls). Ticks that
      // fire in the same pump window as the status refresh are legal;
      // the counter resets only after the reconcile has run.
      service.statusRows = const [];
      await tester.pump(voiceStatusPollInterval);
      await tester.pump();
      service.eventsCallIds.clear();
      await tester.pump(voiceEventsPollInterval * 3);
      await tester.pump();
      expect(find.textContaining('heard: hi'), findsNothing);
      expect(service.eventsCallIds, isEmpty);

      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 3));
    });
  });

  group('audio capture/playback lifecycle (T4, behind a fake)', () {
    testWidgets(
      'unsupported platform degrades: no capture button, note shown',
      (tester) async {
        final service = _FakeVoiceService();
        final audio = _FakeVoiceAudio(supported: false);
        await tester.pumpWidget(_host(service: service, audio: audio));
        await _settle(tester);

        expect(find.text('mic idle'), findsOneWidget);
        expect(
          find.text('capture unavailable on this platform'),
          findsOneWidget,
        );
        expect(find.text('start capture'), findsNothing);

        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 3));
      },
    );

    testWidgets('start capture -> mic capturing; stop capture -> bytes note', (
      tester,
    ) async {
      final service = _FakeVoiceService();
      final audio = _FakeVoiceAudio(supported: true);
      await tester.pumpWidget(_host(service: service, audio: audio));
      await _settle(tester);

      await tester.tap(find.text('start capture'));
      await _settle(tester);

      expect(audio.startCalls, 1);
      expect(find.text('mic capturing...'), findsOneWidget);
      expect(find.text('stop capture'), findsOneWidget);

      await tester.tap(find.text('stop capture'));
      await _settle(tester);

      expect(audio.stopCalls, 1);
      expect(find.text('mic idle'), findsOneWidget);
      expect(find.textContaining('captured 4 bytes'), findsOneWidget);
      // Returned bytes ride through the fake's canned result.
      expect(audio.stopResult.bytes, [1, 2, 3, 4]);

      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets(
      'capture permission refusal surfaces the named error verbatim',
      (tester) async {
        final service = _FakeVoiceService();
        final audio = _FakeVoiceAudio(supported: true);
        await tester.pumpWidget(_host(service: service, audio: audio));
        await _settle(tester);

        audio.startError = const VoiceAudioException(
          'voice: mic permission denied or unavailable',
        );
        await tester.tap(find.text('start capture'));
        await _settle(tester);

        expect(
          find.text('voice: mic permission denied or unavailable'),
          findsOneWidget,
        );
        expect(find.text('mic idle'), findsOneWidget);

        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 3));
      },
    );
  });
}

/// Fake [VoiceAudio] for widget tests: no getUserMedia anywhere.
class _FakeVoiceAudio implements VoiceAudio {
  _FakeVoiceAudio({required this.supported});

  final bool supported;
  int startCalls = 0;
  int stopCalls = 0;
  int playCalls = 0;
  VoiceAudioException? startError;
  VoiceCaptureResult stopResult = const VoiceCaptureResult(
    bytes: [1, 2, 3, 4],
    mimeType: 'audio/webm',
  );
  List<int>? lastPlayed;

  @override
  bool get isSupported => supported;

  @override
  Future<void> startCapture() async {
    startCalls++;
    final err = startError;
    if (err != null) throw err;
  }

  @override
  Future<VoiceCaptureResult> stopCapture() async {
    stopCalls++;
    return stopResult;
  }

  @override
  Future<void> playWav(List<int> wavBytes) async {
    playCalls++;
    lastPlayed = wavBytes;
  }

  @override
  Future<void> dispose() async {}
}
