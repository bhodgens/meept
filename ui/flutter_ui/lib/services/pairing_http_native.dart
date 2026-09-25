// NATIVE-ONLY half of the pairing HTTP client: dio + IOHttpClientAdapter
// with the same localhost self-signed-cert acceptance DaemonCertPinner
// applies. Conditional-import target: never import directly; import
// pairing_service.dart.
import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:dio/io.dart';
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
      validateStatus: (status) => status != null && status < 500,
    ),
  );
  dio.httpClientAdapter = IOHttpClientAdapter(
    createHttpClient: () {
      final client = HttpClient()
        ..connectionTimeout = const Duration(seconds: 10);
      // The daemon's TLS cert is self-signed; pairing is loopback-only
      // daemon-side, so mirror DaemonCertPinner's localhost-only trust.
      client.badCertificateCallback = (cert, host, port) {
        final ok =
            host == 'localhost' || host == '127.0.0.1' || host == '::1';
        if (!ok) debugPrint('[pairing] rejected non-localhost cert for $host');
        return ok;
      };
      return client;
    },
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
