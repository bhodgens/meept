import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../providers/providers.dart';
import '../../services/sdk_client.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// One structured preference row.
typedef _Pref = ({
  String key,
  String label,
  _PrefKind kind,
  List<String>? choices,
});

enum _PrefKind { boolToggle, intField, choice }

const _prefs = <_Pref>[
  (
    key: 'chat.scroll_speed',
    label: 'chat scroll speed',
    kind: _PrefKind.intField,
    choices: null,
  ),
  (
    key: 'chat.auto_copy_on_release',
    label: 'auto-copy on release',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
  (
    key: 'chat.verbosity',
    label: 'chat verbosity',
    kind: _PrefKind.choice,
    choices: ['quiet', 'normal', 'verbose'],
  ),
  (
    key: 'gui.layout',
    label: 'gui layout',
    kind: _PrefKind.choice,
    choices: ['toptabs', 'sidebar'],
  ),
  (
    key: 'rendering.markdown',
    label: 'markdown rendering',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
  (
    key: 'rendering.syntax_highlighting',
    label: 'syntax highlighting',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
  (
    key: 'rendering.sidebar_animation',
    label: 'sidebar animation',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
  (
    key: 'rendering.word_wrap',
    label: 'word wrap',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
  (
    key: 'session.auto_resume',
    label: 'auto-resume last session',
    kind: _PrefKind.boolToggle,
    choices: null,
  ),
];

/// Parse the client config text returned by GET /api/v1/config/client.
///
/// The daemon stores JSON5; most real files are strict JSON so plain
/// [jsonDecode] succeeds. When it does not (comments, trailing commas,
/// unquoted keys), fall back to a conservative cleanup pass before
/// retrying. Returns {} when nothing parses — the editor then shows
/// defaults rather than crashing.
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

/// Structured editor for GUI-relevant blocks of client.json5.
///
/// Loads via GET /api/v1/config/client and applies per-field RFC 7396
/// merge-patches via PATCH /api/v1/config/client. Each change saves
/// immediately; the daemon merges atomically and preserves unknown keys.
class ClientPrefsEditor extends ConsumerStatefulWidget {
  const ClientPrefsEditor({super.key});

  @override
  ConsumerState<ClientPrefsEditor> createState() => _ClientPrefsEditorState();
}

class _ClientPrefsEditorState extends ConsumerState<ClientPrefsEditor> {
  late final SdkApiClient _client;
  final Map<String, TextEditingController> _intControllers = {};
  final Map<String, String?> _choiceValues = {};

  bool _isLoading = true;
  bool _isSaving = false;
  String? _error;
  Map<String, dynamic> _config = {};

  @override
  void initState() {
    super.initState();
    _client = ref.read(sdkClientProvider);
    _load();
  }

  dynamic _readValue(String key) {
    dynamic node = _config;
    for (final part in key.split('.')) {
      if (node is Map && node.containsKey(part)) {
        node = node[part];
      } else {
        return null;
      }
    }
    return node;
  }

  /// Build a nested merge-patch fragment from a dotted key:
  /// `'a.b' , 5` -> `{'a': {'b': 5}}`.
  static Map<String, dynamic> buildPatch(String key, dynamic value) {
    final parts = key.split('.');
    var patch = <String, dynamic>{parts.last: value};
    for (var i = parts.length - 2; i >= 0; i--) {
      patch = <String, dynamic>{parts[i]: patch};
    }
    return patch;
  }

  Future<void> _load() async {
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final raw = await _client.getClientConfig();
      if (!mounted) return;
      _applyConfig(parseClientConfig(raw));
      setState(() {
        _isLoading = false;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  void _applyConfig(Map<String, dynamic> parsed) {
    setState(() {
      _config = parsed;
      for (final p in _prefs) {
        if (p.kind == _PrefKind.intField) {
          _intControllers[p.key]?.dispose();
          final v = _readValue(p.key);
          _intControllers[p.key] = TextEditingController(
            text: v == null ? '' : '$v',
          );
        } else if (p.kind == _PrefKind.choice) {
          final v = _readValue(p.key);
          _choiceValues[p.key] = v?.toString();
        }
      }
    });
  }

  Future<void> _saveField(String key, dynamic value) async {
    setState(() => _isSaving = true);
    try {
      await _client.setClientConfig(buildPatch(key, value));
      // Re-fetch so the form reflects the true merged file.
      final raw = await _client.getClientConfig();
      if (!mounted) return;
      _applyConfig(parseClientConfig(raw));
      setState(() {
        _isSaving = false;
        _error = null;
      });
      // Live-apply prefs that the running app reads from providers.
      if (key == 'gui.layout' && mounted) {
        final notifier = ref.read(guiLayoutProvider.notifier);
        await notifier.set(value as String);
      }
      if (key == 'chat.verbosity' && mounted) {
        // Map the string level to the provider's int enum and apply via
        // setLevel. persist: false — this method already PATCHed; the
        // notifier's hook would duplicate the write.
        final level = switch (value as String) {
          'quiet' => VerbosityLevel.quiet,
          'verbose' => VerbosityLevel.verbose,
          _ => VerbosityLevel.normal,
        };
        ref.read(verbosityProvider.notifier).setLevel(level, persist: false);
      }
      if (mounted) {
        // Rendering prefs: keep the live provider in sync so chat
        // re-renders immediately (markdown, word wrap, auto-resume).
        final rp = ref.read(renderingPrefsProvider.notifier);
        if (key == 'rendering.markdown') {
          rp.setMarkdown(value as bool);
        } else if (key == 'rendering.word_wrap') {
          rp.setWordWrap(value as bool);
        } else if (key == 'session.auto_resume') {
          rp.setAutoResume(value as bool);
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isSaving = false;
          _error = e.toString();
        });
      }
    }
  }

  @override
  void dispose() {
    for (final c in _intControllers.values) {
      c.dispose();
    }
    super.dispose();
  }

  InputDecoration _decoration(String label) => InputDecoration(
    labelText: label,
    labelStyle: CyberpunkTypography.bodySmall.copyWith(
      color: CyberpunkColors.lightGray,
    ),
    isDense: true,
    filled: true,
    fillColor: CyberpunkColors.black,
    border: OutlineInputBorder(
      borderRadius: BorderRadius.circular(6),
      borderSide: const BorderSide(color: CyberpunkColors.midGray),
    ),
    enabledBorder: OutlineInputBorder(
      borderRadius: BorderRadius.circular(6),
      borderSide: const BorderSide(color: CyberpunkColors.midGray),
    ),
    focusedBorder: OutlineInputBorder(
      borderRadius: BorderRadius.circular(6),
      borderSide: const BorderSide(
        color: CyberpunkColors.orangePrimary,
        width: 1.5,
      ),
    ),
    errorStyle: const TextStyle(color: CyberpunkColors.redAlert, fontSize: 10),
  );

  Widget _controlFor(_Pref p) {
    switch (p.kind) {
      case _PrefKind.boolToggle:
        final v = _readValue(p.key);
        return Switch(
          value: v is bool ? v : false,
          activeThumbColor: CyberpunkColors.orangePrimary,
          onChanged: _isSaving || _isLoading
              ? null
              : (v) => _saveField(p.key, v),
        );
      case _PrefKind.intField:
        return TextField(
          controller: _intControllers[p.key],
          keyboardType: TextInputType.number,
          style: CyberpunkTypography.bodySmall.copyWith(
            fontFamily: 'SourceCodePro',
          ),
          decoration: _decoration(p.label),
          onSubmitted: (text) {
            final v = int.tryParse(text.trim());
            if (v != null) _saveField(p.key, v);
          },
        );
      case _PrefKind.choice:
        final current = _choiceValues[p.key] ?? _readValue(p.key)?.toString();
        final choices = p.choices ?? const <String>[];
        return DropdownButton<String>(
          value: choices.contains(current) ? current : null,
          isDense: true,
          isExpanded: true,
          dropdownColor: CyberpunkColors.darkGray,
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.lightGray,
          ),
          underline: const SizedBox.shrink(),
          items: choices
              .map((c) => DropdownMenuItem(value: c, child: Text(c)))
              .toList(),
          onChanged: _isSaving || _isLoading
              ? null
              : (v) {
                  if (v == null) return;
                  setState(() => _choiceValues[p.key] = v);
                  _saveField(p.key, v);
                },
        );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: CyberpunkColors.midGray)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.tune, color: CyberpunkColors.blueInfo, size: 16),
              const SizedBox(width: 8),
              Text(
                'client preferences',
                style: CyberpunkTypography.label.copyWith(
                  color: CyberpunkColors.blueInfo,
                ),
              ),
              const Spacer(),
              if (_isSaving)
                const SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.black,
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            '// client.json5 — each change saves immediately via merge-patch; other keys preserved',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.midGray,
              fontFamily: 'SourceCodePro',
              fontSize: 10,
            ),
          ),
          const SizedBox(height: 8),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                'error: $_error',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.redAlert,
                ),
              ),
            ),
          if (_isLoading)
            const Center(
              child: SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  valueColor: AlwaysStoppedAnimation<Color>(
                    CyberpunkColors.orangePrimary,
                  ),
                ),
              ),
            )
          else
            ..._prefs.map(
              (p) => Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  children: [
                    SizedBox(
                      width: 220,
                      child: Text(
                        p.label,
                        style: CyberpunkTypography.bodySmall.copyWith(
                          color: CyberpunkColors.lightGray,
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(child: _controlFor(p)),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}
