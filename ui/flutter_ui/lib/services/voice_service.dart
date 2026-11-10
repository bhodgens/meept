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
}

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
}
