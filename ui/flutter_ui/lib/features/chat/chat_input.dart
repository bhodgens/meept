import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/slash_commands.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'slash_autocomplete.dart';

/// Chat input widget - terminal-style with blinking cursor, 3 lines, black bg.
class ChatInput extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatInput({super.key, required this.sessionId});

  @override
  ConsumerState<ChatInput> createState() => _ChatInputState();
}

/// TextEditingController that paints a terminal-style underscore cursor
/// by overriding [buildTextSpan] — the Flutter API for custom text rendering
/// inside an EditableText.
///
/// Unlike overriding the `value` getter (which corrupts the text model),
/// `buildTextSpan` only changes what pixels the user sees. The actual
/// text, selection, and composing region in `value` stay untouched.
class _TerminalCursorController extends TextEditingController {
  bool showCursor = false;

  _TerminalCursorController();

  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    final value = this.value;

    if (!showCursor ||
        !value.selection.isValid ||
        !value.selection.isCollapsed) {
      return TextSpan(text: value.text, style: style);
    }

    final offset = value.selection.baseOffset.clamp(0, value.text.length);
    final before = value.text.substring(0, offset);
    final after = value.text.substring(offset);

    return TextSpan(
      style: style,
      children: [
        TextSpan(text: before),
        TextSpan(
          text: '\u2582', // lower quarter block — thick underscore
          style: style?.copyWith(color: CyberpunkColors.greenSuccess),
        ),
        TextSpan(text: after),
      ],
    );
  }

  /// Trigger a repaint (called by the blink timer).
  void refresh() => notifyListeners();
}

