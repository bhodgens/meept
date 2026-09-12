// NATIVE-ONLY FILE: Reads TLS certificate fingerprints from filesystem.
// Web has no filesystem access; certificate pinning is handled by the browser.
import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';

/// Validates the daemon's self-signed TLS certificate.
///
/// On macOS, the app runs in an App Sandbox that prevents reading
/// files outside the sandbox container. This means the PEM file at
/// `~/.meept/tls/cert.pem` is generally not readable at runtime.
///
/// Strategy (in priority order):
/// 1. If the fingerprint was successfully loaded from disk, pin to it.
/// 2. If the fingerprint could not be loaded (sandbox, missing file, etc.),
///    accept the certificate **only** for localhost connections. This is
///    safe because the daemon listens exclusively on localhost and
///    authentication is enforced via API keys.
class DaemonCertPinner {
  static String? _cachedFingerprint;
  static bool _loadAttempted = false;

  /// The cached certificate SHA-256 fingerprint fingerprint, when available.
  static String? get currentFingerprint => _cachedFingerprint;

  /// Load and cache the daemon cert's SHA-256 fingerprint.
  ///
  /// Reads the PEM file, extracts the base64 DER content, and hashes
  /// the DER bytes so the fingerprint is comparable to [X509Certificate.der].
  /// Returns null if the cert file cannot be found or read (e.g. due to
  /// App Sandbox restrictions).
  static Future<String?> loadFingerprint() async {
    if (_cachedFingerprint != null) return _cachedFingerprint;
    if (_loadAttempted) return _cachedFingerprint;
    _loadAttempted = true;
    _loadFingerprintSync();
    return _cachedFingerprint;
  }

  /// Synchronously load the fingerprint from disk.
  static void _loadFingerprintSync() {
    if (_cachedFingerprint != null) return;
    if (kIsWeb) return; // No filesystem access on web

    final homeDir = Platform.environment['HOME'];
    if (homeDir == null) return;

    // The daemon writes its self-signed cert to one of two layouts, depending
    // on the config shape:
    //   transport.http.tls_cert_file — "~/.meept/tls/cert.pem" in the shipped
    //   template; internal/config DefaultConfig uses "~/.meept/certs/tls.crt"
    //   (schema.go HTTPTransportConfig defaults).
    // Try both under $MEEPT_HOME (when set: internal/config/home.go) and the
    // real $HOME, so pinning stays enforced for either layout instead of
    // silently degrading to localhost-only trust.
    final meeptHome = Platform.environment['MEEPT_HOME'];
    final candidates = <String>[
      if (meeptHome != null && meeptHome.isNotEmpty) ...<String>[
        '$meeptHome/tls/cert.pem',
        '$meeptHome/certs/tls.crt',
      ],
      '$homeDir/.meept/tls/cert.pem',
      '$homeDir/.meept/certs/tls.crt',
    ];

    for (final certPath in candidates) {
      try {
        final certFile = File(certPath);
        if (!certFile.existsSync()) continue;
        final pemContent = certFile.readAsStringSync();
        final derBytes = _pemToDer(pemContent);
        _cachedFingerprint = sha256.convert(derBytes).toString();
        debugPrint(
          '[cert] Fingerprint loaded from $certPath: $_cachedFingerprint',
        );
        return;
      } catch (e) {
        // Cert not found or unreadable (App Sandbox, permissions, etc.).
        // Leaves fingerprint null — validateCert will fall back to
        // localhost-only trust.
        debugPrint('[cert] Failed to load fingerprint from $certPath: $e');
      }
    }
    debugPrint('[cert] No daemon cert found (tried ${candidates.join(", ")})');
  }

  /// Extract DER bytes from a PEM-encoded certificate string.
  static Uint8List _pemToDer(String pem) {
    final b64 = pem
        .split('\n')
        .where((line) => !line.startsWith('-----'))
        .join()
        .trim();
    return base64.decode(b64);
  }

  /// Clear cached fingerprint (for testing or after cert rotation).
  static void invalidate() {
    _cachedFingerprint = null;
    _loadAttempted = false;
  }

  /// Validate a presented certificate.
  ///
  /// Only allows localhost connections (127.0.0.1, ::1, localhost).
  ///
  /// If a fingerprint was loaded from disk, pins to it. Otherwise
  /// (e.g. under App Sandbox where the file is unreadable), accepts
  /// any certificate for localhost connections. This is acceptable
  /// because the daemon binds exclusively to localhost and security
  /// is enforced via API key authentication.
  static bool validateCert(X509Certificate cert, String host) {
    debugPrint(
      '[cert] validateCert called: host=$host, hasFingerprint=${_cachedFingerprint != null}',
    );

    // Only allow localhost connections.
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      debugPrint('[cert] REJECTED: non-localhost host=$host');
      return false;
    }

    // If we have a fingerprint, enforce pinning.
    if (_cachedFingerprint != null) {
      final actual = sha256.convert(cert.der).toString();
      final match = actual == _cachedFingerprint;
      debugPrint('[cert] Fingerprint check: match=$match');
      if (!match) {
        debugPrint('[cert] REJECTED: fingerprint mismatch (got: $actual)');
      }
      return match;
    }

    // No fingerprint available (App Sandbox, missing file, etc.).
    debugPrint('[cert] ACCEPTED: no fingerprint, localhost connection');
    return true;
  }
}
