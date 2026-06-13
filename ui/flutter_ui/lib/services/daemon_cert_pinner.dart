import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';

/// Pins the daemon's self-signed TLS certificate by comparing its
/// SHA-256 fingerprint against the cert file on disk.
class DaemonCertPinner {
  static String? _cachedFingerprint;

  /// Load and cache the daemon cert's SHA-256 fingerprint.
  ///
  /// Reads the PEM file, extracts the base64 DER content, and hashes
  /// the DER bytes so the fingerprint is comparable to [X509Certificate.der].
  /// Returns null if the cert file cannot be found or read.
  static Future<String?> loadFingerprint() async {
    if (_cachedFingerprint != null) return _cachedFingerprint;

    final homeDir = Platform.environment['HOME'];
    if (homeDir == null) return null;

    final certPath = '$homeDir/.meept/tls/cert.pem';
    try {
      final certFile = File(certPath);
      if (!await certFile.exists()) return null;
      final pemContent = await certFile.readAsString();
      // Extract base64 content between PEM headers and decode to DER.
      final derBytes = _pemToDer(pemContent);
      _cachedFingerprint = sha256.convert(derBytes).toString();
      return _cachedFingerprint;
    } catch (_) {
      return null;
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
  }

  /// Validate a presented certificate against the pinned fingerprint.
  ///
  /// Only allows localhost connections. Returns false if the cert
  /// fingerprint doesn't match or if no fingerprint has been loaded.
  static bool validateCert(X509Certificate cert, String host) {
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      return false;
    }
    if (_cachedFingerprint == null) {
      return false;
    }
    final actual = sha256.convert(cert.der).toString();
    return actual == _cachedFingerprint;
  }
}
