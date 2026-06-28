import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Signature for an async detail fetcher keyed by an opaque id.
///
/// Implementations typically wrap an SDK call such as
/// `sdkClient.getSession(id)` or `sdkClient.getAgent(id)`.
typedef DetailFetcher<T> = Future<T> Function(String id);

/// Build a family of per-id [AsyncValue] providers backed by [fetcher].
///
/// Riverpod caches each family provider by its id argument; subsequent reads
/// of the same id return the cached [AsyncValue] without re-invoking the
/// fetcher, as long as the [ProviderContainer] (or widget tree's
/// [ProviderScope]) is still alive.
///
/// Usage:
/// ```dart
/// final sessionDetailFamily = cachedDetailFamily<Session>(
///   (id) async => sdkClient.getSession(id),
/// );
///
/// // in a widget:
/// final detail = ref.watch(sessionDetailFamily('sess-1'));
/// ```
///
/// To prefetch (warm the cache), simply `ref.read(family('id'))` without
/// awaiting — Riverpod kicks off the future immediately and the result is
/// cached for subsequent `ref.watch` calls.
CachedDetailFamily<T> cachedDetailFamily<T>(DetailFetcher<T> fetcher) {
  return CachedDetailFamily<T>(fetcher);
}

/// Wraps a [FutureProvider.family] keyed by `String` id.
///
/// Exposes a single [call] method (invoked via `family('id')` syntax) that
/// returns the [ProviderListenable] suitable for `ref.watch` / `ref.read`.
/// Riverpod's family mechanism handles per-id caching automatically; the
/// fetcher is invoked at most once per id per [ProviderContainer].
class CachedDetailFamily<T> {
  final DetailFetcher<T> fetcher;
  late final ProviderListenable<AsyncValue<T>> Function(String id) _provider;

  CachedDetailFamily(this.fetcher) {
    final inner = FutureProvider.family<T, String>((ref, id) async {
      return fetcher(id);
    });
    // Wrap in an explicit closure to avoid implicit-call tear-off lint and
    // to make the contract explicit (single-arg String → ProviderListenable).
    _provider = (String id) => inner(id);
  }

  /// Returns the [ProviderListenable] for [id].
  ///
  /// Use as `ref.watch(family('id'))` or `container.read(family('id'))`.
  /// Riverpod caches the underlying provider by id, so the fetcher runs at
  /// most once per id per [ProviderContainer].
  ProviderListenable<AsyncValue<T>> call(String id) => _provider(id);
}
