// WEB half of the pairing HTTP client. dio runs on web too; the browser
// handles TLS and CORS (the daemon answers loopback origins permissively).
// Conditional-import target: never import directly; import
// pairing_service.dart.
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart' show debugPrint;

/// POST [payload] as JSON to [uri]; returns the decoded JSON body, or null
/// when the daemon is unreachable.
Future<Map<String, dynamic>?> postJsonPairing(
  Uri uri,
  Map<String, dynamic> payload,
) async {
  final dio = Dio(
    BaseOptions(
      connectTimeout: const Duration(seconds: 10),
      // The daemon's self-signed cert is a browser concern on web; no
      // adapter override (IOHttpClientAdapter is native-only).
      validateStatus: (status) => status != null && status < 500,
    ),
  );
  try {
    final resp = await dio.post<Map<String, dynamic>>(
      uri.toString(),
      data: jsonEncode(payload),
      options: Options(headers: {'Content-Type': 'application/json'}),
    );
    return resp.data ?? <String, dynamic>{};
  } catch (e) {
    debugPrint('[pairing] exchange failed: $e');
    return null;
  }
}
