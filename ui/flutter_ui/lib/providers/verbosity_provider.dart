import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'providers.dart';

/// Verbosity levels mirror the TUI's VerbosityLevel enum
/// (internal/tui/app.go:52-58). Values:
///   0 = quiet   — only high-level completion events
///   1 = normal  — tool results + agent completions (default)
///   2 = verbose — everything including tool starts
class VerbosityLevel {
  const VerbosityLevel._();
  static const int quiet = 0;
  static const int normal = 1;
  static const int verbose = 2;

  static String name(int level) {
    switch (level) {
      case quiet:
        return 'quiet';
      case verbose:
        return 'verbose';
      default:
        return 'normal';
    }
  }
}

/// Optional persistence hook invoked after a successful [VerbosityNotifier.cycle]
/// with the newly-selected level. Implementations should fire-and-forget the
/// underlying RPC; failures are swallowed by [VerbosityNotifier.cycle] so the
/// UI never reverts.
typedef VerbosityPersistCallback = Future<void> Function(int level);

/// Current verbosity level. Cycle via [VerbosityNotifier.cycle].
///
/// Persistence wiring (Task 2.2): when constructed via [verbosityProvider], the
/// notifier receives a callback that PATCHes `/api/v1/config/client` with the
/// new verbosity. The callback is fire-and-forget — UI state is committed
/// synchronously and the RPC runs in the background.
class VerbosityNotifier extends StateNotifier<int> {
  final VerbosityPersistCallback? _onPersist;

  VerbosityNotifier({VerbosityPersistCallback? onPersist})
      : _onPersist = onPersist,
        super(VerbosityLevel.normal);

  /// Cycle 0 -> 1 -> 2 -> 0. Matches TUI Ctrl+V (app.go:727).
  ///
  /// After committing the new state, fires [_onPersist] (if provided) as a
  /// fire-and-forget background call. RPC failures are swallowed: the user
  /// sees their choice immediately and the next session loads whatever was
  /// last successfully persisted.
  void cycle() {
    final next = (state + 1) % 3;
    state = next;
    final cb = _onPersist;
    if (cb != null) {
      cb(next).catchError((Object _, StackTrace __) {
        // Best-effort persistence. Logged by caller's Dio interceptor if
        // present; UI state is NOT reverted on failure.
      });
    }
  }
}

final verbosityProvider =
    StateNotifierProvider<VerbosityNotifier, int>((ref) {
  return VerbosityNotifier(
    onPersist: (level) => ref.read(sdkClientProvider).setClientConfig({
      'chat': {'verbosity': VerbosityLevel.name(level)},
    }),
  );
});

/// Pure predicate used by ChatNotifier to filter agent events by tier.
/// Mirrors TUI app.go:1347: `if tier <= a.verbosity`.
bool shouldEmitAgentEvent({required int currentVerbosity, required int eventTier}) {
  return eventTier <= currentVerbosity;
}
