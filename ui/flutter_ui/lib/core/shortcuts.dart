import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Leader key state machine.
///
/// Two-stage input: on leader key, enter "waiting" state. Next
/// character dispatches the corresponding action. Times out after
/// 500ms if no follow-up key is pressed.
class LeaderKeyController extends ChangeNotifier {
  static bool get _isMacOS => Platform.isMacOS;

  /// Whether the leader key is currently in "waiting for sequence" mode.
  bool get isWaiting => _waiting;
  bool _waiting = false;

  Timer? _timeout;

  /// Set this callback to route tab switches from the shortcut layer
  /// up to the containing widget.
  void Function(int index)? onTabSelected;

  /// Set this callback to toggle the drawer.
  VoidCallback? onToggleDrawer;

  /// Set this callback to focus the chat input.
  VoidCallback? onFocusInput;

  /// Whether the next focus should include '/' prefix.
  bool _slashPrefix = false;

  /// Whether to use slash prefix on next focus.
  bool get slashPrefix => _slashPrefix;

  /// Set this callback to show help.
  VoidCallback? onShowHelp;

  /// Set this callback to handle branches/projects.
  VoidCallback? onBranches;

  /// Set this callback to handle find/search.
  VoidCallback? onFind;

  /// Handle a raw key event for leader triggers and direct shortcuts.
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
      _slashPrefix = true;
      onFocusInput?.call();
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
        onTabSelected?.call(1); // sessions
        break;
      case 'p':
        onFind?.call();
        break;
      case 'b':
        onBranches?.call();
        break;
      case 'd':
        onToggleDrawer?.call();
        break;
      case 'c':
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
      return HardwareKeyboard.instance.isControlPressed ||
          HardwareKeyboard.instance.isMetaPressed;
    }
    return HardwareKeyboard.instance.isControlPressed;
  }

  static bool _isFocusInputTrigger(KeyEvent event) {
    if (event is! KeyDownEvent) return false;
    if (event.logicalKey != LogicalKeyboardKey.keyK) return false;
    if (_isMacOS) {
      return HardwareKeyboard.instance.isControlPressed ||
          HardwareKeyboard.instance.isMetaPressed;
    }
    return HardwareKeyboard.instance.isControlPressed;
  }

  /// Convert common logical keys to their character representation.
  static String? _logicalKeyToChar(LogicalKeyboardKey key) {
    if (key == LogicalKeyboardKey.keyS) return 's';
    if (key == LogicalKeyboardKey.keyP) return 'p';
    if (key == LogicalKeyboardKey.keyB) return 'b';
    if (key == LogicalKeyboardKey.keyD) return 'd';
    if (key == LogicalKeyboardKey.keyC) return 'c';
    if (key == LogicalKeyboardKey.slash) return '?';
    if (key == LogicalKeyboardKey.digit1) return '1';
    return null;
  }
}

/// Wraps a child with app-wide shortcuts using raw Focus key events.
///
/// All shortcuts are handled natively through a Focus node with raw
/// key events. No Flutter Shortcuts/Actions system is used, which
/// avoids system beeps on macOS when intercepting cmd+x / ctrl+x.
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
    final ctrl = widget.controller;

    // If in leader mode, try to consume the next keystroke as a sequence.
    if (ctrl.isWaiting) {
      return ctrl.handleLeaderSequence(event);
    }

    final result = ctrl.handleKeyEvent(event);
    if (result == KeyEventResult.handled) return result;

    // Auto-focus chat input on typing when no text field has focus
    if (event is KeyDownEvent &&
        event.character != null &&
        event.character!.isNotEmpty &&
        event.character!.trim().isNotEmpty &&
        FocusManager.instance.primaryFocus == node) {
      ctrl._slashPrefix = false;
      ctrl.onFocusInput?.call();
      return KeyEventResult.ignored;
    }

    return result;
  }
}
