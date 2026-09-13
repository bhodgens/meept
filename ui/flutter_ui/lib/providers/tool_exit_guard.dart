import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

/// A panel's asynchronous veto over leaving that panel.
///
/// Same shape as `ToolPanelShell.exitGuard`, so a panel hands the same
/// closure to the shell (back control and esc) and to the registry below
/// (the hamburger menu, a tab switch). `false` means "keep the panel and
/// its state", `true` means "the exit may proceed".
typedef ToolExitGuard = Future<bool> Function();

/// Route-level veto (`GoRoute.onExit`) for exits no panel control drives.
///
/// The browser Back button and the OS back gesture reach the router through
/// [RouterDelegate.popRoute], not through the shared back control, so the
/// shell's own guard never sees them. `GoRouterDelegate.popRoute` walks the
/// exiting route's `onExit` after `maybePop` declines to handle the pop (a
/// panel route is the only entry on the stack, so it always declines), which
/// makes this the one place a system pop can be vetoed.
///
/// Reads the registry through the [ProviderScope] above [context] and asks it
/// exactly like the shell does. Every panel route in
/// `ui/flutter_ui/lib/core/router.dart` points its `onExit` here.
Future<bool> guardRouteExit(BuildContext context, GoRouterState state) {
  final container = ProviderScope.containerOf(context, listen: false);
  return container.read(toolExitGuardProvider).requestExit();
}

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
