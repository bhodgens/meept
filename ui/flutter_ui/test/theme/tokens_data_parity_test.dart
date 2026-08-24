import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/theme/tokens_data.dart';

/// Cross-surface parity guard: tokens_data.dart is a const copy of
/// theme/tokens.json5 (the single source of truth embedded into the Go
/// binary). This test fails when the copy drifts, so the two frontends can
/// never disagree on role values.
///
/// The parser is deliberately lenient: it strips // comments and trailing
/// commas, then reads quoted keys/values. It does not need full JSON5.
void main() {
  // Tests run from ui/flutter_ui; the token file lives at the repo root.
  const candidates = [
    '../../theme/tokens.json5',
    '../theme/tokens.json5',
    'theme/tokens.json5',
  ];

  late Map<String, dynamic> canonical;

  setUpAll(() {
    String? raw;
    for (final path in candidates) {
      final f = File(path);
      if (f.existsSync()) {
        raw = f.readAsStringSync();
        break;
      }
    }
    assert(raw != null,
        'theme/tokens.json5 not found relative to test cwd — run from repo');

    var stripped = raw!.replaceAll(RegExp(r'//[^\n]*'), '');
    stripped = stripped.replaceAll(RegExp(r',(\s*[}\]])'), r'1');
    canonical = jsonDecode(stripped) as Map<String, dynamic>;
  });

  test('variants match exactly', () {
    expect(kTokensData.keys.toSet(), canonical.keys.toSet());
  });

  test('every role matches byte-for-byte', () {
    for (final entry in canonical.entries) {
      final variant = entry.key;
      final roles = entry.value as Map<String, dynamic>;
      expect(kTokensData[variant], isNotNull,
          reason: 'variant $variant missing from tokens_data.dart');
      for (final role in roles.keys) {
        expect(
          kTokensData[variant]![role],
          roles[role],
          reason: '$variant.$role diverged from tokens.json5',
        );
      }
      expect(kTokensData[variant]!.keys.toSet(), roles.keys.toSet(),
          reason: '$variant has a different role set than tokens.json5');
    }
  });
}
