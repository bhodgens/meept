import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
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

    final homeDir = Platform.environment['HOME'];
    if (homeDir == null) return;

    final certPath = '$homeDir/.meept/tls/cert.pem';
    try {
      final certFile = File(certPath);
      if (!certFile.existsSync()) {
        debugPrint('[cert] Cert file not found: $certPath');
        return;
      }
      final pemContent = certFile.readAsStringSync();
      final derBytes = _pemToDer(pemContent);
      _cachedFingerprint = sha256.convert(derBytes).toString();
      debugPrint('[cert] Fingerprint loaded: $_cachedFingerprint');
    } catch (e) {
      // Cert not found or unreadable (App Sandbox, permissions, etc.).
      // Leaves fingerprint null — validateCert will fall back to
      // localhost-only trust.
      debugPrint('[cert] Failed to load fingerprint: $e');
    }
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
    // Only allow localhost connections.
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      return false;
    }

    // If we have a fingerprint, enforce pinning.
    if (_cachedFingerprint != null) {
      final actual = sha256.convert(cert.der).toString();
      return actual == _cachedFingerprint;
    }

    // No fingerprint available (App Sandbox, missing file, etc.).
    // Accept the cert for localhost — security is provided by API key auth.
    return true;
  }
}
