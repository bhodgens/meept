// Native half of the external-URL opener (deep links, leaf 03).
//
// dart:io lives ONLY here (never at a top level of shared code); the web
// build never imports this file (platform_open_helpers.dart condition).
//
// macOS opens via `open <url>`; Linux via `xdg-open <url>`. Windows has no
// GUI build today; the call degrades to a lowercase log line.
import 'dart:io' show Platform, Process;

import 'package:flutter/foundation.dart' show debugPrint;

/// Open [url] with the platform's default handler. Returns true when a
/// launcher was spawned. Failures log lowercase and return false — the
/// deep-link path never throws on an unopenable URL.
Future<bool> openExternal(String url) async {
  try {
    if (Platform.isMacOS) {
      final result = await Process.run('open', <String>[url]);
      return result.exitCode == 0;
    }
    if (Platform.isLinux) {
      final result = await Process.run('xdg-open', <String>[url]);
      return result.exitCode == 0;
    }
    debugPrint('[deep-link] no external opener for this platform');
    return false;
  } catch (e) {
    debugPrint('[deep-link] open failed: $e');
    return false;
  }
}
