// Web stub for DaemonCertPinner
// All methods are no-ops since web browsers handle TLS certificates automatically.

import 'package:flutter/foundation.dart' show debugPrint;

/// Web stub - certificate pinning is handled by the browser.
class DaemonCertPinner {
  static String? get currentFingerprint => null;

  static Future<String?> loadFingerprint() async {
    debugPrint('[cert] Web: cert pinning not available');
    return null;
  }

  static void invalidate() {}

  static bool validateCert(dynamic cert, String host) {
    // On web, we trust the browser's TLS handling
    if (host != 'localhost' && host != '127.0.0.1' && host != '::1') {
      debugPrint('[cert] Web: non-localhost connection: $host');
      return true;  // Browser handles validation
    }
    return true;
  }
}
