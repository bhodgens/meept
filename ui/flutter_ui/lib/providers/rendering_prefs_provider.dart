import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/sdk_client.dart';

/// Live GUI-rendering preferences mirrored from client.json5.
///
/// Loaded once at startup from GET /api/v1/config/client (best-effort;
/// failures keep the defaults) and mutated live by the settings panel's
/// [ClientPrefsEditor]. Widgets watch individual fields so a settings
/// change re-renders the affected UI immediately.
class RenderingPrefs {
  const RenderingPrefs({
    this.markdown = true,
    this.wordWrap = true,
    this.autoResume = true,
    this.useDeviceTimeForQuota = false,
  });

  final bool markdown;
  final bool wordWrap;
  final bool autoResume;

  /// M9 timezone convention: when false (the default) agent quota HH:MM
  /// timestamps render the DAEMON's wall-clock (the offset embedded in the
  /// wire RFC3339); when true they render in the device zone. Mirrors the
  /// TUI's client.json5 `rendering.time_display: "daemon"|"local"`.
  final bool useDeviceTimeForQuota;

  RenderingPrefs copyWith({
    bool? markdown,
    bool? wordWrap,
    bool? autoResume,
    bool? useDeviceTimeForQuota,
  }) {
    return RenderingPrefs(
      markdown: markdown ?? this.markdown,
      wordWrap: wordWrap ?? this.wordWrap,
      autoResume: autoResume ?? this.autoResume,
      useDeviceTimeForQuota:
          useDeviceTimeForQuota ?? this.useDeviceTimeForQuota,
    );
  }
}

class RenderingPrefsNotifier extends StateNotifier<RenderingPrefs> {
  RenderingPrefsNotifier() : super(const RenderingPrefs());

  /// Fetch prefs from the daemon config. Safe to call before the daemon
  /// is reachable — errors leave defaults in place.
  Future<void> load(SdkApiClient client) async {
    try {
      final raw = await client.getClientConfig();
      final parsed = _parseConfig(raw);
      final rendering = parsed['rendering'];
      final session = parsed['session'];
      state = RenderingPrefs(
        markdown: rendering is Map && rendering['markdown'] is bool
            ? rendering['markdown'] as bool
            : true,
        wordWrap: rendering is Map && rendering['word_wrap'] is bool
            ? rendering['word_wrap'] as bool
            : true,
        autoResume: session is Map && session['auto_resume'] is bool
            ? session['auto_resume'] as bool
            : true,
        // M9: rendering.time_display is "daemon" | "local" (default
        // "daemon"). Anything but an explicit "local" keeps the default.
        useDeviceTimeForQuota:
            rendering is Map && rendering['time_display'] is String
            ? rendering['time_display'] as String == 'local'
            : false,
      );
    } catch (_) {
      // Offline / unreachable — defaults already set.
    }
  }

  void setMarkdown(bool v) => state = state.copyWith(markdown: v);
  void setWordWrap(bool v) => state = state.copyWith(wordWrap: v);
  void setAutoResume(bool v) => state = state.copyWith(autoResume: v);
  void setUseDeviceTimeForQuota(bool v) =>
      state = state.copyWith(useDeviceTimeForQuota: v);
}

/// Lenient parse of the client config text: strips JSON5 comments and
/// trailing commas before decoding. Mirrors parseClientConfig in
/// client_prefs_editor.dart but kept local to avoid feature→feature imports.
Map<String, dynamic> _parseConfig(String raw) {
  var cleaned = raw
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//[^\n]*'), '');
  cleaned = cleaned.replaceFirst(RegExp(r'^[^\{]*'), '');
  cleaned = cleaned.replaceAll(RegExp(r',\s*([}\]])'), r'$1');
  try {
    final v = jsonDecode(cleaned);
    if (v is Map<String, dynamic>) return v;
    if (v is Map) return v.map((k, val) => MapEntry('$k', val));
    return {};
  } catch (_) {
    return {};
  }
}

final renderingPrefsProvider =
    StateNotifierProvider<RenderingPrefsNotifier, RenderingPrefs>((ref) {
      return RenderingPrefsNotifier();
    });
