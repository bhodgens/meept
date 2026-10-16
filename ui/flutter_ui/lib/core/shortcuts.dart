// App-wide keyboard shortcuts. All bindings use Ctrl on every platform
// (TUI parity — identical keys across surfaces per the repo UI conventions).

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// App-wide intent types for keyboard shortcuts.
abstract class AppIntent extends Intent {
  const AppIntent();
}

/// Leader key trigger — opens the command palette.
class LeaderIntent extends AppIntent {
  const LeaderIntent();
}

/// Switch to Sessions tab.
class SessionsTabIntent extends AppIntent {
  const SessionsTabIntent();
}

/// Switch to Chat tab.
class ChatTabIntent extends AppIntent {
  const ChatTabIntent();
}

/// Focus input with '/' prefix.
class FocusInputIntent extends AppIntent {
  const FocusInputIntent();
}

/// Show keyboard shortcut help.
class ShowHelpIntent extends AppIntent {
  const ShowHelpIntent();
}

/// Escape — close drawer / dismiss / blur.
class EscapeIntent extends AppIntent {
  const EscapeIntent();
}

/// Project / branches context.
class BranchesIntent extends AppIntent {
  const BranchesIntent();
}

/// Focus search / find.
class FindIntent extends AppIntent {
  const FindIntent();
}

/// Global semantic search (single `f` key from sessions tab).
class GlobalSearchIntent extends AppIntent {
  const GlobalSearchIntent();
}

/// Controller for app-wide keyboard shortcuts.
///
/// Previously this class hosted a two-stage "leader key" state machine
/// (leader + follow-up character). It has been replaced by a command
/// palette: pressing Ctrl+X (all platforms, TUI parity) now invokes
/// [onShowCommandPalette] which opens a modal palette dialog.
/// All former `onTabSelected` / `onNavigate` / etc. callbacks are
/// retained because they are still used by direct shortcuts and by
/// the palette's selection handler in `HomeScreen`.
class LeaderKeyController extends ChangeNotifier {
  /// Whether branches are enabled (from session.branches_enabled config).
  /// Defaults to false (branches disabled, use threads instead).
  bool branchesEnabled = false;

  /// Set this callback to route tab switches from the shortcut layer
  /// up to the containing widget. Index maps to [HomeTab.values].
  void Function(int index)? onTabSelected;

  /// Set this callback to focus the chat input, optionally with '/' prefix.
  VoidCallback? onFocusInput;

  /// Set this callback to show help.
  VoidCallback? onShowHelp;

  /// Set this callback to handle branches/projects.
  /// Only called if [branchesEnabled] is true.
  VoidCallback? onBranches;

  /// Set this callback to handle find/search.
  VoidCallback? onFind;

  /// Toggle steer mode (Ctrl+S, TUI parity). The chat layer decides:
  /// agent active → arm/disarm steer; idle → navigate to sessions.
  VoidCallback? onToggleSteer;

  /// Toggle TTS on/off (Ctrl+T, TUI parity).
  VoidCallback? onToggleTts;

  /// Open the fuzzy finder over sessions/tasks (Ctrl+P, TUI parity).
  /// The Flutter equivalent is the search panel route.
  VoidCallback? onFuzzyFinder;

  /// Set this callback to open the in-session find bar (Ctrl+F, TUI parity).
  VoidCallback? onInSessionFind;

  /// Set this callback to open global search (single `f` key from
  /// sessions tab). The callback should check the current tab/route
  /// before navigating — the controller fires on every unmodified `f`
  /// press, leaving route-gating to the widget layer.
  VoidCallback? onGlobalSearch;

  /// Stop the current turn (Ctrl+C, TUI parity — the TUI quits on
  /// double-press; the GUI has no quit semantics, so a single press stops
  /// the active turn instead). Optional: when null, Ctrl+C is ignored.
  VoidCallback? onStopCurrentTurn;

  /// Open the command palette dialog (replaces the former leader mode).
  VoidCallback? onShowCommandPalette;

  /// Cycle the verbosity level (Ctrl+V on all platforms, TUI parity).
  VoidCallback? onCycleVerbosity;

  /// Optional callback for go_router navigation.
  void Function(String path)? onNavigate;

