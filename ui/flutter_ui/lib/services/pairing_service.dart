// NATIVE+WEB: one-shot HTTP client for the daemon's first-run pairing
// handshake (issue #59). A distribution GUI build carries NO embedded API
// key; this service exchanges the daemon's one-time pairing code for the
// per-install dev key and hands it to StorageService.
//
// Security notes:
//   - The pairing endpoints are loopback-restricted daemon-side; we simply
//     POST to the same https endpoint the app already dials.
//   - The code is single use: a failed retry means the code was consumed or
//     expired — the user needs a fresh code from the daemon console.
//   - desktop (kIsWeb false) auto-pairing reads the local dev key file via
//     platformService (null on web); web always pairs explicitly.
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;

import '../core/constants.dart';
import 'pairing_http_native.dart'
    if (dart.library.js_interop) 'pairing_http_web.dart'
    if (dart.library.html) 'pairing_http_web.dart';
import 'storage_service.dart';

/// Result of one pairing attempt.
class PairingResult {
  /// True when the exchange succeeded and the key was persisted.
  final bool success;

  /// Human-facing (lowercase) failure reason for the pairing screen.
  final String? error;

  const PairingResult({required this.success, this.error});
}

class PairingService {
  /// Storage the obtained key is persisted into.
  final StorageService storage;

  /// Override endpoint (defaults to the configured host/port).
  final String? host;
  final int? port;

  PairingService({required this.storage, this.host, this.port});

  /// Whether this client needs pairing: no key in storage AND no embedded
  /// dart-define key (the legacy dev-workflow path).
  static bool needsPairing(StorageService storage) {
    final stored = storage.getApiKey();
    if (stored != null && stored.isNotEmpty) return false;
    return AppConstants.defaultApiKey.isEmpty;
  }

  /// Exchange [code] for the dev key and persist it.
  ///
  /// Returns a [PairingResult]; on failure [PairingResult.error] names the
  /// reason in lowercase for the pairing screen.
  Future<PairingResult> exchange(String code) async {
    final trimmed = code.trim();
    if (trimmed.isEmpty) {
      return const PairingResult(
        success: false,
        error: 'enter the one-time pairing code from the daemon console',
      );
    }

    final h = host ?? storage.getApiHost() ?? AppConstants.defaultApiHost;
    final p = port ?? storage.getApiPort() ?? AppConstants.defaultApiPort;
    final uri = Uri.parse('https://$h:$p/api/v1/pair/exchange');

    final body = await postJsonPairing(uri, {'code': trimmed});
    if (body == null) {
      return const PairingResult(
        success: false,
        error: 'cannot reach the daemon — is it running on this machine?',
      );
    }

    final error = body['error'] as String?;
    if (error != null) {
      final msg = body['message'] as String? ?? 'pairing rejected';
      return PairingResult(success: false, error: msg.toLowerCase());
    }

    final apiKey = body['api_key'] as String?;
    if (apiKey == null || apiKey.isEmpty) {
      return const PairingResult(
        success: false,
        error: 'daemon returned no key — restart the daemon and retry',
      );
    }

    await storage.setApiKey(apiKey);
    debugPrint('[pairing] key obtained and stored');
    return const PairingResult(success: true);
  }

  /// Auto-pair on desktop by reading the daemon's local dev key file
  /// ($HOME/.meept/dev_key, honoring MEEPT_HOME). NEVER on web: the browser
  /// has no filesystem, so web clients always pair explicitly with a code.
  ///
  /// Returns true when a key was found and stored.
  Future<bool> autoPairFromLocalKeyFile() async {
    if (kIsWeb) return false;
    // Delegate to the storage layer's existing dev-key-file read
    // (platformService returns null home on web).
    final key = await storage.tryReadDevKeyFile();
    if (key == null || key.isEmpty) return false;
    await storage.setApiKey(key);
    debugPrint('[pairing] paired from local dev key file');
    return true;
  }
}

// The minimal POST lives in the conditional-import split files
// (pairing_http_native.dart / pairing_http_web.dart) so no dart:io import
// lands in the web build.
