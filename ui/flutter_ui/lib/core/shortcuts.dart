import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'router.dart';

/// App-wide intent types for keyboard shortcuts.
abstract class AppIntent extends Intent {
  const AppIntent();
}

/// Leader key trigger — waits for a follow-up character.
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

/// Leader key state machine.
///
/// Two-stage input: on leader key, enter "waiting" state. Next
/// character dispatches the corresponding action. Times out after
/// 0.5s if no follow-up key is pressed.
///
/// Navigation can be handled in two ways:
/// 1. Wire [onNavigate] to call `context.go(path)` — preferred for
///    widgets that have a BuildContext.
/// 2. Leave [onNavigate] unset and the controller will use the global
///    [router] directly via `router.go(path)`.
class LeaderKeyController extends ChangeNotifier {
  static bool get _isMacOS => !kIsWeb && Platform.isMacOS;

  /// Whether the leader key is currently in "waiting for sequence" mode.
  bool get isWaiting => _waiting;
  bool _waiting = false;

  Timer? _timeout;

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
  ///
  /// For global semantic search (leader+p). In-session find (cmd+f/ctrl+f)
  /// uses [onInSessionFind].
  VoidCallback? onFind;

  /// Set this callback to open the in-session find bar (Cmd+F / Ctrl+F).
  VoidCallback? onInSessionFind;

  /// Optional callback for go_router navigation.
  ///
  /// When set, leader sequences that trigger navigation will call
  /// this with the target route path (e.g. `/sessions`, `/tools/search`).
  /// When unset, the controller falls back to the global [router].
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

  /// Handle a raw key event directly (used when not using Flutter's
  /// Actions system, e.g. for sequential leader keys).
  KeyEventResult handleKeyEvent(KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;

    final key = event.logicalKey;

    // --- Leader trigger ---
    if (_isLeaderTrigger(event)) {
      _enterLeaderMode();
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

    if (key == LogicalKeyboardKey.escape) {
      if (_waiting) {
        _exitLeaderMode();
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }

    // Nothing recognized
    return KeyEventResult.ignored;
  }

  /// Handle the second keystroke while in leader mode.
  KeyEventResult handleLeaderSequence(KeyEvent event) {
    if (event is! KeyDownEvent) return KeyEventResult.ignored;

    final ch = _logicalKeyToChar(event.logicalKey);
    if (ch == null) {
      _exitLeaderMode();
      return KeyEventResult.ignored;
    }

    switch (ch) {
      case 's':
        _navigate('/sessions');
        onTabSelected?.call(1); // sessions
        break;
      case 'p':
        _navigate('/tools/search');
        onFind?.call();
        break;
      case 'b':
        _navigate('/tools/branches');
        onBranches?.call();
        break;
      case 'c':
        _navigate('/');
        onTabSelected?.call(0); // chat
        break;
      case '?':
        onShowHelp?.call();
        break;
      default:
        break;
    }
    _exitLeaderMode();
    return KeyEventResult.handled;
  }

  /// Navigate using go_router.
  ///
  /// Prefers [onNavigate] callback (for BuildContext-based navigation);
  /// falls back to the global [router] instance.
  void _navigate(String path) {
    if (onNavigate != null) {
      onNavigate!(path);
    } else {
      router.go(path);
    }
  }

  void _enterLeaderMode() {
    _timeout?.cancel();
    _waiting = true;
    notifyListeners();
    _timeout = Timer(const Duration(milliseconds: 500), _exitLeaderMode);
  }

  void _exitLeaderMode() {
    _timeout?.cancel();
    if (_waiting) {
      _waiting = false;
      notifyListeners();
    }
  }

  @override
  void dispose() {
    _timeout?.cancel();
    super.dispose();
  }

  static bool _isLeaderTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyX) return false;
    if (_isMacOS) {
      return HardwareKeyboard.instance.isMetaPressed;
    }
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

  /// Convert common logical keys to their character representation.
  static String? _logicalKeyToChar(LogicalKeyboardKey key) {
    if (key == LogicalKeyboardKey.keyS) return 's';
    if (key == LogicalKeyboardKey.keyP) return 'p';
    if (key == LogicalKeyboardKey.keyB) return 'b';
    if (key == LogicalKeyboardKey.keyC) return 'c';
    if (key == LogicalKeyboardKey.slash) return '?';
    if (key == LogicalKeyboardKey.digit1) return '1';
    return null;
  }
}

/// Wraps a child with app-wide shortcuts using Flutter's Shortcuts + Actions.
///
/// The leader key is handled natively through a Focus node with raw
/// key events (required because leader sequences are multi-keystroke).
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
    return Shortcuts(
      shortcuts: <LogicalKeySet, Intent>{
        // These shortcuts are registered but the actual dispatch is handled
        // by the Focus widget below for leader sequences.
        LeaderKeyController.leaderKeySet: const LeaderIntent(),
        LeaderKeyController.focusInputKeySet: const FocusInputIntent(),
        LeaderKeyController.findKeySet: const FindIntent(),
      },
      child: Actions(
        actions: <Type, Action<Intent>>{
          LeaderIntent: CallbackAction<LeaderIntent>(
            onInvoke: (_) {
              widget.controller._enterLeaderMode();
              return null;
            },
          ),
          FocusInputIntent: CallbackAction<FocusInputIntent>(
            onInvoke: (_) {
              widget.controller.onFocusInput?.call();
              return null;
            },
          ),
          FindIntent: CallbackAction<FindIntent>(
            onInvoke: (_) {
              widget.controller.onFind?.call();
              return null;
            },
          ),
          EscapeIntent: CallbackAction<EscapeIntent>(
            onInvoke: (_) {
              if (widget.controller.isWaiting) {
                widget.controller._exitLeaderMode();
                return null;
              }
              return null;
            },
          ),
        },
        child: Focus(
          autofocus: true,
          onKeyEvent: _handleKeyEvent,
          child: widget.child,
        ),
      ),
    );
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    final ctrl = widget.controller;

    // If in leader mode, try to consume the next keystroke as a sequence.
    if (ctrl.isWaiting) {
      return ctrl.handleLeaderSequence(event);
    }

    return ctrl.handleKeyEvent(event);
  }
}
