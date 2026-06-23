import 'dart:async';
import 'dart:io' show File;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/slash_commands.dart';
import '../../models/api_models.dart' show Attachment;
import '../../services/sdk_client.dart' show SdkApiClient;
import '../../services/skills_service.dart' show SkillsService;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'slash_autocomplete.dart';

/// Image file extensions that should be uploaded as multimodal image parts
/// rather than appended as plain-text references.
const Set<String> _kImageExtensions = {
  '.png',
  '.jpg',
  '.jpeg',
  '.gif',
  '.webp',
};

/// Returns true if [path] has a recognised image file extension.
bool _isImagePath(String path) {
  final dot = path.lastIndexOf('.');
  if (dot < 0) return false;
  final ext = path.substring(dot).toLowerCase();
  return _kImageExtensions.contains(ext);
}

/// Guess the MIME type from the image file extension.
String _guessImageMime(String path) {
  final dot = path.lastIndexOf('.');
  if (dot < 0) return 'application/octet-stream';
  final ext = path.substring(dot).toLowerCase();
  return switch (ext) {
    '.png' => 'image/png',
    '.jpg' || '.jpeg' => 'image/jpeg',
    '.gif' => 'image/gif',
    '.webp' => 'image/webp',
    _ => 'application/octet-stream',
  };
}

