import 'dart:async';

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

/// Where a panel route lands the user when an allowed system pop leaves it.
///
/// Every tool panel is a top-level route with the chat home below it, so an
/// allowed pop returns there - the same destination the shared back control
/// uses (`exitToolPanel`).
const String _chatHomeLocation = '/';

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
Future<bool> guardRouteExit(BuildContext context, GoRouterState state) async {
  final container = ProviderScope.containerOf(context, listen: false);
  final allowed = await container.read(toolExitGuardProvider).requestExit();
  if (!allowed) return false;
  // An ALLOWED onExit is not a navigation in go_router 14.x, so leaving the
  // route is this callback's job; see [_leaveRouteAfterAllowedExit].
  if (context.mounted) _leaveRouteAfterAllowedExit(context);
  return true;
}

/// Leave the current panel route after its veto allowed the exit.
///
/// `GoRouterDelegate.popRoute` answers the platform with "not handled" when
/// `onExit` allows the pop (`return !(await lastRoute.onExit(...))`), and the
/// platform's own back handling does not move a single-entry go_router stack:
/// nothing navigates. Without this the panel would stay on screen while the
/// registry had already released its guard, so the next tool switch or window
/// close would drop the panel's edits with no prompt at all.
///
/// Only when the router has no other destination to apply. While an
/// app-driven navigation is being applied - a menu pick, a tab switch, a home
/// layout's guarded navigation, or the shared back control - this same
/// `onExit` runs inside that navigation, and a second `go` here would replace
/// the destination the user actually chose with the chat home.
void _leaveRouteAfterAllowedExit(BuildContext context) {
  final router = GoRouter.maybeOf(context);
  if (router == null) return;
  final applied = router.state.uri.path;
  final requested = router.routeInformationProvider.value.uri.path;
  if (requested != applied) return;
  router.go(_chatHomeLocation);
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

  /// The answer to the exit request currently being decided, if any.
  ///
  /// The guard is asynchronous and usually opens a confirmation dialog, so a
  /// second request arriving before the first answer (a second browser pop, a
  /// menu pick during the shared back control's dialog) would ask the guard
  /// again and stack a second dialog over the first. While one request is in
  /// flight, further requests share its answer instead of asking again.
  /// `ToolPanelShell._exitPending` prevents the same double dialog from a
  /// second press of its back control or esc by ignoring it.
  ///
  /// Sharing rather than refusing is what keeps this correct on the exit
  /// path: leaving the panel after an allowed pop runs the route's `onExit`
  /// again (see [_leaveRouteAfterAllowedExit]), and a refusal there would
  /// cancel the very navigation the user just approved.
  ///
  /// There is deliberately no time bound on the latch. A bound would have to
  /// invent an answer for a request that is still being asked - usually a
  /// dialog the user has open in front of them - and answering "allowed"
  /// there would dismiss unsaved edits on a timer. The one moment a stuck
  /// request can be answered honestly is when its guard goes away, which
  /// [release] does.
  Future<bool>? _inFlight;

  /// The latch behind [_inFlight], so [release] can settle a request whose
  /// guard is going away instead of leaving it and the latch pending forever.
  Completer<bool>? _inFlightCompleter;

  /// The guard [_inFlight] was handed to, so [release] can tell whether the
  /// panel being dropped is the one whose answer is still owed.
  ToolExitGuard? _inFlightGuard;

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
  ///
  /// A panel that goes away (a route change, a window close, a tab switch)
  /// while its own exit request is undecided takes the answer with it: no
  /// other path ever completes that guard's future, so the latch is settled
  /// here. Which answer it settles with is decided by who owns the screen
  /// now:
  ///
  ///   - no guard is registered (the releasing guard was the only one -
  ///     `_guard` cleared above), so nothing on screen can refuse and the
  ///     request is settled ALLOWED. The guard that would have refused is no
  ///     longer the panel on screen.
  ///   - a superseding guard registered on top while this request was owed.
  ///     That guard owns the screen now and was never asked, so the request
  ///     is settled REFUSED. Allowing here released the exit past a panel
  ///     that was never consulted with its edits still on screen.
  ///
  /// CONTRACT (pinned by test/features/home/tool_exit_guard_test.dart): a
  /// request is answered exactly once, by whichever settles it first. A guard
  /// that is released while its own request is undecided therefore loses a
  /// later answer it gives - the released guard's dialog is answered after the
  /// latch already settled, and that late answer (a cancel included) is
  /// ignored because the request is closed. This is deliberate: the settling
  /// answer is final, and [_settleInFlight] is the only place a request ends,
  /// so the two endings (guard answering, guard going away) cannot both
  /// complete the same completer.
  void release(ToolExitGuard guard) {
    if (identical(_guard, guard)) _guard = null;
    if (identical(_inFlightGuard, guard)) {
      // `_guard` is null exactly when the releasing guard still owned the
      // screen; a superseding registration leaves it non-null and refuses.
      final stillMine = identical(_guard, guard);
      _settleInFlight(stillMine || _guard == null);
    }
  }

  /// Ask the registered guard whether the open panel may be left, and
  /// release the registration when it allows the exit.
  ///
  /// Returns true when nothing is registered (nothing to lose: no dialog,
  /// no state change). Releasing on an allowed exit is what keeps one
  /// switch from asking twice: the route change that follows the answer
  /// must not consult a guard for edits the user just agreed to discard.
  Future<bool> requestExit() {
    final inFlight = _inFlight;
    if (inFlight != null) return inFlight;

    // The completer is installed before the guard is asked: a guard that
    // answers without awaiting anything completes synchronously, and a
    // request that latched itself afterwards would leave every later request
    // sharing a stale answer - the panel would never be asked again.
    final completer = Completer<bool>();
    final pending = completer.future;
    final asked = _guard;
    _inFlight = pending;
    _inFlightCompleter = completer;
    _inFlightGuard = asked;
    unawaited(
      _ask().then(
        (allowed) {
          // A settled latch (see [release]) has already answered this request
          // and cleared the fields, so only the still-current ask completes.
          if (identical(_inFlightCompleter, completer)) {
            _settleInFlight(allowed);
          }
        },
        onError: (Object error, StackTrace stack) {
          if (!identical(_inFlightCompleter, completer)) return;
          _inFlight = null;
          _inFlightCompleter = null;
          _inFlightGuard = null;
          completer.completeError(error, stack);
        },
      ),
    );
    return pending;
  }

  /// Answer the in-flight request with [allowed] and drop the latch.
  ///
  /// The one place the latch is cleared, so the two ways a request ends - the
  /// guard answering, and the guard going away (see [release]) - cannot drift
  /// apart and leave a request pending that nothing will ever complete.
  void _settleInFlight(bool allowed) {
    final completer = _inFlightCompleter;
    _inFlight = null;
    _inFlightCompleter = null;
    _inFlightGuard = null;
    if (completer != null && !completer.isCompleted) {
      completer.complete(allowed);
    }
  }

  Future<bool> _ask() async {
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
