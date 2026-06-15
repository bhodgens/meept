import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/core/constants.dart';
import 'package:meept_ui/services/storage_service.dart';

void main() {
  // Sprint 4 / S7-H-Key: getApiKey() must return String? (nullable), not throw.
  // When no key is configured and AppConstants.defaultApiKey is empty
  // (release build), the method returns null.
  //
  // We can't fully control --dart-define in `flutter test`, so we verify:
  // 1. The method's return type is nullable (compiles when assigned to String?)
  // 2. The defaultApiKey constant is accessible
  // 3. Calling getApiKey before init() returns either null or the dart-define
  //    default (which is '' in release builds, the dev key only when injected).

  test('getApiKey returns String? (nullable) — compiles when assigned to null',
      () {
    // Use a fresh StorageService via the singleton. We don't await init(),
    // so _cachedApiKey is null and _prefs is null.
    final String? apiKey = StorageService.instance.getApiKey();
    // In test builds --dart-define is not set, so defaultApiKey == ''.
    // With empty default, the method returns null.
    if (AppConstants.defaultApiKey.isEmpty) {
      expect(apiKey, isNull,
          reason: 'getApiKey should return null when no key is configured '
              'and defaultApiKey is empty (release-like)');
    } else {
      // If dart-define is set (e.g. dev workflow), it returns the dev key.
      expect(apiKey, AppConstants.defaultApiKey);
    }
  });

  test('defaultApiKey is empty in test builds (no --dart-define)', () {
    // Tests run without --dart-define so the default is ''.
    expect(AppConstants.defaultApiKey, '',
        reason: 'Tests should not have the dev key injected; if this fails, '
            'the test harness was invoked with --dart-define, which masks '
            'misconfiguration. See S7-H-Key.');
  });
}
