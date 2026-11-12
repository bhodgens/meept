// voice_service.dart - typed voice.local.* client for the Flutter GUI
// (voice-local-leg leaf 13b).
//
// Transport contract (reuse, do not rebuild): the daemon enables
// POST /api/v1/bus/call when its RPC server exists
// (internal/daemon/daemon.go, http.WithRPCCall(rpcServer.CallMethod));
// the route dispatches ANY registered RPC method
// (internal/comm/http/api_handlers.go handleBusCall). The GUI sends
//
//   {"method": "voice.local.open", "params": {...}}
//
// and gets back {"result": {...}} on success. Errors surface as an HTTP
// status plus {"error": "<message>"}; the message string is passed
// through VERBATIM (no GUI-side re-mapping) so the daemon's named
// refusal envelopes (e.g. "voice: local leg is disabled" for
// voice.local.enabled=false) reach the user unchanged.
//
// RPC shapes (internal/rpc/voice_local.go, CL8 verbatim):
//   voice.local.open   {employee_id, label} -> {call_id, in_path, out_path, session_id}
//   voice.local.end    {call_id}            -> {ok}
//   voice.local.status {}                   -> {calls: [{call_id, employee_id,
//                                              target_name, target_display,
//                                              since_rfc3339, leg}]}
//     (contact-display leaf 02: display pair; old servers still send the
//     raw `target` — the decode below stays tolerant of both.)
//   voice.local.events  {call_id, cursor?, limit?}
//                       -> {call_id, events: [{seq, kind, at, text?, words?,
//                          ms?, reason?}], cursor, truncated?, has_more?,
//                          found}
//     (bughunt C-12 GUI/TUI parity: the CLI prints this feed live; the GUI
//     polls it on the same cadence through VoiceEventsSubscription.)
import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show debugPrint;

import '../core/constants.dart';
import 'storage_service.dart';

/// One live voice call row from voice.local.status (CL8 shape,
/// contact-display leaf 02/03: name + display pair; `target` is the
/// deprecated raw string an OLD server still sends — nullable, and
/// NEVER used for routing, only as the legacy render fallback).
class VoiceSessionRow {
  final String callId;
  final String employeeId;

  /// Boundary-resolved contact name (leaf 02). Absent on old servers.
  final String? targetName;

  /// Operator-safe display form (name + last-4, or "local:<label>"
  /// verbatim on local legs). Absent on old servers.
  final String? targetDisplay;

  /// Deprecated raw target; non-empty only from an old server or a
  /// local-leg label.
  final String target;
  final String sinceRfc3339;

  /// "phone" or "local" - status lists BOTH legs.
  final String leg;

  const VoiceSessionRow({
    required this.callId,
    required this.employeeId,
    this.targetName,
    this.targetDisplay,
    required this.target,
    required this.sinceRfc3339,
    required this.leg,
  });

  /// The render string for this row (contact-display leaf 03, master
  /// O1): display form first, then the name, then the legacy raw
  /// target, else 'unknown'. Pure field preference — no client-side
  /// resolution or number formatting.
  String get displayTarget {
    final display = targetDisplay;
    if (display != null && display.isNotEmpty) return display;
    final name = targetName;
    if (name != null && name.isNotEmpty) return name;
    if (target.isNotEmpty) return target;
    return 'unknown';
  }

  factory VoiceSessionRow.fromJson(Map<String, dynamic> json) {
    return VoiceSessionRow(
      callId: json['call_id'] as String? ?? '',
      employeeId: json['employee_id'] as String? ?? '',
      targetName: json['target_name'] as String?,
      targetDisplay: json['target_display'] as String?,
      // Tolerant decode: old servers still send `target`; the contact-
      // display daemon omits it entirely.
      target: json['target'] as String? ?? '',
      sinceRfc3339: json['since_rfc3339'] as String? ?? '',
      leg: json['leg'] as String? ?? '',
    );
  }
}

/// The voice.local.open result (CL8 verbatim).
///
/// [inPath]/[outPath] are the audio-pair unix socket files under
/// $MEEPT_HOME/voice/sock the DAEMON-side clients dial. A browser cannot
/// dial unix sockets, so the GUI keeps them informational only (see
/// docs/workflows/voice.md GUI section).
class VoiceLocalOpenResult {
  final String callId;
  final String inPath;
  final String outPath;
  final String sessionId;