  // Ctrl on ALL platforms (TUI parity — identical keys across surfaces per
  // the repo UI conventions; Ctrl+V precedent). No Cmd variants.
  static LogicalKeySet get leaderKeySet {
    return LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyX);
  }

  static LogicalKeySet get focusInputKeySet {
    return LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyK);
  }

  /// Ctrl+F — open in-session find bar (TUI parity, all platforms).
  static LogicalKeySet get findKeySet {
    return LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyF);
  }

  /// Handle a raw key event directly (Focus widget dispatch path).
  KeyEventResult handleKeyEvent(KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;

    // --- Leader trigger → open command palette ---
    if (_isLeaderTrigger(event)) {
      onShowCommandPalette?.call();
      return KeyEventResult.handled;
    }

    // --- Ctrl+V verbosity (all platforms, TUI parity) ---
    if (_isVerbosityTrigger(event)) {
      onCycleVerbosity?.call();
      return KeyEventResult.handled;
    }

    // --- Direct shortcuts ---
    if (_isFocusInputTrigger(event)) {
      onFocusInput?.call();
      return KeyEventResult.handled;
    }

    if (_isFindTrigger(event)) {
      onInSessionFind?.call();
      return KeyEventResult.handled;
    }

    // --- Ctrl+C stop current turn (TUI parity; no quit semantics in GUI) ---
    if (_isCtrlKeyTrigger(event, LogicalKeyboardKey.keyC)) {
      onStopCurrentTurn?.call();
      return KeyEventResult.handled;
    }

    // --- Ctrl+S steer toggle (all platforms, TUI parity) ---
    if (_isCtrlKeyTrigger(event, LogicalKeyboardKey.keyS)) {
      onToggleSteer?.call();
      return KeyEventResult.handled;
    }

    // --- Ctrl+T TTS toggle (all platforms, TUI parity) ---
    if (_isCtrlKeyTrigger(event, LogicalKeyboardKey.keyT)) {
      onToggleTts?.call();
      return KeyEventResult.handled;
    }

    // --- Ctrl+P fuzzy finder → search panel (TUI parity) ---
    if (_isCtrlKeyTrigger(event, LogicalKeyboardKey.keyP)) {
      onFuzzyFinder?.call();
      return KeyEventResult.handled;
    }

    // Escape is intentionally ignored here — the Focus system handles
    // dismissal of dialogs/popups via EscapeIntent.
    return KeyEventResult.ignored;
  }

  /// Plain-Ctrl (no meta/shift/alt) trigger check, used for the TUI-parity
  /// Ctrl+C / Ctrl+S / Ctrl+T / Ctrl+P bindings which are identical on all
  /// platforms.
  static bool _isCtrlKeyTrigger(KeyEvent event, LogicalKeyboardKey key) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != key) return false;
    final hw = HardwareKeyboard.instance;
    return hw.isControlPressed && !hw.isMetaPressed && !hw.isAltPressed;
  }

  static bool _isLeaderTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyX) return false;
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Detect Ctrl+V on ALL platforms (parity with TUI per CLAUDE.md UI
  /// conventions — Ctrl+V on macOS is intentionally the same as
  /// Linux/Windows; we do NOT use Cmd+V here).
  static bool _isVerbosityTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyV) return false;
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Detect Ctrl+K on ALL platforms (parity with TUI — same key everywhere).
  static bool _isFocusInputTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyK) return false;
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Detect Ctrl+F for in-session find (all platforms, TUI parity).
  static bool _isFindTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyF) return false;
    return HardwareKeyboard.instance.isControlPressed;
  }
}

/// Wraps a child with app-wide shortcuts via a Focus node.
///
/// Dispatch happens in [LeaderKeyController.handleKeyEvent] through a
/// raw-key-event Focus node. The legacy Shortcuts+Actions widgets were
/// removed when the leader-key state machine was replaced by the
/// command palette.
///
/// Uses CallbackShortcuts to intercept system-reserved key combinations
/// (Ctrl+X for command palette, Ctrl+K for focus input, etc.) before
/// Flutter's default text editing shortcuts consume them.
///
/// All bindings use Ctrl on every platform (TUI parity — identical keys
/// across surfaces per the repo UI conventions; the former per-platform
/// Cmd-vs-Ctrl preference was removed).
class AppShortcuts extends ConsumerStatefulWidget {
  final Widget child;
  final LeaderKeyController controller;

  const AppShortcuts({
    super.key,
    required this.child,
    required this.controller,
  });

  @override
  ConsumerState<AppShortcuts> createState() => _AppShortcutsState();
}

class _AppShortcutsState extends ConsumerState<AppShortcuts> {
  @override
  Widget build(BuildContext context) {
    // Use CallbackShortcuts to intercept system-reserved key combos
    // before Flutter's default text editing shortcuts consume them.
    // This is necessary because Ctrl+K, Ctrl+F, etc. would otherwise be
    // handled by Flutter's EditableText widgets by default.
    return CallbackShortcuts(
      bindings: {
        // Ctrl+X → command palette (TUI parity, all platforms)
        const SingleActivator(LogicalKeyboardKey.keyX, control: true): () =>
            widget.controller.onShowCommandPalette?.call(),
        // Ctrl+V → cycle verbosity (all platforms, TUI parity)
        const SingleActivator(LogicalKeyboardKey.keyV, control: true): () =>
            widget.controller.onCycleVerbosity?.call(),
        // Ctrl+K → focus input (TUI parity, all platforms)
        const SingleActivator(LogicalKeyboardKey.keyK, control: true): () =>
            widget.controller.onFocusInput?.call(),
        // Ctrl+F → in-session find (TUI parity, all platforms)
        const SingleActivator(LogicalKeyboardKey.keyF, control: true): () =>
            widget.controller.onInSessionFind?.call(),
        // Ctrl+C → stop current turn (TUI parity; no quit semantics in GUI)
        const SingleActivator(LogicalKeyboardKey.keyC, control: true): () =>
            widget.controller.onStopCurrentTurn?.call(),
      },
      child: Focus(
        autofocus: true,
        onKeyEvent: _handleKeyEvent,
        child: widget.child,
      ),
    );
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    return widget.controller.handleKeyEvent(event);
  }
}
