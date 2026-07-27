// Web stub for tilde expansion helpers.
//
// Web has no filesystem access and no HOME environment variable, so the home
// directory is always null. [expandTilde] in path_utils.dart treats a null
// home as a no-op on web via its `kIsWeb` short-circuit, so this stub exists
// purely to satisfy the conditional import.
library path_expansion.io_tilde_web;

/// Always null on web — no filesystem or environment access.
String? homeDirectory() => null;
