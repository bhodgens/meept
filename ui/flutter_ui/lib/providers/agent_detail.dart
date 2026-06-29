import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/api_models.dart';
import 'providers.dart' show sdkClientProvider;

/// Per-id cached [Agent] fetcher.
///
/// Riverpod caches each family provider by its id argument; subsequent reads
/// of the same id return the cached [AsyncValue] without re-invoking the
/// fetcher, as long as the [ProviderContainer] (or widget tree's
/// [ProviderScope]) is still alive.
///
/// To prefetch (warm the cache), simply `ref.read(agentDetailFamily('id'))`
/// without awaiting — Riverpod kicks off the future immediately and the
/// result is cached for subsequent `ref.watch` calls.
///
/// Sibling of [sessionDetailFamily]; see that provider for design rationale
/// (direct `FutureProvider.family` rather than via `cachedDetailFamily`).
final agentDetailFamily =
    FutureProvider.family<Agent, String>((ref, id) async {
  final client = ref.read(sdkClientProvider);
  final raw = await client.getAgent(id);
  return Agent.fromJson(raw);
});
