import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/constants.dart';
import '../../core/slash_commands.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/providers.dart';
import 'slash_autocomplete.dart';

/// Chat input widget - auto-expanding bottom pane with paste detection,
/// double-enter steer, shift+enter newline, and slash command autocomplete.
class ChatInput extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatInput({super.key, required this.sessionId});

  @override
  ConsumerState<ChatInput> createState() => _ChatInputState();
}

class _ChatInputState extends ConsumerState<ChatInput> {
  final _controller = TextEditingController();
  final _focusNode = FocusNode();
  String _selectedAgent = 'coder';

  // Paste detection state
  String _previousText = '';
  final Map<int, String> _pasteStore = {};
  int _pasteCounter = 0;

  // Double-enter detection
  DateTime? _lastEnterTime;
  Timer? _enterDebounceTimer;
  static const _doubleEnterThresholdMs = 300;

  // Auto-expand height tracking
  static const int _minLines = 2;
  static const int _maxLines = 8;

  // Slash autocomplete state
  bool _showSlashAutocomplete = false;
  String _slashQuery = '';

  // Ghost text for single slash match
  String? _ghostText;

  // Slash command registry
  static final _slashRegistry = SlashCommandRegistry();

  // File path attachments
  final List<String> _attachments = [];

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onTextChanged);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Listen for focus-input requests from the shortcut system
    ref.listen<bool>(
      focusInputRequestProvider,
      (previous, next) {
        if (next) {
          _focusNode.requestFocus();
          // Prefill '/' if input is empty
          if (_controller.text.isEmpty) {
            _controller.text = '/';
            _controller.selection = TextSelection.collapsed(
              offset: 1,
            );
          }
          ref.read(focusInputRequestProvider.notifier).state = false;
        }
      },
    );
  }

  @override
  void dispose() {
    _enterDebounceTimer?.cancel();
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
      });
    } else {
      if (_showSlashAutocomplete) {
        setState(() {
          _showSlashAutocomplete = false;
          _slashQuery = '';
        });
      }
    }
  }

  void _onSlashSelected(SlashCommand command) {
    setState(() {
      _controller.text = '${command.name} ';
      _controller.selection = TextSelection.collapsed(
        offset: _controller.text.length,
      );
      _showSlashAutocomplete = false;
      _slashQuery = '';
    });
    _focusNode.requestFocus();
  }

  /// Detect paste operations by checking for large line count jumps
  void _detectPaste(String currentText) {
    if (currentText.length < _previousText.length) return;

    final added = currentText.substring(_previousText.length);
    final addedLines = '\n'.allMatches(added).length + 1;

    if (addedLines >= 3) {
      _pasteCounter++;
      final token = '{paste:$_pasteCounter}';
      _pasteStore[_pasteCounter] = added;

      final newText = _previousText + token;
      _controller.text = newText;
      _controller.selection = TextSelection.collapsed(offset: newText.length);
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

  void _sendNormal(String text) {
    final payload = _preparePayload(text);
    if (payload.isEmpty) return;

    final chatNotifier = ref.read(chatProvider.notifier);
    final activeAgent = ref.read(activeAgentProvider);

    chatNotifier.sendMessage(
      sessionId: widget.sessionId,
      text: payload,
      agentId: activeAgent?.id ?? _selectedAgent,
    );

    _controller.clear();
    _previousText = '';
    _pasteStore.clear();
    _pasteCounter = 0;
    _attachments.clear();
    _ghostText = null;
    _showSlashAutocomplete = false;
    _slashQuery = '';
  }

  void _sendSteer(String text) {
    final payload = _preparePayload(text);
    if (payload.isEmpty) return;

    ref.read(chatProvider.notifier).sendSteer(
      sessionId: widget.sessionId,
      text: payload,
    );

    _controller.clear();
    _previousText = '';
    _pasteStore.clear();
    _pasteCounter = 0;
    _attachments.clear();
    _ghostText = null;
    _showSlashAutocomplete = false;
    _slashQuery = '';
  }

  /// Handle raw key events for Enter, Shift+Enter, Escape
  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent) {
      if (event.logicalKey == LogicalKeyboardKey.enter) {
        final isShiftPressed = HardwareKeyboard.instance.isShiftPressed;

        if (isShiftPressed) {
          final text = _controller.text;
          final selection = _controller.selection;
          final newText = text.replaceRange(
            selection.start,
            selection.end,
            '\n',
          );
          _controller.text = newText;
          _controller.selection = TextSelection.collapsed(
            offset: selection.start + 1,
          );
          return KeyEventResult.handled;
        }

        // If slash autocomplete is showing, let it handle Enter
        if (_showSlashAutocomplete) {
          return KeyEventResult.ignored;
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

        // Check configured double-enter behavior from storage
        final storage = ref.read(storageProvider);
        final mode = storage.getDoubleEnter();

        _enterDebounceTimer = Timer(
          const Duration(milliseconds: _doubleEnterThresholdMs),
          () {
            _lastEnterTime = null;
            if (mode == 'steer') {
              _sendNormal(_controller.text);
            } else {
              // interrupt / preempt — not yet wired to backend endpoints
              _sendNormal(_controller.text);
            }
          },
        );
        return KeyEventResult.handled;
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
        // Close drawer if open
        final drawerState = ref.read(drawerOpenProvider);
        if (drawerState) {
          ref.read(drawerOpenProvider.notifier).state = false;
          return KeyEventResult.handled;
        }
        _focusNode.unfocus();
        return KeyEventResult.handled;
      }

      // Tab accepts ghost text (single slash command match) or cycles focus
      if (event.logicalKey == LogicalKeyboardKey.tab) {
        if (_ghostText != null) {
          _controller.text = _ghostText!;
          _controller.selection = TextSelection.collapsed(
            offset: _controller.text.length,
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
    final chatState = ref.watch(chatProvider);

    return Focus(
      onKeyEvent: _handleKeyEvent,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: const BoxDecoration(
          color: CyberpunkColors.darkGray,
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
                _buildAgentSelector(),
                const SizedBox(width: 8),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                    decoration: BoxDecoration(
                      color: CyberpunkColors.black,
                      border: Border.all(
                        color: CyberpunkColors.midGray,
                        width: 1,
                      ),
                      borderRadius: BorderRadius.circular(4),
                    ),
                    child: Stack(
                      alignment: Alignment.centerLeft,
                      children: [
                        TextField(
                          controller: _controller,
                          focusNode: _focusNode,
                          enabled: !chatState.isLoading,
                          style: CyberpunkTypography.bodyMedium.copyWith(
                            color: CyberpunkColors.greenSuccess,
                            fontFamily: 'SourceCodePro',
                          ),
                          cursorColor: CyberpunkColors.orangePrimary,
                          decoration: InputDecoration(
                            hintText: chatState.isLoading
                                ? 'sending...'
                                : 'enter command...',
                            hintStyle: CyberpunkTypography.bodySmall,
                            border: InputBorder.none,
                            contentPadding: EdgeInsets.zero,
                            isDense: true,
                            // Ghost text shown as suffix when single match
                            suffix: _ghostText != null &&
                                    _controller.text.isNotEmpty &&
                                    _ghostText!.startsWith(_controller.text)
                                ? Text(
                                    _ghostText!.substring(_controller.text.length),
                                    style: CyberpunkTypography.bodyMedium.copyWith(
                                      color: CyberpunkColors.midGray,
                                      fontFamily: 'SourceCodePro',
                                    ),
                                  )
                                : null,
                          ),
                          minLines: _minLines,
                          maxLines: _maxLines,
                          keyboardType: TextInputType.multiline,
                          textCapitalization: TextCapitalization.sentences,
                          onSubmitted: (_) {},
                        ),
                      ],
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

  Widget _buildAgentSelector() {
    final agents = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    final selectedAgentId = activeAgent?.id ?? _selectedAgent;

    return PopupMenuButton<String>(
      onSelected: (String agentId) {
        final agent = agents.agents.firstWhere(
          (a) => a.id == agentId,
          orElse: () => Agent(
            id: agentId,
            name: agentId,
            description: '',
            prompt: '',
            enabled: true,
          ),
        );
        ref.read(activeAgentProvider.notifier).state = agent;
        setState(() {
          _selectedAgent = agentId;
        });
      },
      itemBuilder: (BuildContext context) {
        if (agents.isLoading) {
          return [
            const PopupMenuItem<String>(
              enabled: false,
              value: '__loading__',
              child: SizedBox(
                width: 120,
                child: LinearProgressIndicator(),
              ),
            ),
          ];
        }

        final apiAgentIds = agents.agents.map((a) => a.id).toSet();
        final allAgents = <Agent>[
          if (!apiAgentIds.contains(_selectedAgent))
            Agent(
              id: _selectedAgent,
              name: _selectedAgent,
              description: '',
              prompt: '',
              enabled: true,
            ),
          ...agents.agents,
        ];

        return allAgents.map((Agent agent) {
          return PopupMenuItem<String>(
            value: agent.id,
            child: Row(
              children: [
                Icon(
                  getAgentIcon(agent.id),
                  size: 16,
                  color: agent.id == selectedAgentId
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.greenSuccess,
                ),
                const SizedBox(width: 8),
                Text(
                  agent.name,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    fontFamily: 'SourceCodePro',
                    color: agent.id == selectedAgentId
                        ? CyberpunkColors.orangePrimary
                        : null,
                  ),
                ),
              ],
            ),
          );
        }).toList();
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          border: Border.all(color: CyberpunkColors.orangePrimary, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              getAgentIcon(selectedAgentId),
              size: 16,
              color: CyberpunkColors.orangePrimary,
            ),
            const SizedBox(width: 6),
            Text(
              activeAgent?.name ?? _selectedAgent,
              style: CyberpunkTypography.label.copyWith(
                fontSize: 10,
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Icon(
              Icons.expand_more,
              size: 14,
              color: CyberpunkColors.orangePrimary,
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
