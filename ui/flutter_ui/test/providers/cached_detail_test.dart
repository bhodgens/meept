import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/cached_detail.dart';

void main() {
  group('CachedDetail', () {
    test('first read triggers fetch; second read returns cache', () async {
      var fetchCalls = 0;
      String? fetchedId;
      final family = cachedDetailFamily<String>((id) async {
        fetchCalls++;
        fetchedId = id;
        return 'detail-for-$id';
      });

      final container = ProviderContainer();
      addTearDown(container.dispose);

      final first = container.read(family('item-1'));
      expect(first.isLoading, isTrue);
      await Future.delayed(Duration.zero);
      final settled = container.read(family('item-1'));
      expect(settled.hasValue, isTrue);
      expect(settled.value, 'detail-for-item-1');
      expect(fetchedId, 'item-1');
      expect(fetchCalls, 1);

      final again = container.read(family('item-1'));
      expect(again.value, 'detail-for-item-1');
      expect(fetchCalls, 1);
    });

    test('prefetch warms cache without blocking caller', () async {
      var fetchCalls = 0;
      final family = cachedDetailFamily<String>((id) async {
        fetchCalls++;
        await Future.delayed(const Duration(milliseconds: 5));
        return 'warmed-$id';
      });

      final container = ProviderContainer();
      addTearDown(container.dispose);

      container.read(family('pre-1'));
      await Future.delayed(const Duration(milliseconds: 20));
      expect(fetchCalls, 1);

      final cached = container.read(family('pre-1'));
      expect(cached.value, 'warmed-pre-1');
      expect(fetchCalls, 1);
    });
  });
}
