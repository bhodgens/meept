import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/verbosity_provider.dart';

void main() {
  group('verbosityProvider', () {
    test('initial value defaults to normal (1)', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      expect(container.read(verbosityProvider), 1);
    });

    test('cycleVerbosity rotates 1 -> 2 -> 0 -> 1', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 2);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 0);

      container.read(verbosityProvider.notifier).cycleVerbosity();
      expect(container.read(verbosityProvider), 1);
    });

    test('shouldEmitAgentEvent drops events with tier > current verbosity', () {
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 0), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 1), isTrue);
      expect(shouldEmitAgentEvent(currentVerbosity: 1, eventTier: 2), isFalse);
    });
  });
}
