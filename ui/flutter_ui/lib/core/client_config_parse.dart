/// Tolerant JSON5-ish decoder for daemon config file bodies.
///
/// Extracted from the client prefs editor so non-UI consumers (e.g. the
/// chat provider's liveness-timeout fetch) can parse the same
/// GET /api/v1/config/client payload without a widget-library import.
/// The daemon stores JSON5; most real files are strict JSON so plain
/// [jsonDecode] succeeds. When it does not (comments, trailing commas,
/// unquoted keys), fall back to a conservative cleanup pass before
/// retrying. Returns {} when nothing parses.
library;

import 'dart:convert';

Map<String, dynamic> parseClientConfig(String raw) {
  Map<String, dynamic> tryDecode(String s) {
    final v = jsonDecode(s);
    if (v is Map<String, dynamic>) return v;
    if (v is Map) return v.map((k, val) => MapEntry('$k', val));
    throw const FormatException('not an object');
  }

  try {
    return tryDecode(raw);
  } catch (_) {}

  var cleaned = raw
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//[^\n]*'), '');
  cleaned = cleaned.replaceFirst(RegExp(r'^[^\{]*'), '');
  cleaned = cleaned.replaceAllMapped(
    RegExp(r',\s*([}\]])'),
    (m) => m.group(1)!,
  );
  cleaned = cleaned.replaceAllMapped(
    RegExp(r'([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)\s*:'),
    (m) => '${m.group(1)}"${m.group(2)}":',
  );
  return tryDecode(cleaned);
}
