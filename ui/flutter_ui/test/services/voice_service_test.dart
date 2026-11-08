// Unit tests for VoiceService (voice-local-leg leaf 13b, T1).
//
// Runs against a fake Dio HttpClientAdapter: no network. Verifies the
// bus/call envelope handling ({"result": ...} unwrapping) and the
// VERBATIM error pass-through: the daemon's {"error": "<message>"} body
// must reach the caller unchanged (no GUI-side re-mapping), especially
// the disabled-leg refusal "voice: local leg is disabled".
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/services/voice_service.dart';

class _CannedRoute {
  final int status;
  final Map<String, dynamic> body;
  const _CannedRoute(this.status, this.body);
}

void main() {
  group('VoiceService open/end/status (bus/call envelope)', () {
    test('open posts the voice.local.open shape and unwraps result', () async {
      late Map<String, dynamic> sent;
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(200, {
          'result': {
            'call_id': 'c1',
            'in_path': '/sock/c1.in',
            'out_path': '/sock/c1.out',
            'session_id': 's1',
          },
        }),
        capture: (b) => sent = b,
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      final open = await service.open('eve', 'desk');

      expect(sent['method'], 'voice.local.open');
      expect(sent['params']['employee_id'], 'eve');
      expect(sent['params']['label'], 'desk');
      expect(open.callId, 'c1');
      expect(open.sessionId, 's1');
      expect(open.inPath, '/sock/c1.in');
      expect(open.outPath, '/sock/c1.out');
    });

    test('end posts call_id and returns ok', () async {
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(200, {
          'result': {'ok': true},
        }),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      final ok = await service.end('c1');

      expect(adapter.lastMethod, 'voice.local.end');
      expect(adapter.lastParams, {'call_id': 'c1'});
      expect(ok, isTrue);
    });

    test('status returns parsed rows across both legs', () async {
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(200, {
          'result': {
            'calls': [
              {
                'call_id': 'c1',
                'employee_id': 'eve',
                'target': 'desk',
                'since_rfc3339': '2026-11-07T12:00:00Z',
                'leg': 'local',
              },
              {
                'call_id': 'c2',
                'employee_id': 'bob',
                'target': '+15551234',
                'since_rfc3339': '2026-11-07T12:01:00Z',
                'leg': 'phone',
              },
            ],
          },
        }),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      final rows = await service.status();

      expect(adapter.lastMethod, 'voice.local.status');
      expect(rows, hasLength(2));
      expect(rows[0].leg, 'local');
      expect(rows[1].leg, 'phone');
    });

    test('status tolerates a malformed calls body (empty list)', () async {
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(200, <String, dynamic>{
          'result': <String, dynamic>{},
        }),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      expect(await service.status(), isEmpty);
    });
  });

  group('VoiceService error pass-through (verbatim envelopes)', () {
    test('disabled leg surfaces the daemon message VERBATIM', () async {
      // The named refusal for voice.local.enabled=false
      // (internal/rpc/voice_local.go ErrVoiceLocalDisabled), as the
      // bus/call handler returns it: HTTP 500 + {"error": ...}.
      final adapter = _CapturingAdapter(
        (body) async =>
            const _CannedRoute(500, {'error': 'voice: local leg is disabled'}),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      try {
        await service.open('eve', 'desk');
        fail('expected VoiceServiceException');
      } on VoiceServiceException catch (e) {
        expect(e.message, 'voice: local leg is disabled');
        expect(e.statusCode, 500);
      }
    });

    test('unknown employee envelope passes through verbatim', () async {
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(500, {
          'error': 'voice: unknown employee: ghost',
        }),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      try {
        await service.open('ghost', '');
        fail('expected VoiceServiceException');
      } on VoiceServiceException catch (e) {
        expect(e.message, 'voice: unknown employee: ghost');
      }
    });

    test('busy and duplicate refusals pass through verbatim', () async {
      for (final canned in const [
        'voice: call registry busy (over capacity or employee already live)',
        'voice: a live call to this target already exists',
      ]) {
        final adapter = _CapturingAdapter(
          (body) async => _CannedRoute(409, {'error': canned}),
        );
        final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);
        try {
          await service.open('eve', 'desk');
          fail('expected VoiceServiceException');
        } on VoiceServiceException catch (e) {
          expect(e.message, canned);
        }
      }
    });

    test('method-not-found (unregistered plane) surfaces verbatim', () async {
      final adapter = _CapturingAdapter(
        (body) async => const _CannedRoute(500, {
          'error': 'method not found: voice.local.open',
        }),
      );
      final service = VoiceService(dio: Dio()..httpClientAdapter = adapter);

      try {
        await service.open('eve', 'desk');
        fail('expected VoiceServiceException');
      } on VoiceServiceException catch (e) {
        expect(e.message, 'method not found: voice.local.open');
      }
    });
  });
}

/// Adapter that captures the posted payload and answers from a builder.
class _CapturingAdapter implements HttpClientAdapter {
  final Future<_CannedRoute> Function(Map<String, dynamic> payload) _answer;
  final void Function(Map<String, dynamic> payload)? capture;

  String? lastMethod;
  Map<String, dynamic>? lastParams;

  _CapturingAdapter(this._answer, {this.capture});

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final bodyBytes = await requestStream?.fold<List<int>>(
      <int>[],
      (a, b) => a..addAll(b),
    );
    final payload = bodyBytes == null
        ? <String, dynamic>{}
        : jsonDecode(utf8.decode(bodyBytes)) as Map<String, dynamic>;
    lastMethod = payload['method'] as String?;
    lastParams = payload['params'] == null
        ? null
        : Map<String, dynamic>.from(payload['params'] as Map);
    capture?.call(payload);
    final canned = await _answer(payload);
    return ResponseBody.fromString(
      jsonEncode(canned.body),
      canned.status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}