  const VoiceLocalOpenResult({
    required this.callId,
    required this.inPath,
    required this.outPath,
    required this.sessionId,
  });

  factory VoiceLocalOpenResult.fromJson(Map<String, dynamic> json) {
    return VoiceLocalOpenResult(
      callId: json['call_id'] as String? ?? '',
      inPath: json['in_path'] as String? ?? '',
      outPath: json['out_path'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
    );
  }
}

/// One per-call telemetry event row from voice.local.events (CL8
/// verbatim; bughunt C-12). [kind] is one of heard, prompt_dispatch,
/// prompt_held, prompt_released, prompt_dropped, turn_final, barge_in,
/// call_ended (a future daemon kind renders as its raw string).
/// [at] is RFC3339Nano on the daemon clock. [text] is an always-truncated
/// preview; [words]/[ms]/[reason] are kind-dependent and 0/"" when
/// omitted on the wire.
class VoiceCallEvent {
  final int seq;
  final String kind;
  final String at;
  final String text;
  final int words;
  final int ms;
  final String reason;

  const VoiceCallEvent({
    required this.seq,
    required this.kind,
    required this.at,
    this.text = '',
    this.words = 0,
    this.ms = 0,
    this.reason = '',
  });

  factory VoiceCallEvent.fromJson(Map<String, dynamic> json) {
    return VoiceCallEvent(
      seq: (json['seq'] as num?)?.toInt() ?? 0,
      kind: json['kind'] as String? ?? '',
      at: json['at'] as String? ?? '',
      text: json['text'] as String? ?? '',
      words: (json['words'] as num?)?.toInt() ?? 0,
      ms: (json['ms'] as num?)?.toInt() ?? 0,
      reason: json['reason'] as String? ?? '',
    );
  }
}

/// The voice.local.events result (CL8 verbatim; bughunt C-12).
///
/// Cursor semantics (internal/rpc/voice_local.go): the daemon returns the
/// highest seq in [events], ECHOING the request cursor when nothing new
/// was returned, so a poller advances BY ASSIGNMENT
/// (`cursor = result.cursor`) and is correct in both directions.
/// [truncated] reports genuine ring eviction (rows between the request
/// cursor and the retained window were overwritten) — the poller notes
/// the gap instead of failing. [hasMore] reports a limit-capped backlog;
/// [found] is false when the call is unknown/evicted or its ring does
/// not exist yet (rings are created lazily by the first append).
class VoiceLocalEventsResult {
  final String callId;
  final List<VoiceCallEvent> events;

  /// Highest seq returned; echoes the request cursor when the batch was
  /// empty. Advance by assignment.
  final int cursor;
  final bool truncated;
  final bool hasMore;
  final bool found;

  const VoiceLocalEventsResult({
    required this.callId,
    required this.events,
    required this.cursor,
    this.truncated = false,
    this.hasMore = false,
    this.found = false,
  });

  factory VoiceLocalEventsResult.fromJson(Map<String, dynamic> json) {
    final rawEvents = json['events'];
    return VoiceLocalEventsResult(
      callId: json['call_id'] as String? ?? '',
      events: (rawEvents is List ? rawEvents : const <dynamic>[])
          .whereType<Map>()
          .map((e) => VoiceCallEvent.fromJson(Map<String, dynamic>.from(e)))
          .toList(growable: false),
      cursor: (json['cursor'] as num?)?.toInt() ?? 0,
      truncated: json['truncated'] == true,
      hasMore: json['has_more'] == true,
      found: json['found'] == true,
    );
  }
}

/// Error thrown by [VoiceService] when the daemon refuses a call.
///
/// [message] is the daemon's error-envelope string VERBATIM - the GUI
/// never re-maps or rewords it (leaf 13b contract).
class VoiceServiceException implements Exception {
  final String message;
  final int statusCode;

  const VoiceServiceException({
    required this.message,
    required this.statusCode,
  });