class _ChatInputState extends ConsumerState<ChatInput>
    with SingleTickerProviderStateMixin {
  late final _TerminalCursorController _controller;
  final _focusNode = FocusNode();
  late final AnimationController _blinkController;
  bool _cursorVisible = false;

  // Paste detection state
  String _previousText = '';
  final Map<int, String> _pasteStore = {};
  int _pasteCounter = 0;

  // Double-enter detection
  DateTime? _lastEnterTime;
  Timer? _enterDebounceTimer;
  static const _doubleEnterThresholdMs = 300;

  // Auto-expand height tracking - terminal style: 3 lines min
  static const int _minLines = 3;
  static const int _maxLines = 8;

  // Slash autocomplete state
  bool _showSlashAutocomplete = false;
  String _slashQuery = '';
  int _slashSelectedIndex = 0;

  // Ghost text for single slash match
  String? _ghostText;

  // Slash command registry
  static final _slashRegistry = SlashCommandRegistry();

  // File path attachments
  final List<String> _attachments = [];
  bool _hasFocused = false;

  @override
  void initState() {
    super.initState();
    _blinkController = AnimationController(
      duration: const Duration(milliseconds: 530),
      vsync: this,
    )..addStatusListener((status) {
        final visible = status == AnimationStatus.forward;
        if (visible != _cursorVisible) {
          _cursorVisible = visible;
          _controller.showCursor = visible && _focusNode.hasFocus;
          _controller.refresh();
        }
      });
    _controller = _TerminalCursorController();
    _controller.addListener(_onTextChanged);
    _focusNode.addListener(_onFocusChange);
    // Auto-focus the input field when the chat tab is first shown
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _focusNode.requestFocus();
      _hasFocused = true;
    });
  }

  void _onFocusChange() {
    if (_focusNode.hasFocus) {
      _blinkController.repeat(reverse: true);
    } else {
      _blinkController.stop();
      _cursorVisible = false;
      _controller.showCursor = false;
    }
    setState(() {});
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Refocus when the widget is rebuilt and becomes visible
    // This handles the case where user switches back to chat tab
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_hasFocused && !_focusNode.hasFocus) {
        _focusNode.requestFocus();
      }
    });
  }

  @override
  void dispose() {
    _enterDebounceTimer?.cancel();
    _blinkController.dispose();
    _controller.removeListener(_onTextChanged);
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _onTextChanged() {
    final currentText = _controller.text;
    _detectPaste(currentText);
    _detectSlashCommand(currentText);
    _detectFilePaths(currentText);
    _updateGhostText(currentText);
    _previousText = currentText;
  }

  /// Update ghost text for single slash command match.
  void _updateGhostText(String text) {
    if (!text.startsWith('/') || text.contains(' ')) {
      if (_ghostText != null) {
        setState(() => _ghostText = null);
      }
      return;
    }
    final matches = _slashRegistry.match(text);
    if (matches.length == 1) {
      // Ghost text is the full command + space, shown as suffix after cursor
      final ghost = '${matches.first.name} ';
      if (ghost != _ghostText) {
        setState(() => _ghostText = ghost);
      }
    } else {
      if (_ghostText != null) {
        setState(() => _ghostText = null);
      }
    }
  }

  /// Detect file path pastes: absolute paths that look like files.
  void _detectFilePaths(String currentText) {
    if (currentText.length < _previousText.length) return;
    final added = currentText.substring(_previousText.length);
    if (added.isEmpty) return;

    // Only trigger on multi-line paste (single keystroke shouldn't count)
    final addedLines = '\n'.allMatches(added).length + 1;
    if (addedLines < 3) return;

    // Scan for lines that look like file paths
    for (final line in added.split('\n')) {
      final candidate = line.trim();
      if (candidate.isEmpty) continue;
      if (_looksLikeFilePath(candidate) && !_attachments.contains(candidate)) {
        _attachments.add(candidate);
      }
    }
  }

  bool _looksLikeFilePath(String s) {
    // Absolute path heuristic: starts with / or ~/ or contains / after a word char
    if (s.startsWith('/') || s.startsWith('~/')) return true;
    if (RegExp(r'^[a-zA-Z]:[/\\]').hasMatch(s)) return true; // Windows
    return false;
  }

  void _detectSlashCommand(String text) {
    if (text.startsWith('/')) {
      final spaceIdx = text.indexOf(' ');
      final query = spaceIdx == -1 ? text : text.substring(0, spaceIdx);
      setState(() {
        _showSlashAutocomplete = true;
        _slashQuery = query;
        _slashSelectedIndex = 0;
      });
    } else {
      if (_showSlashAutocomplete) {
        setState(() {
          _showSlashAutocomplete = false;
          _slashQuery = '';
          _slashSelectedIndex = 0;
        });
      }
    }
  }

  void _onSlashSelected(SlashCommand command) {
    final cmdText = '${command.name} ';
    setState(() {
      _controller.text = cmdText;
      _controller.selection = TextSelection.collapsed(
        offset: cmdText.length,
      );
      _showSlashAutocomplete = false;
      _slashQuery = '';
    });
    _focusNode.requestFocus();
  }

  /// Detect paste operations by checking for large line count jumps
  void _detectPaste(String currentText) {
    if (currentText.length < _previousText.length) return;
    // Only detect pure appends — skip if previous text is not a prefix
    if (!currentText.startsWith(_previousText)) return;

    final added = currentText.substring(_previousText.length);
    final addedLines = '\n'.allMatches(added).length + 1;

    if (addedLines >= 3) {
      _pasteCounter++;
      final token = '{paste:$_pasteCounter}';
      _pasteStore[_pasteCounter] = added;

      final newText = _previousText + token;
      // Remove listener before mutating controller to prevent re-entrant
      // notification (bug F6: _onTextChanged called from listener + mutation).
      _controller.removeListener(_onTextChanged);
      _controller.text = newText;
      final finalLen = newText.length;
      _controller.selection = TextSelection.collapsed(offset: finalLen);
      _controller.addListener(_onTextChanged);
    }
  }

  /// Expand all paste tokens back to original content
  String _expandPastes(String text) {
    var result = text;
    for (final entry in _pasteStore.entries.toList().reversed) {
      result = result.replaceAll('{paste:${entry.key}}', entry.value);
    }
    return result;
  }

  String _preparePayload(String text) {
    var expanded = _expandPastes(text.trim());
    // Append file attachments if any
    if (_attachments.isNotEmpty) {
      expanded += '\n\n[attachments: ${_attachments.join(', ')}]';
    }
    return expanded;
  }

  /// Reset all input state after sending or handling a command.
  void _resetInputState() {
    _controller.text = '';
    _previousText = '';
    _pasteStore.clear();
    _pasteCounter = 0;
    _attachments.clear();
    _ghostText = null;
    _showSlashAutocomplete = false;
    _slashQuery = '';
  }

  /// Create a new chat session and switch to it.
  Future<void> _createNewSession() async {
    final notifier = ref.read(sessionProvider.notifier);
    final session = await notifier.createSession(
      'session ${DateTime.now().toIso8601String().substring(0, 16)}',
    );
    if (session != null) {
      ref.read(activeSessionProvider.notifier).state = session;
      ref.read(chatProvider.notifier).clearMessages();
    }
  }

  /// Try to handle a slash command locally.
  ///
  /// Returns true if the command was consumed and should NOT be sent
  /// to the backend as a chat message. Commands that are not recognized
  /// or need backend processing return false.
  bool _tryHandleSlashCommand(String text) {
    final spaceIdx = text.indexOf(' ');
    final command = spaceIdx == -1 ? text : text.substring(0, spaceIdx);

    switch (command) {
      case '/new':
        _createNewSession();
        return true;
      case '/clear':
        ref.read(chatProvider.notifier).clearMessages();
        return true;
      case '/stop':
        ref.read(chatProvider.notifier).sendSteer(
              sessionId: widget.sessionId,
              text: '/stop',
            );
        return true;
      default:
        return false;
    }
  }

  void _sendNormal(String text) {
    final payload = _preparePayload(text);
    if (payload.isEmpty) return;

    if (_tryHandleSlashCommand(payload)) {
      _resetInputState();
      return;
    }

    final chatNotifier = ref.read(chatProvider.notifier);
    final activeAgent = ref.read(activeAgentProvider);

    chatNotifier.sendMessage(
      sessionId: widget.sessionId,
      text: payload,
      agentId: activeAgent?.id ?? 'coder',
    );

    _resetInputState();
  }

  void _sendSteer(String text) {
    final payload = _preparePayload(text);
    if (payload.isEmpty) return;

    ref.read(chatProvider.notifier).sendSteer(
      sessionId: widget.sessionId,
      text: payload,
    );

    _resetInputState();
  }

  /// Handle raw key events for Enter, Shift+Enter, Escape
  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent) {
      if (event.logicalKey == LogicalKeyboardKey.enter) {
        // Guard: ignore Enter while LLM is responding (bug F6).
        if (ref.read(chatProvider).isLoading) return KeyEventResult.ignored;

        final isShiftPressed = HardwareKeyboard.instance.isShiftPressed;

        if (isShiftPressed) {
          final cleanText = _controller.text;
          final baseValue = _controller.value;
          final offset = baseValue.selection.end.clamp(0, cleanText.length);
          final before = cleanText.substring(0, offset);
          final after = cleanText.substring(offset);
          final fullNewText = '$before\n$after';
          final cursorNewPos = offset + 1;

          _controller.removeListener(_onTextChanged);
          _controller.text = fullNewText;
          _controller.selection = TextSelection.collapsed(offset: cursorNewPos);
          _controller.addListener(_onTextChanged);
          return KeyEventResult.handled;
        }

        // If slash autocomplete is showing, accept the selected match
        if (_showSlashAutocomplete) {
          final matches = _slashRegistry.match(_slashQuery);
          if (matches.isNotEmpty) {
            final idx = _slashSelectedIndex.clamp(0, matches.length - 1);
            _onSlashSelected(matches[idx]);
          }
          return KeyEventResult.handled;
        }

        final now = DateTime.now();
        if (_lastEnterTime != null) {
          final delta = now.difference(_lastEnterTime!).inMilliseconds;
          if (delta <= _doubleEnterThresholdMs) {
            _enterDebounceTimer?.cancel();
            _lastEnterTime = null;
            _sendSteer(_controller.text);
            return KeyEventResult.handled;
          }
        }

        _lastEnterTime = now;
        _enterDebounceTimer?.cancel();

        _enterDebounceTimer = Timer(
          const Duration(milliseconds: _doubleEnterThresholdMs),
          () {
            _lastEnterTime = null;
            _sendNormal(_controller.text);
          },
        );
        return KeyEventResult.handled;
      }

      // Arrow up/down navigates slash autocomplete when visible
      if (_showSlashAutocomplete) {
        final matches = _slashRegistry.match(_slashQuery);
        final maxIdx = (matches.length > 8 ? 8 : matches.length) - 1;
        if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
          setState(() {
            _slashSelectedIndex =
                (_slashSelectedIndex + 1).clamp(0, maxIdx < 0 ? 0 : maxIdx);
          });
          return KeyEventResult.handled;
        }
        if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
          setState(() {
            _slashSelectedIndex =
                (_slashSelectedIndex - 1).clamp(0, maxIdx < 0 ? 0 : maxIdx);
          });
          return KeyEventResult.handled;
        }
      }

      if (event.logicalKey == LogicalKeyboardKey.escape) {
        if (_showSlashAutocomplete) {
          setState(() {
            _showSlashAutocomplete = false;
            _slashQuery = '';
            _ghostText = null;
          });
          return KeyEventResult.handled;
        }
        _focusNode.unfocus();
        return KeyEventResult.handled;
      }

      // Tab accepts ghost text (single slash command match) or cycles focus
      if (event.logicalKey == LogicalKeyboardKey.tab) {
        if (_ghostText != null) {
          _controller.text = _ghostText!;
          final ghostLen = _ghostText!.length;
          _controller.selection = TextSelection.collapsed(
            offset: ghostLen,
          );
          _ghostText = null;
          setState(() {
            _showSlashAutocomplete = false;
            _slashQuery = '';
          });
          return KeyEventResult.handled;
        }
        _focusNode.unfocus();
        return KeyEventResult.handled;
      }
    }

    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    // Listen for focus-input requests from the shortcut system
    ref.listen<bool>(
      focusInputRequestProvider,
      (previous, next) {
        if (next) {
          _focusNode.requestFocus();
          if (_controller.text.isEmpty) {
            _controller.text = '/';
            _controller.selection = const TextSelection.collapsed(offset: 1);
          }
          ref.read(focusInputRequestProvider.notifier).state = false;
        }
      },
    );

    return Focus(
      onKeyEvent: _handleKeyEvent,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: const BoxDecoration(
          color: CyberpunkColors.black,
          border: Border(
            top: BorderSide(color: CyberpunkColors.orangePrimary, width: 1),
          ),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (_showSlashAutocomplete)
              SlashAutocomplete(
                query: _slashQuery,
                selectedIndex: _slashSelectedIndex,
                onSelected: _onSlashSelected,
                onDismiss: () {
                  setState(() {
                    _showSlashAutocomplete = false;
                    _slashQuery = '';
                  });
                },
              ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                const SizedBox(width: 8),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: CyberpunkColors.black,
                      border: Border.all(
                        color: CyberpunkColors.midGray,
                        width: 1,
                      ),
                      borderRadius: BorderRadius.circular(4),
                    ),
                    child: TextField(
                      controller: _controller,
                      focusNode: _focusNode,
                      // Always enabled so users can type/edit during "thinking..." state.
                      // The send button already disables itself (onTap: null) when loading.
                      style: CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.greenSuccess,
                        fontFamily: 'SourceCodePro',
                      ),
                      // Hide the native bar cursor — the custom
                      // _TerminalCursorController.buildTextSpan renders
                      // a terminal-style blinking underscore instead.
                      cursorColor: Colors.transparent,
                      cursorWidth: 0,
                      decoration: const InputDecoration(
                        hintText: '',
                        hintStyle: CyberpunkTypography.bodySmall,
                        border: InputBorder.none,
                        contentPadding: EdgeInsets.zero,
                        isDense: true,
                      ),
                      minLines: _minLines,
                      maxLines: _maxLines,
                      keyboardType: TextInputType.multiline,
                      textCapitalization: TextCapitalization.none,
                      onSubmitted: (_) {},
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                _buildSendButton(),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildSendButton() {
    final chatState = ref.watch(chatProvider);
    return GestureDetector(
      onTap: chatState.isLoading ? null : () => _sendNormal(_controller.text),
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: chatState.isLoading
              ? CyberpunkColors.orangeDark
              : CyberpunkColors.orangePrimary,
          borderRadius: BorderRadius.circular(4),
        ),
        child: chatState.isLoading
            ? SizedBox(
                width: 18,
                height: 18,
                child: AnimatedOpacity(
                  opacity: chatState.isLoading ? 1.0 : 0.5,
                  duration: const Duration(milliseconds: 400),
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      CyberpunkColors.blackTransparent(0.8),
                    ),
                  ),
                ),
              )
            : const Icon(
                Icons.send,
                color: CyberpunkColors.black,
                size: 18,
              ),
      ),
    );
  }
}
