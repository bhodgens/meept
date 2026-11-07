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

  int openCalls = 0;
  int endCalls = 0;
  int statusCalls = 0;
  String? lastOpenedEmployee;
  String? lastOpenedLabel;
  final List<String> endedCallIds = <String>[];

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
  String leg = 'local',
}) {
  return VoiceSessionRow(
    callId: callId,
    employeeId: employeeId,
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
        _row('c1'),
        _row('c2', employeeId: 'bob', target: '+15551234', leg: 'phone'),
      ];
    await tester.pumpWidget(_host(service: service));
    await _settle(tester);

    expect(find.text('eve -> desk'), findsOneWidget);
    expect(find.text('bob -> +15551234'), findsOneWidget);
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
