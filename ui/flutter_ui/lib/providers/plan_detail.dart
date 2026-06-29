import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/api_models.dart';
import 'providers.dart' show sdkClientProvider;

/// Per-id cached [Plan] fetcher.
///
/// Riverpod caches each family provider by its id argument; subsequent reads
/// of the same id return the cached [AsyncValue] without re-invoking the
/// fetcher, as long as the [ProviderContainer] (or widget tree's
/// [ProviderScope]) is still alive.
///
/// To prefetch (warm the cache), simply `ref.read(planDetailFamily('id'))`
/// without awaiting — Riverpod kicks off the future immediately and the
/// result is cached for subsequent `ref.watch` calls.
///
/// Sibling of [sessionDetailFamily]; see that provider for design rationale
/// (direct `FutureProvider.family` rather than via `cachedDetailFamily`).
final planDetailFamily =
    FutureProvider.family<Plan, String>((ref, id) async {
  final client = ref.read(sdkClientProvider);
  final raw = await client.getPlan(id);
  return Plan.fromJson(raw);
});
