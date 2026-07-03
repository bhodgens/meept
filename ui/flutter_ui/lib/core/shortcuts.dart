// Platform detection via compile-time constants for web compatibility
// Runtime checks use platformService where needed (see LeaderKeyController._isMacOS)


import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/preferences_provider.dart';

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
/// palette: pressing the leader combo (Cmd+X mac / Ctrl+X other) now
/// invokes [onShowCommandPalette] which opens a modal palette dialog.
/// All former `onTabSelected` / `onNavigate` / etc. callbacks are
/// retained because they are still used by direct shortcuts and by
/// the palette's selection handler in `HomeScreen`.
class LeaderKeyController extends ChangeNotifier {
  static bool get _isMacOS {
    // Use compile-time constant: true only on macOS native builds
    // On web and non-macOS native, returns false (defaults to ctrl+x)
    return const bool.fromEnvironment('os.macos');
  }

  /// Set this callback to route tab switches from the shortcut layer
  /// up to the containing widget. Index maps to [HomeTab.values].
  void Function(int index)? onTabSelected;

  /// Set this callback to focus the chat input, optionally with '/' prefix.
  VoidCallback? onFocusInput;

  /// Set this callback to show help.
  VoidCallback? onShowHelp;

  /// Set this callback to handle branches/projects.
  VoidCallback? onBranches;

  /// Set this callback to handle find/search.
  VoidCallback? onFind;

  /// Set this callback to open the in-session find bar (Cmd+F / Ctrl+F).
  VoidCallback? onInSessionFind;

  /// Set this callback to open global search (single `f` key from
  /// sessions tab). The callback should check the current tab/route
  /// before navigating — the controller fires on every unmodified `f`
  /// press, leaving route-gating to the widget layer.
  VoidCallback? onGlobalSearch;

  /// Open the command palette dialog (replaces the former leader mode).
  VoidCallback? onShowCommandPalette;

  /// Cycle the verbosity level (Ctrl+V on all platforms, TUI parity).
  VoidCallback? onCycleVerbosity;

  /// Optional callback for go_router navigation.
  void Function(String path)? onNavigate;

  static LogicalKeySet get leaderKeySet {
    return _isMacOS
        ? LogicalKeySet(LogicalKeyboardKey.meta, LogicalKeyboardKey.keyX)
        : LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyX);
  }

  static LogicalKeySet get focusInputKeySet {
    return _isMacOS
        ? LogicalKeySet(LogicalKeyboardKey.meta, LogicalKeyboardKey.keyK)
        : LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyK);
  }

  /// Cmd+F (macOS) / Ctrl+F (other) — open in-session find bar.
  static LogicalKeySet get findKeySet {
    return _isMacOS
        ? LogicalKeySet(LogicalKeyboardKey.meta, LogicalKeyboardKey.keyF)
        : LogicalKeySet(LogicalKeyboardKey.control, LogicalKeyboardKey.keyF);
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

    if (_isGlobalSearchTrigger(event)) {
      onGlobalSearch?.call();
      return KeyEventResult.handled;
    }

    // Escape is intentionally ignored here — the Focus system handles
    // dismissal of dialogs/popups via EscapeIntent.
    return KeyEventResult.ignored;
  }

  static bool _isLeaderTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyX) return false;
    if (_isMacOS) {
      return HardwareKeyboard.instance.isMetaPressed;
    }
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

  static bool _isFocusInputTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyK) return false;
    if (_isMacOS) {
      return HardwareKeyboard.instance.isMetaPressed;
    }
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Detect Cmd+F / Ctrl+F for in-session find.
  static bool _isFindTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyF) return false;
    if (_isMacOS) {
      return HardwareKeyboard.instance.isMetaPressed;
    }
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Detect a single `f` key press with no modifiers for global search.
  static bool _isGlobalSearchTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyF) return false;
    if (HardwareKeyboard.instance.isMetaPressed) return false;
    if (HardwareKeyboard.instance.isControlPressed) return false;
    if (HardwareKeyboard.instance.isAltPressed) return false;
    if (HardwareKeyboard.instance.isShiftPressed) return false;
    return true;
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
/// (Cmd+X/Ctrl+X for command palette, Cmd+K/Ctrl+K for focus input, etc.)
/// before Flutter's default text editing shortcuts consume them.
///
/// The modifier key (ctrl vs cmd) for leader, focus input, and find
/// shortcuts is configurable via [modifierKeyProvider] ("ctrl" or "cmd").
/// Defaults to "ctrl" on all platforms.
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
  /// Get the current modifier preference ("ctrl" or "cmd").
  String get _modifier => ref.read(modifierKeyProvider);

  /// Check if the user prefers cmd as the modifier.
  bool get _useCmd => _modifier == 'cmd';

  @override
  Widget build(BuildContext context) {
    // Use CallbackShortcuts to intercept system-reserved key combos
    // before Flutter's default text editing shortcuts consume them.
    // This is necessary because Cmd+X (Cut), Cmd+K, Cmd+F, etc. are
    // handled by Flutter's EditableText widgets by default.
    //
    // We register BOTH ctrl and cmd variants, but only fire the callback
    // if the pressed modifier matches the user's preference. This ensures
    // the shortcut works regardless of which modifier is actually pressed,
    // while respecting the user's configured preference.
    return CallbackShortcuts(
      bindings: {
        // Leader key → command palette
        // Register both variants, but check preference in callback
        const SingleActivator(LogicalKeyboardKey.keyX, meta: true, control: false):
            () {
              if (_useCmd) widget.controller.onShowCommandPalette?.call();
            },
        const SingleActivator(LogicalKeyboardKey.keyX, meta: false, control: true):
            () {
              if (!_useCmd) widget.controller.onShowCommandPalette?.call();
            },
        // Ctrl+V → cycle verbosity (all platforms, TUI parity)
        // Always uses Ctrl, never Cmd
        const SingleActivator(LogicalKeyboardKey.keyV, control: true):
            () => widget.controller.onCycleVerbosity?.call(),
        // Cmd+K / Ctrl+K → focus input (follows modifier preference)
        const SingleActivator(LogicalKeyboardKey.keyK, meta: true, control: false):
            () {
              if (_useCmd) widget.controller.onFocusInput?.call();
            },
        const SingleActivator(LogicalKeyboardKey.keyK, meta: false, control: true):
            () {
              if (!_useCmd) widget.controller.onFocusInput?.call();
            },
        // Cmd+F / Ctrl+F → in-session find (follows modifier preference)
        const SingleActivator(LogicalKeyboardKey.keyF, meta: true, control: false):
            () {
              if (_useCmd) widget.controller.onInSessionFind?.call();
            },
        const SingleActivator(LogicalKeyboardKey.keyF, meta: false, control: true):
            () {
              if (!_useCmd) widget.controller.onInSessionFind?.call();
            },
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
