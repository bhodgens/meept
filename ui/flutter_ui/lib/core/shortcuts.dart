import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

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
  static bool get _isMacOS => !kIsWeb && Platform.isMacOS;

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
class AppShortcuts extends StatefulWidget {
  final Widget child;
  final LeaderKeyController controller;

  const AppShortcuts({
    super.key,
    required this.child,
    required this.controller,
  });

  @override
  State<AppShortcuts> createState() => _AppShortcutsState();
}

class _AppShortcutsState extends State<AppShortcuts> {
  @override
  Widget build(BuildContext context) {
    return Focus(
      autofocus: true,
      onKeyEvent: _handleKeyEvent,
      child: widget.child,
    );
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    return widget.controller.handleKeyEvent(event);
  }
}
