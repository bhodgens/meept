import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../providers/providers.dart';
import '../providers/tool_exit_guard.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// Leave the currently open tool panel and return to the chat view.
///
/// A tool panel opens through one of two paths:
///   1. a full-screen route (`/tools/<name>`), or
///   2. the embedded chat-tab tool slot (`activeToolProvider`).
///
/// Both paths are cleared here, so one helper is correct for either
/// path. A pushed detail page (for example a prompt detail) pops
/// first, because `context.canPop()` is true in that case.
void exitToolPanel(BuildContext context, WidgetRef ref) {
  ref.read(activeToolProvider.notifier).state = '';
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/');
  }
}

/// Shared chrome for every tool panel.
///
/// Gives each panel the same three things (AGENTS.md: TUI and GUI
/// features stay at parity, UI text stays lowercase):
///   - a back control that returns to the chat view,
///   - an ESC shortcut with the same effect,
///   - the panel body.
///
/// Wrap the whole panel body in this shell. Do not add a second,
/// panel-local back button: one shared control keeps the exit
/// behavior identical everywhere.
///
/// ESC is handled at this ancestor node, so an inner widget that
/// handles ESC itself (the find bar, the command palette) keeps
/// priority: key events reach the primary focus first and stop at
/// the first handler that returns `handled`.
///
/// A panel that holds unsaved work passes [exitGuard] so both exit
/// affordances ask before dropping it. A second press of the back control or
/// esc is ignored while the first request is decided, so this shell cannot
/// stack its own confirmation on itself (see the `_exitPending` scope note:
/// a menu pick or tab switch bypasses it, and only the dialog's modality
/// blocks a double ask there).
class ToolPanelShell extends ConsumerStatefulWidget {
  /// Panel name shown in the header, lowercase.
  final String title;

  /// Optional icon shown between the back control and the title.
  final IconData? icon;

  /// Optional widgets shown at the trailing edge of the header row.
  final List<Widget> actions;

  /// Optional extra controls rendered under the shared header row,
  /// inside the same bordered block (for example a panel search
  /// field, a tab bar, or a filter row).
  final Widget? header;

  /// Optional asynchronous veto, asked before the shell leaves the panel.
  ///
  /// Both the back control and the esc handler call this first and exit only
  /// when it resolves `true`; `false` leaves the panel mounted with all of
  /// its state intact. `null` (the default) exits immediately, exactly as it
  /// did before the guard existed, so panels with nothing to lose need no
  /// change.
  ///
  /// This veto carries every exit the shell drives, because it cannot rely on
  /// a route-level `PopScope`: the embedded path (a tool rendered inside the
  /// chat tab) lives on the same route, so no route veto can see it, and the
  /// menu and tab paths ask the registry instead of the route. The panel
  /// routes do carry `GoRoute.onExit` (see `guardRouteExit`) for the exits no
  /// control drives - the browser Back button and the OS back gesture - but
  /// that covers only the full-screen path.
  final Future<bool> Function()? exitGuard;

  /// Panel body.
  final Widget child;

  const ToolPanelShell({
    super.key,
    required this.title,
    required this.child,
    this.icon,
    this.actions = const <Widget>[],
    this.header,
    this.exitGuard,
  });

  @override
  ConsumerState<ToolPanelShell> createState() => _ToolPanelShellState();
}

class _ToolPanelShellState extends ConsumerState<ToolPanelShell> {
  /// Latch for a shell-driven exit request (back control, esc) in flight.
  ///
  /// The guard is asynchronous - it usually opens a confirmation dialog - so
  /// a second press of THIS shell's back control or esc before the first
  /// answer arrives would ask the guard again and stack a second dialog over
  /// the panel. While this is set, further presses of that control are
  /// ignored.
  ///
  /// SCOPE: this latch is per-shell and covers only the shell's own
  /// affordances; it is NOT the shared request latch. [_requestExit] calls
  /// the panel guard directly instead of going through
  /// [ToolExitGuardRegistry.requestExit], so a hamburger-menu pick, a tab
  /// switch or the registry's own request never sees this flag and can ask
  /// the same guard while this one is still deciding. That double ask is
  /// blocked in practice only because the discard dialog is modal - it
  /// swallows the pointer and key events that would reach another
  /// affordance - not because of anything this latch does.
  bool _exitPending = false;

  @override
  Widget build(BuildContext context) {
    return Focus(
      // Autofocus so ESC reaches this node without a click first.
      autofocus: true,
      onKeyEvent: (node, event) {
        if (event is KeyDownEvent &&
            event.logicalKey == LogicalKeyboardKey.escape) {
          _requestExit();
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _buildHeader(context),
          Expanded(child: widget.child),
        ],
      ),
    );
  }

  /// Ask the panel's guard, then leave the panel only when it allows the
  /// exit.
  ///
  /// The esc handler cannot wait for the answer, so the exit is driven from
  /// the guard's future instead of the key event; the key event is still
  /// reported as handled either way.
  Future<void> _requestExit() async {
    if (_exitPending) return;
    // The panel's own guard wins; a panel that only registered in the shared
    // provider (and passed no guard here) is still honoured, so neither
    // affordance can bypass unsaved state.
    final guard = widget.exitGuard ?? ref.read(toolExitGuardProvider).guard;
    if (guard == null) {
      exitToolPanel(context, ref);
      return;
    }
    _exitPending = true;
    try {
      final allowed = await guard();
      if (!allowed) return;
      // The panel can be closed from elsewhere (a menu pick, another route)
      // while the guard's confirmation is open.
      if (!mounted) return;
      // The exit was allowed, so drop the shared registration: the route
      // change below must not consult the same guard again and ask a second
      // time. A guard this shell did not take from the registry is not the
      // registered one, so [ToolExitGuardRegistry.release] leaves that
      // registration alone.
      ref.read(toolExitGuardProvider).release(guard);
      exitToolPanel(context, ref);
    } finally {
      // Released on every outcome (refused, allowed and exited, or disposed
      // mid-await), so the next press starts a fresh request.
      _exitPending = false;
    }
  }

  Widget _buildHeader(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Row(
              children: [
                IconButton(
                  icon: const Icon(Icons.arrow_back, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  tooltip: 'back (esc)',
                  onPressed: () => _requestExit(),
                ),
                if (widget.icon != null) ...[
                  const SizedBox(width: 4),
                  Icon(
                    widget.icon,
                    color: CyberpunkColors.orangePrimary,
                    size: 18,
                  ),
                ],
                const SizedBox(width: 8),
                Text(
                  widget.title.toLowerCase(),
                  style: CyberpunkTypography.label.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                ...widget.actions,
              ],
            ),
          ),
          if (widget.header != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
              child: widget.header!,
            ),
        ],
      ),
    );
  }
}
