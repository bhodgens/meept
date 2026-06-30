import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/verbosity_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub [SdkApiClient] that captures the last [setClientConfig] patch without
/// performing any network I/O. Used by provider-level tests that go through
/// [verbosityProvider] (and therefore through [sdkClientProvider]).
class _CapturingSdkClient extends SdkApiClient {
  _CapturingSdkClient() : super(host: 'localhost', port: 8081);

  Map<String, dynamic>? lastPatch;

  @override
  Future<void> setClientConfig(Map<String, dynamic> patch) async {
    lastPatch = Map<String, dynamic>.from(patch);
  }
}

void main() {
  group('verbosityProvider', () {
    test('initial value defaults to normal (1)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      expect(container.read(verbosityProvider), 1);
    });

    test('cycle rotates 1 -> 2 -> 0 -> 1', () {
      final stub = _CapturingSdkClient();
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWithValue(stub)],
      );
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 2);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 0);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), 1);
    });

    test('cycle through provider PATCHes setClientConfig', () async {
      final stub = _CapturingSdkClient();
      final container = ProviderContainer(
        overrides: [sdkClientProvider.overrideWithValue(stub)],
      );
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycle();
      expect(container.read(verbosityProvider), VerbosityLevel.verbose);

      // Fire-and-forget: allow the microtask to resolve before asserting.
      await Future<void>.delayed(Duration.zero);

      expect(stub.lastPatch, {
        'chat': {'verbosity': 'verbose'},
      });
    });

    test('VerbosityLevel.name returns correct strings', () {
      expect(VerbosityLevel.name(VerbosityLevel.quiet), 'quiet');
      expect(VerbosityLevel.name(VerbosityLevel.normal), 'normal');
      expect(VerbosityLevel.name(VerbosityLevel.verbose), 'verbose');
    });

    test('shouldEmitAgentEvent drops events with tier > current verbosity', () {
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 0), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 1), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 2), isFalse);
    });
  });

  group('VerbosityNotifier (direct)', () {
    test('cycle invokes persist callback with new level', () async {
      int? capturedLevel;
      final notifier = VerbosityNotifier(
        onPersist: (level) async {
          capturedLevel = level;
        },
      );
      addTearDown(notifier.dispose);

      expect(notifier.state, VerbosityLevel.normal);
      notifier.cycle();
      expect(notifier.state, VerbosityLevel.verbose);

      // Fire-and-forget: pump to let microtask resolve.
      await Future<void>.delayed(Duration.zero);
      expect(capturedLevel, VerbosityLevel.verbose);
    });

    test('cycle wraps quiet -> normal', () {
      final notifier = VerbosityNotifier();
      addTearDown(notifier.dispose);

      notifier.state = VerbosityLevel.quiet;
      notifier.cycle();
      expect(notifier.state, VerbosityLevel.normal);
    });

    test('cycle without callback does not crash', () {
      final notifier = VerbosityNotifier();
      addTearDown(notifier.dispose);

      notifier.cycle();
      expect(notifier.state, VerbosityLevel.verbose);
    });

    test('cycle swallows persist callback failure without reverting state', () async {
      final notifier = VerbosityNotifier(
        onPersist: (level) async {
          throw Exception('rpc down');
        },
      );
      addTearDown(notifier.dispose);

      notifier.cycle();
      // State must remain at the new level despite the persist failure.
      expect(notifier.state, VerbosityLevel.verbose);

      // Give the fire-and-forget future a chance to reject.
      await Future<void>.delayed(Duration.zero);
      expect(notifier.state, VerbosityLevel.verbose);
    });
  });
}
