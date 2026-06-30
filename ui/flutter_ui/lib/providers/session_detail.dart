import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/api_models.dart';
import 'providers.dart' show sdkClientProvider;

/// Per-id cached [Session] fetcher.
///
/// Riverpod caches each family provider by its id argument; subsequent reads
/// of the same id return the cached [AsyncValue] without re-invoking the
/// fetcher, as long as the [ProviderContainer] (or widget tree's
/// [ProviderScope]) is still alive.
///
/// To prefetch (warm the cache), simply `ref.read(sessionDetailFamily('id'))`
/// without awaiting — Riverpod kicks off the future immediately and the
/// result is cached for subsequent `ref.watch` calls.
///
/// Implemented as a direct `FutureProvider.family` because Riverpod's family
/// already provides per-id caching, so an additional wrapper would add no
/// benefit.
final sessionDetailFamily =
    FutureProvider.family<Session, String>((ref, id) async {
  final client = ref.read(sdkClientProvider);
  final raw = await client.getSession(id);
  return Session.fromJson(raw);
});