  @override
  String toString() => message;
}

/// The voice.local.* surface [VoiceService] implements. The seam the
/// voice page's notifier and widget tests depend on (leaf 13b T2: widget
/// tests run against a fake of this contract, never the network).
abstract class VoiceServiceContract {
  /// voice.local.open - open a local call for [employeeId].
  Future<VoiceLocalOpenResult> open(String employeeId, String label);

  /// voice.local.end - end one call. Double end is idempotent (ok=true).
  Future<bool> end(String callId);

  /// voice.local.status - live calls across BOTH legs.
  Future<List<VoiceSessionRow>> status();

  /// voice.local.events - one telemetry ring read for [callId] since
  /// [cursor] (bughunt C-12). [limit] is optional (the daemon defaults
  /// it). The result cursor advances BY ASSIGNMENT (daemon echo
  /// semantics); [found] is false for an unknown/evicted call or a
  /// ring that no event has created yet.
  Future<VoiceLocalEventsResult> events(String callId, {int? limit});
}

/// Poll cadence for the per-call telemetry feed (bughunt C-12): the
/// CLI's voiceEventPollInterval (cmd/meept/voice.go, 500 ms).
const Duration voiceEventsPollInterval = Duration(milliseconds: 500);

/// One voice.local.events round-trip is bounded like the CLI's poller
/// (C-11): a wedged daemon connection cannot hold a tick past this
/// window; the tick is abandoned and retried on the next cycle.
const Duration voiceEventsCallTimeout = Duration(seconds: 5);

/// Typed client for the voice.local.* RPC plane over the bus/call bridge.
///
/// Auth/header handling matches the chat_provider/SdkApiClient pattern:
/// Bearer API key, JSON content type, HTTPS-only base URL built from
/// storage-backed host/port (see [fromDefaults]).
class VoiceService implements VoiceServiceContract {
  final Dio _dio;

