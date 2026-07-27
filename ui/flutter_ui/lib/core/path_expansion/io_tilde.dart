// Native (dart:io) implementation of tilde expansion helpers.
//
// This file is only imported on non-web builds via the conditional import in
// path_utils.dart. The web build receives the stub in io_tilde_web.dart
// instead. See CLAUDE.md "Flutter Multi-Platform (Web + Desktop)".
library path_expansion.io_tilde;

import 'dart:io' show Platform;

/// Resolve the current user's home directory from the environment.
///
/// Returns `HOME` (unix) or `USERPROFILE` (Windows). Returns null when
/// neither is set (extremely rare on real desktops).
String? homeDirectory() {
  return Platform.environment['HOME'] ?? Platform.environment['USERPROFILE'];
}
