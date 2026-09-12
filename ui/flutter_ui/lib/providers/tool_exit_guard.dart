import 'package:flutter_riverpod/flutter_riverpod.dart';

/// A panel's asynchronous veto over leaving that panel.
///
/// Same shape as `ToolPanelShell.exitGuard`, so a panel hands the same
/// closure to the shell (back control and esc) and to the registry below
/// (the hamburger menu, a tab switch). `false` means "keep the panel and
/// its state", `true` means "the exit may proceed".
typedef ToolExitGuard = Future<bool> Function();

/// The exit guard of the panel that is currently open, if it has unsaved
/// state.
///
/// A panel registers its guard while mounted and releases it on dispose, so
/// one registration serves every path that can leave the panel:
///   - `ToolPanelShell`'s back control and esc,
///   - the hamburger menu's tool switch (`openToolFromMenu`),
///   - a home tab switch that would drop the embedded chat-tab tool slot.
///
/// A plain object behind a [Provider] rather than reactive state: the guard
/// is read imperatively at the moment of an exit attempt, so registering
/// and releasing must not rebuild anything (and cannot: nothing watches
/// this provider).
class ToolExitGuardRegistry {
  ToolExitGuard? _guard;

  /// The registered guard, or null when the open panel has nothing to lose.
  ToolExitGuard? get guard => _guard;

  /// Make [guard] the active veto.
  ///
  /// Last writer wins, so a panel opening on top of another takes over.
  void register(ToolExitGuard guard) => _guard = guard;

  /// Drop the registration owned by [guard].
  ///
  /// Only clears the registration while [guard] is still the active one: a
  /// panel disposing itself must never clear a guard a newer panel
  /// registered.
  void release(ToolExitGuard guard) {
    if (identical(_guard, guard)) _guard = null;
  }

  /// Ask the registered guard whether the open panel may be left, and
  /// release the registration when it allows the exit.
  ///
  /// Returns true when nothing is registered (nothing to lose: no dialog,
  /// no state change). Releasing on an allowed exit is what keeps one
  /// switch from asking twice: the route change that follows the answer
  /// must not consult a guard for edits the user just agreed to discard.
  Future<bool> requestExit() async {
    final guard = _guard;
    if (guard == null) return true;
    final allowed = await guard();
    if (allowed) release(guard);
    return allowed;
  }
}

/// Holds the exit guard of the panel that is currently open.
final toolExitGuardProvider = Provider<ToolExitGuardRegistry>(
  (ref) => ToolExitGuardRegistry(),
);