  VoiceService({Dio? dio, String? host, int? port, String? apiKey})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl:
                  'https://${host ?? AppConstants.defaultApiHost}:'
                  '${port ?? AppConstants.defaultApiPort}',
              connectTimeout: AppConstants.connectionTimeout,
              receiveTimeout: AppConstants.connectionTimeout,
              headers: {
                'Content-Type': 'application/json',
                if (apiKey != null && apiKey.isNotEmpty)
                  'Authorization': 'Bearer $apiKey',
              },
            ),
          );

  /// Storage-backed construction, mirroring SdkApiClient.storage: host,
  /// port and API key come from persisted settings with the compiled-in
  /// defaults as fallback.
  factory VoiceService.fromDefaults({StorageService? storage}) {
    String host = AppConstants.defaultApiHost;
    int port = AppConstants.defaultApiPort;
    String? apiKey;
    if (storage != null) {
      host = storage.getApiHost() ?? host;
      port = storage.getApiPort() ?? port;
      apiKey = storage.getApiKey();
    }
    if ((apiKey == null || apiKey.isEmpty) &&
        AppConstants.defaultApiKey.isNotEmpty) {
      apiKey = AppConstants.defaultApiKey;
    }
    return VoiceService(host: host, port: port, apiKey: apiKey);
  }

  void dispose() {
    _dio.close(force: true);
  }

  /// One bus/call round trip. Returns the inner `result` object.
  ///
  /// Error handling: a non-2xx response carries {"error": "<message>"} -
  /// the message is surfaced verbatim inside [VoiceServiceException]. A
  /// 200 envelope with an `error` key (defensive) is surfaced the same
  /// way. Network-level failures keep Dio's own message.
  Future<Map<String, dynamic>> _call(
    String method,
    Map<String, dynamic> params,
  ) async {
    final Response<dynamic> response;
    try {
      response = await _dio.post(
        '/api/v1/bus/call',
        data: {'method': method, 'params': params},
      );
    } on DioException catch (e) {
      throw _envelopeError(e);
    }
    final data = response.data;
    if (data is Map) {
      // Defensive: some daemon paths answer 200 with an error field.
      final err = data['error'];
      if (err is String && err.isNotEmpty) {
        throw VoiceServiceException(
          message: err,
          statusCode: response.statusCode ?? 0,
        );
      }
      final inner = data['result'];
      if (inner is Map<String, dynamic>) return inner;
      if (inner is Map) return Map<String, dynamic>.from(inner);
    }
    throw VoiceServiceException(
      message: 'unexpected bus/call envelope: $data',
      statusCode: response.statusCode ?? 0,
    );
  }

  /// Map a DioException onto the verbatim daemon error envelope.
  VoiceServiceException _envelopeError(DioException e) {
    final raw = e.response?.data;
    final status = e.response?.statusCode ?? 0;
    if (raw is Map) {
      final err = raw['error'];
      if (err is String && err.isNotEmpty) {
        return VoiceServiceException(message: err, statusCode: status);
      }
    }
    debugPrint('[voice] bus/call transport failure: $e');
    return VoiceServiceException(
      message: e.message ?? 'voice: request failed (HTTP $status)',
      statusCode: status,
    );
  }

  /// voice.local.open - open a local call for [employeeId].
  @override
  Future<VoiceLocalOpenResult> open(String employeeId, String label) async {
    final result = await _call('voice.local.open', {
      'employee_id': employeeId,
      'label': label,
    });
    return VoiceLocalOpenResult.fromJson(result);
  }

  /// voice.local.end - end one call. Double end is idempotent (ok=true).
  @override
  Future<bool> end(String callId) async {
    final result = await _call('voice.local.end', {'call_id': callId});
    return result['ok'] == true;
  }

  /// voice.local.status - live calls across BOTH legs.
  @override
  Future<List<VoiceSessionRow>> status() async {
    final result = await _call('voice.local.status', <String, dynamic>{});
    final calls = result['calls'];
    if (calls is! List) return const <VoiceSessionRow>[];
    return calls
        .whereType<Map>()
        .map((c) => VoiceSessionRow.fromJson(Map<String, dynamic>.from(c)))
        .toList(growable: false);
  }

  /// voice.local.events - one telemetry ring read (bughunt C-12).
  /// [limit] rides the params only when set; the daemon defaults it.
  @override
  Future<VoiceLocalEventsResult> events(String callId, {int? limit}) async {
    final result = await _call('voice.local.events', <String, dynamic>{
      'call_id': callId,
      if (limit != null) 'limit': limit,
    });
    return VoiceLocalEventsResult.fromJson(result);
  }
}

/// Bounded per-call telemetry poller over [VoiceServiceContract.events]
/// (bughunt C-12, the GUI twin of the CLI's voiceEventTracker).
///
/// One subscription per call id: a [Timer.periodic] at
/// [voiceEventsPollInterval] (500 ms, the CLI cadence) issues one
/// voice.local.events round-trip, each bounded by
/// [voiceEventsCallTimeout] (5 s, the CLI's C-11 bound) — a timed-out or
/// errored tick is abandoned silently and retried on the next cycle.
///
/// Semantics mirrored from cmd/meept/voice.go:
/// - the cursor advances by assignment (daemon echo semantics);
/// - [VoiceLocalEventsResult.truncated] surfaces as one gap note row;
/// - found=false ends the subscription ONLY after at least one event
///   was consumed (cursor > 0): a fresh call's ring is created lazily
///   by the first event, so a found=false at cursor 0 is not the end —
///   the CLI's silent-if-never-seen rule; a found=false at cursor > 0
///   is real eviction/teardown and appends one `feed ended` note row;
/// - a tick that starts after [stop] appends nothing (abandoned).
///
/// Rows land on [onRows] as immutable lists; the owner renders them.
class VoiceEventsSubscription {
  VoiceEventsSubscription({
    required this.voiceService,
    required this.callId,
    required this.onRows,
    this.limit,
  });

  final VoiceServiceContract voiceService;
  final String callId;
  final void Function(List<VoiceEventRow> rows) onRows;

  /// Optional read limit passed through to the daemon (the CLI leaves
  /// it unset; the GUI does too by default).
  final int? limit;

  int _cursor = 0;
  bool _consumedAny = false;
  bool _stopped = false;
  Timer? _timer;
  bool _ticking = false;

