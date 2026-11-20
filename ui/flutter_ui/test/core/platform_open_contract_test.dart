// Regression pin for the web-build symbol contract (F-ORCH-18).
//
// platform_open_helpers.dart conditionally exports platform_open_web.dart
// on web; platform_service.dart calls openhelpers.openExternal. The web
// half once defined openExternalWeb instead, breaking EVERY web build
// ("Error: Method not found: 'openExternal'"). VM tests exercise the
// native half; the web half is pinned by `flutter build web --release`.
// This test pins the shared symbol name + never-throws contract on the
// half that is testable headlessly, so a future rename of the export
// contract fails here instead of only in the web build.
import 'package:flutter_test/flutter_test.dart';

import 'package:meept_ui/core/platform/platform_open_helpers.dart'
    as openhelpers;

void main() {
  group('openExternal export contract (F-ORCH-18)', () {
    test('openhelpers.openExternal exists and returns a Future<bool>', () {
      // Compile-time pin: the conditional export must expose exactly
      // `openExternal` (the name platform_service.dart invokes).
      final Future<bool> Function(String) fn = openhelpers.openExternal;
      expect(fn, isNotNull);
    });

    test('openExternal never throws on an unopenable URL', () async {
      // Native half shells out to `open`/`xdg-open`; an unopenable URL
      // must degrade to a bool, never throw (deep-link path contract).
      final bool ok = await openhelpers.openExternal('');
      expect(ok, isA<bool>());
    });
  });
}
