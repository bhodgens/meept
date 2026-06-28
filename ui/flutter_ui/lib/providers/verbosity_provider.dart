import 'package:flutter_riverpod/flutter_riverpod.dart';

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

/// Current verbosity level. Cycle via VerbosityNotifier.cycle().
/// Wiring (Ctrl+V + persistence) lands in HomeScreen per Task 3.3 of the plan.
class VerbosityNotifier extends StateNotifier<int> {
  VerbosityNotifier() : super(VerbosityLevel.normal);

  /// Cycle 0 -> 1 -> 2 -> 0. Matches TUI Ctrl+V (app.go:727).
  void cycle() {
    state = (state + 1) % 3;
  }
}

final verbosityProvider =
    StateNotifierProvider<VerbosityNotifier, int>((ref) => VerbosityNotifier());

/// Pure predicate used by ChatNotifier to filter agent events by tier.
/// Mirrors TUI app.go:1347: `if tier <= a.verbosity`.
bool shouldEmitAgentEvent({required int currentVerbosity, required int eventTier}) {
  return eventTier <= currentVerbosity;
}