  /// Whether found=false at cursor > 0 has ended this feed.
  bool get ended => _ended;
  bool _ended = false;

  void start() {
    if (_stopped || _timer != null) return;
    _timer = Timer.periodic(voiceEventsPollInterval, (_) {
      unawaited(_tick());
    });
  }

  /// Stops the poller. A tick already in flight finishes its RPC but
  /// appends nothing after this returns.
  void stop() {
    _stopped = true;
    _timer?.cancel();
    _timer = null;
  }

  Future<void> _tick() async {
    if (_stopped || _ended || _ticking) return;
    _ticking = true;
    try {
      final result = await voiceService
          .events(callId, limit: limit)
          .timeout(voiceEventsCallTimeout);
      if (_stopped || _ended) return; // abandoned window: print nothing
      _apply(result);
    } on Exception {
      // Best-effort like the CLI: error/timeout retries on the next tick.
    } finally {
      _ticking = false;
    }
  }

  /// One good round-trip: append rows, note gaps, advance the cursor,
  /// and transition to the found=false end state (C-20) only after the
  /// feed was ever seen.
  void _apply(VoiceLocalEventsResult result) {
    if (_ended) return;
    final rows = <VoiceEventRow>[
      for (final ev in result.events) VoiceEventRow.fromEvent(ev),
    ];
    if (result.truncated) {
      rows.add(
        const VoiceEventRow(
          kind: VoiceEventRow.gapKind,
          line:
              'note: telemetry backlog truncated (poller fell behind; '
              'rows were lost)',
        ),
      );
    }
    if (rows.isNotEmpty) onRows(List.unmodifiable(rows));
    _consumedAny = _consumedAny || result.events.isNotEmpty;
    // Advance BY ASSIGNMENT: the daemon echoes the request cursor when
    // nothing new was returned, so this is correct in both directions.
    _cursor = result.cursor;
    if (!result.found && _consumedAny) {
      _ended = true;
      stop();
      onRows(const [
        VoiceEventRow(
          kind: VoiceEventRow.endedKind,
          line: 'telemetry feed ended (call no longer tracked)',
        ),
      ]);
    }
  }

  /// Current cursor (exposed for tests).
  int get cursor => _cursor;
}

/// One rendered telemetry line for the voice page (bughunt C-12): the
/// fully formatted operator line plus the raw kind (a UI-side color /
/// attention marker picks barge_in and the synthetic gap/ended kinds).
/// [VoiceEventRow.fromEvent] mirrors the CLI's voiceEventRender
/// (cmd/meept/voice.go) so both surfaces read the same.
class VoiceEventRow {
  static const gapKind = '_gap';
  static const endedKind = '_ended';

  final String kind;
  final String line;

  const VoiceEventRow({required this.kind, required this.line});

  factory VoiceEventRow.fromEvent(VoiceCallEvent ev) {
    return VoiceEventRow(kind: ev.kind, line: voiceEventLine(ev));
  }

  /// Whether this row is a barge-in (the page renders an attention
  /// marker for it).
  bool get isBargeIn => kind == 'barge_in';
}

/// The CLI-shaped one-line render of one telemetry event (mirrors
/// voiceEventRender in cmd/meept/voice.go). Unknown kinds render the raw
/// kind so a future daemon-side kind never disappears silently.
String voiceEventLine(VoiceCallEvent ev) {
  var at = ev.at;
  if (at.length >= 19) at = at.substring(11, 19);
  switch (ev.kind) {
    case 'heard':
      return '$at heard: ${ev.text}';
    case 'prompt_dispatch':
      return '$at thinking (${ev.words} words): ${ev.text}';
    case 'turn_final':
      return '$at replied (${ev.words} words, ${ev.ms}ms)';
    case 'barge_in':
      return '$at >> barge-in';
    case 'call_ended':
      return '$at call ended: ${ev.reason}';
    case 'prompt_held':
      return '$at held (peer speaking): ${ev.text}';
    case 'prompt_released':
      return '$at released: ${ev.text}';
    case 'prompt_dropped':
      return '$at dropped (${ev.reason}): ${ev.text}';
    default:
      return '$at ${ev.kind}';
  }
}