/// Extract the basename from a path string (handles both / and \).
String _basename(String path) {
  final i = path.lastIndexOf(RegExp(r'[/\\]'));
  return i < 0 ? path : path.substring(i + 1);
}

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

  // Cached skill names for /skill autocomplete (populated async on init).
  List<String> _skillNames = const [];

  // File path attachments — typed [Attachment] entries once uploaded,
  // plus raw path strings pending async upload.
  final List<Attachment> _attachments = [];
  final List<String> _pendingFilePaths = [];
  // Paths already dispatched to an in-flight upload, to avoid double-send.
  final Set<String> _inFlightUploads = {};
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
    // Fire-and-forget skill fetch for /skill autocomplete.  Does not block
    // input rendering; if it fails, we silently fall back to no suggestions.
    unawaited(_loadSkillNames());
  }

  /// Fetch skill names via [SkillsService] (backed by [SdkApiClient]) so the
  /// autocomplete popup can offer them after `/skill `.
  Future<void> _loadSkillNames() async {
    try {
      final sdk = ref.read(sdkClientProvider);
      final service = SkillsService(sdk);
      final names = await service.getSkillNames();
      if (!mounted) return;
      setState(() {
        _skillNames = names;
      });
    } catch (e) {
      debugPrint('[chat_input] skill fetch failed: $e');
    }
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
  ///
  /// Non-image paths are stored as raw strings in [_pendingFilePaths] and
  /// later rendered as text references in [_preparePayload].  Image paths
  /// (.png/.jpg/.jpeg/.gif/.webp) are dispatched to [_uploadDetectedImage]
  /// for asynchronous upload via [SdkApiClient.uploadFile]; on success
  /// they become typed [Attachment] entries which [_buildParts] turns into
  /// multimodal `image_url` parts.
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
      if (!_looksLikeFilePath(candidate)) continue;

      if (_isImagePath(candidate)) {
        // Avoid double-uploading the same path
        if (_inFlightUploads.contains(candidate)) continue;
        if (_attachments.any((a) => a.filename == _basename(candidate))) {
          continue;
        }
        _pendingFilePaths.add(candidate);
        _inFlightUploads.add(candidate);
        // Fire-and-forget — updates state on completion.
        unawaited(_uploadDetectedImage(candidate));
      } else {
        if (!_pendingFilePaths.contains(candidate)) {
          _pendingFilePaths.add(candidate);
        }
      }
    }
  }

  /// Upload a detected image path to the daemon.  Reads the file bytes
  /// via [File].  On success the path is removed from [_pendingFilePaths]
  /// and a typed [Attachment] is added.  On failure the path remains in
  /// [_pendingFilePaths] so it can be sent as a plain-text reference.
  Future<void> _uploadDetectedImage(String path) async {
    try {
      final file = File(path);
      if (!await file.exists()) return;
      final bytes = await file.readAsBytes();

      if (!mounted) return;
      final filename = _basename(path);
      final mime = _guessImageMime(path);
      final sdk = ref.read(sdkClientProvider);
      final upload = await sdk.uploadFile(
        Uint8List.fromList(bytes),
        filename,
        mime,
      );
      if (upload == null) return;
      final uploads = upload['uploads'] as List?;
      if (uploads == null || uploads.isEmpty) return;
      final uploadData = uploads.first as Map<String, dynamic>;

      if (!mounted) return;
      setState(() {
        _attachments.add(Attachment(
          uploadId: uploadData['id'] as String? ?? '',
          filename: filename,
          mimeType: uploadData['mime_type'] as String? ?? mime,
          sizeBytes: (uploadData['size_bytes'] as num?)?.toInt() ??
              bytes.length,
        ));
        _pendingFilePaths.remove(path);
      });
    } catch (e) {
      debugPrint('[chat_input] image upload failed for $path: $e');
    } finally {
      _inFlightUploads.remove(path);
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
      // For `/skill <name>` we keep the full text as the query so the
      // autocomplete can filter skill names by the argument prefix.
      final isSkillArgs = spaceIdx != -1 && text.substring(0, spaceIdx) == '/skill';
      final query = isSkillArgs
          ? text
          : (spaceIdx == -1 ? text : text.substring(0, spaceIdx));
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

  /// Called when the user accepts a skill-name suggestion in the
  /// `/skill <name>` autocomplete.  Inserts `/skill <name> ` and dismisses
  /// the popup.
  void _onSkillNameSelected(String name) {
    final text = '/skill $name ';
    setState(() {
      _controller.text = text;
      _controller.selection = TextSelection.collapsed(offset: text.length);
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
    // Append non-image file path references (e.g. source files for review).
    if (_pendingFilePaths.isNotEmpty) {
      expanded += '\n\n[attachments: ${_pendingFilePaths.join(', ')}]';
    }
    return expanded;
  }

  /// Build multimodal content parts for a send.
  ///
  /// Each uploaded image attachment becomes an `image_url` part; the
  /// user's text (with paste tokens expanded) becomes the trailing
  /// `text` part.  Non-image file paths are NOT included here because
  /// they are already surfaced via [_preparePayload] in the text path.
  List<Map<String, dynamic>> _buildParts(String text) {
    final parts = <Map<String, dynamic>>[];
    for (final attachment in _attachments) {
      parts.add({
        'type': 'image_url',
        'image_url': {'url': 'file://${attachment.uploadId}'},
      });
    }
    final expanded = _expandPastes(text.trim());
    // Even when we have attachments, keep non-image file path references in
    // the text body so the agent can read them.
    if (_pendingFilePaths.isNotEmpty) {
      final attachmentRef =
          '\n\n[attachments: ${_pendingFilePaths.join(', ')}]';
      parts.add({'type': 'text', 'text': '$expanded$attachmentRef'});
    } else if (expanded.isNotEmpty) {
      parts.add({'type': 'text', 'text': expanded});
    }
    return parts;
  }

  /// Remove an attachment when the user taps its chip.
  void _removeAttachment(Attachment attachment) {
    setState(() {
      _attachments.remove(attachment);
    });
  }

  /// Reset all input state after sending or handling a command.
  void _resetInputState() {
    _controller.text = '';
    _previousText = '';
    _pasteStore.clear();
    _pasteCounter = 0;
    _attachments.clear();
    _pendingFilePaths.clear();
    _inFlightUploads.clear();
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
    // Multimodal path: when image attachments are present, build structured
    // content parts and route via sendMessageWithParts.  Slash commands are
    // text-only and never run in this branch.
    if (_attachments.isNotEmpty) {
      final parts = _buildParts(text);
      if (parts.isEmpty) return;
      final expanded = _expandPastes(text.trim());
      final chatNotifier = ref.read(chatProvider.notifier);
      final activeAgent = ref.read(activeAgentProvider);
      chatNotifier.sendMessageWithParts(
        sessionId: widget.sessionId,
        text: expanded.isNotEmpty ? expanded : '(image attached)',
        parts: parts,
        agentId: activeAgent?.id ?? 'coder',
      );
      _resetInputState();
      return;
    }

    // Original text-only path
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
          if (_slashQuery.startsWith('/skill ')) {
            // Skill-name mode: dispatch to the skill handler.
            final arg = _slashQuery.substring('/skill '.length).trim();
            final matches = _skillNames
                .where((n) => n.toLowerCase().startsWith(arg.toLowerCase()))
                .take(8)
                .toList();
            if (matches.isNotEmpty) {
              final idx = _slashSelectedIndex.clamp(0, matches.length - 1);
              _onSkillNameSelected(matches[idx]);
            }
            return KeyEventResult.handled;
          }
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
        // Compute the visible item count based on the current mode.
        final int visibleCount;
        if (_slashQuery.startsWith('/skill ')) {
          final arg = _slashQuery.substring('/skill '.length).trim();
          final sm = _skillNames
              .where((n) => n.toLowerCase().startsWith(arg.toLowerCase()))
              .take(8)
              .toList();
          visibleCount = sm.length;
        } else {
          final matches = _slashRegistry.match(_slashQuery);
          visibleCount = matches.length > 8 ? 8 : matches.length;
        }
        final maxIdx = visibleCount - 1;
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

    final hasFocus = _focusNode.hasFocus;

    return Focus(
      onKeyEvent: _handleKeyEvent,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          border: Border(
            top: BorderSide(
              color: hasFocus
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.midGray,
              width: 1,
            ),
          ),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (_showSlashAutocomplete)
              SlashAutocomplete(
                query: _slashQuery,
                selectedIndex: _slashSelectedIndex,
                skillNames: _skillNames,
                onSkillSelected: _onSkillNameSelected,
                onSelected: _onSlashSelected,
                onDismiss: () {
                  setState(() {
                    _showSlashAutocomplete = false;
                    _slashQuery = '';
                  });
                },
              ),
            // Attachment chips — image uploads (typed Attachment) plus
            // pending file paths that haven't finished uploading yet.
            if (_attachments.isNotEmpty || _pendingFilePaths.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: <Widget>[
                      for (final a in _attachments)
                        Padding(
                          padding: const EdgeInsets.only(right: 4),
                          child: GestureDetector(
                            onTap: () => _removeAttachment(a),
                            child: Text(
                              '[${a.filename}]',
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.greenSuccess,
                                fontSize: 11,
                              ),
                            ),
                          ),
                        ),
                      for (final p in _pendingFilePaths)
                        Padding(
                          padding: const EdgeInsets.only(right: 4),
                          child: Text(
                            '[${_basename(p)}...]',
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.orangeGlow,
                              fontSize: 11,
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                const SizedBox(width: 8),
                Expanded(
                  child: TextField(
                    controller: _controller,
                    focusNode: _focusNode,
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.greenSuccess,
                      fontFamily: 'SourceCodePro',
                    ),
                    cursorColor: Colors.transparent,
                    cursorWidth: 0,
                    decoration: const InputDecoration(
                      hintText: '',
                      hintStyle: CyberpunkTypography.bodySmall,
                      border: InputBorder.none,
                      contentPadding: EdgeInsets.symmetric(horizontal: 8, vertical: 10),
                      isDense: true,
                      filled: true,
                      fillColor: CyberpunkColors.black,
                    ),
                    minLines: _minLines,
                    maxLines: _maxLines,
                    keyboardType: TextInputType.multiline,
                    textCapitalization: TextCapitalization.none,
                    onSubmitted: (_) {},
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
