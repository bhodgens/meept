/// Path utility: tilde (`~`) expansion for slash-command path arguments.
///
/// Exposes a single function, [expandTilde], that replaces a leading `~`
/// (or `~/`) with the user's home directory. Web-safe via conditional
/// imports — on web the function is a no-op (returns [path] unchanged)
/// because there is no filesystem or `HOME` environment variable.
///
/// Usage:
/// ```dart
/// import 'package:meept/core/path_utils.dart';
/// final expanded = expandTilde('~/git/foo'); // -> /Users/me/git/foo
/// ```
library;

import 'package:flutter/foundation.dart' show kIsWeb;

// Conditional import picks the native implementation on desktop and the
// web stub on web builds. The stub avoids `dart:io` entirely.
import 'path_expansion/io_tilde.dart'
    if (dart.library.html) 'path_expansion/io_tilde_web.dart'
    as platform;

/// Replace a leading `~` (or `~/`) in [path] with the home directory.
///
/// - On native platforms, the home directory comes from `Platform.environment`
///   (`HOME` on unix, `USERPROFILE` on Windows).
/// - On web, [path] is returned unchanged (no filesystem access).
/// - When [path] does not start with `~`, it is returned verbatim.
/// - When the home directory cannot be determined on a native platform, only
///   the leading `~` is stripped (so `/~git/foo` → `git/foo`), matching the
///   daemon-side fallback.
String expandTilde(String path) {
  if (kIsWeb) return path;
  if (!path.startsWith('~')) return path;
  final home = platform.homeDirectory();
  if (home == null || home.isEmpty) {
    // No home resolvable — strip the tilde prefix so downstream parsing
    // doesn't choke on a literal `~`.
    return path.startsWith('~/') ? path.substring(2) : path.substring(1);
  }
  // Normalise trailing separator so we never emit `//`.
  final sep = home.endsWith('/') ? '' : '/';
  return path.startsWith('~/') ? '$home$sep${path.substring(2)}' : home;
}
