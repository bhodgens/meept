import 'dart:async' show Timer;
import 'dart:math' show max;

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/error_banner.dart';
import '../../providers/chat_provider.dart';
import 'agent_progress_indicator.dart';
import 'chat_message_bubble.dart';
import 'find_bar.dart';
import 'find_state.dart';
import 'scroll_state.dart';

/// Chat message list - displays chat messages with auto-scroll
class ChatMessageList extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatMessageList({super.key, required this.sessionId});

  @override
  ConsumerState<ChatMessageList> createState() => _ChatMessageListState();
}

class _ChatMessageListState extends ConsumerState<ChatMessageList> {
  final ScrollController _scrollController = ScrollController();
  final Map<String, GlobalKey> _messageKeys = {};
  bool _isAtBottom = true;
  /// Guards against re-entrant scroll-back fetches from _onScroll.
  bool _loadingOlder = false;
  /// Reactive mirror of [!_isAtBottom]; drives the scroll-to-bottom
  /// button visibility. _isAtBottom alone is a plain field the build
  /// method cannot observe.
  bool _showScrollToBottom = false;
  int _previousMessageCount = 0;

  @override
  void initState() {
    super.initState();
    debugPrint('[session-debug] ChatMessageList.initState sessionId=${widget.sessionId}');
    _scrollController.addListener(_onScroll);
    WidgetsBinding.instance.addPostFrameCallback((_) => _loadMessages());
  }

  @override
  void didUpdateWidget(ChatMessageList oldWidget) {
    super.didUpdateWidget(oldWidget);
    debugPrint('[session-debug] ChatMessageList.didUpdateWidget old=${oldWidget.sessionId} new=${widget.sessionId} changed=${widget.sessionId != oldWidget.sessionId}');
    if (widget.sessionId != oldWidget.sessionId) {
      _previousMessageCount = 0;
      // Defer to next frame so we don't modify providers during the build
      // phase (didUpdateWidget runs inside the build lifecycle).
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _loadMessages();
      });
    }
  }

  Future<void> _loadMessages() async {
    debugPrint('[session-debug] ChatMessageList._loadMessages sessionId=${widget.sessionId}');
    await ref.read(chatProvider(widget.sessionId).notifier);
    final st = ref.read(chatProvider(widget.sessionId));
    debugPrint('[session-debug] ChatMessageList._loadMessages done: messages=${st.messages.length} isLoading=${st.isLoading} error=${st.error}');
  }

  @override
  void dispose() {
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    final maxScroll = _scrollController.position.maxScrollExtent;
    final currentScroll = _scrollController.offset;
    _isAtBottom = currentScroll >= (maxScroll - 100);
    _updateScrollButton();
    // Scroll-back pagination: near the top, fetch the next older page.
    // Keep the viewport anchored by preserving the offset distance from
    // the top across the prepend (handled in _loadOlder).
    if (currentScroll <= 200) {
      _loadOlder();
    }
  }

  Future<void> _loadOlder() async {
    if (_loadingOlder) return;
    final notifier = ref.read(chatProvider(widget.sessionId).notifier);
    if (!notifier.hasMoreHistory || notifier.isLoadingOlder) return;
    _loadingOlder = true;
    // Preserve the user's position: remember the oldest visible message id
    // so we can re-anchor after the prepend shifts everything down.
    final previousOffset = _scrollController.offset;
    final previousMax = _scrollController.position.maxScrollExtent;
    try {
      await notifier.loadOlderMessages();
      if (!mounted) return;
      // After the frame with the prepended items, restore the scroll
      // offset relative to the new content height.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scrollController.hasClients) return;
        final delta =
            _scrollController.position.maxScrollExtent - previousMax;
        if (delta > 0) {
          _scrollController.jumpTo(previousOffset + delta);
        }
      });
    } finally {
      _loadingOlder = false;
    }
  }

  void _updateScrollButton() {
    final show = !_isAtBottom;
    if (show != _showScrollToBottom && mounted) {
      setState(() => _showScrollToBottom = show);
    }
  }

  void _scrollToBottom() {
    if (_scrollController.hasClients) {
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final chatState = ref.watch(chatProvider(widget.sessionId));
    final sessionId = widget.sessionId;
    debugPrint('[session-debug] ChatMessageList.build sessionId=$sessionId messages=${chatState.messages.length} isLoading=${chatState.isLoading} error=${chatState.error}');
    final findVisible = ref.watch(findBarVisibleProvider(sessionId));
    final findQuery = ref.watch(findQueryProvider(sessionId));
    final findCase = ref.watch(findCaseSensitiveProvider(sessionId));
    final findRegex = ref.watch(findRegexProvider(sessionId));
    final findCursor = ref.watch(findCursorProvider(sessionId));

    // Compute matches whenever query/toggles/messages change.
    final findResult = computeFindMatches(
      contents: chatState.messages.map((m) => m.content).toList(),
      query: findQuery,
      caseSensitive: findCase,
      regex: findRegex,
    );

    // Auto-scroll when new messages arrive and user is at bottom
    if (chatState.messages.isNotEmpty && _isAtBottom && chatState.messages.length != _previousMessageCount) {
      _previousMessageCount = chatState.messages.length;
      WidgetsBinding.instance.addPostFrameCallback((_) { if (mounted) _scrollToBottom(); });
    }

    // Pending scroll from search-result navigation.  Consume once the
    // target message is present in the list.
    final pendingScrollId = ref.read(pendingScrollMessageProvider(sessionId));
    if (pendingScrollId.isNotEmpty) {
      final targetIdx = chatState.messages.indexWhere(
        (m) => m.id == pendingScrollId,
      );
      if (targetIdx >= 0) {
        // Clear the pending request before scheduling the scroll so a
        // subsequent rebuild doesn't re-trigger it.
        ref.read(pendingScrollMessageProvider(sessionId).notifier).state = '';
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) _scrollToMessage(pendingScrollId);
        });
      }
    }

    // Auto-scroll to current find match when cursor changes.
    if (findVisible && findResult.matches.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _scrollToFindMatch(findResult.matches, findCursor, chatState.messages.length);
      });
    }

    return SelectionArea(
      child: NotificationListener<ScrollNotification>(
        onNotification: (notification) {
          if (notification is ScrollEndNotification) {
            final metrics = notification.metrics;
            _isAtBottom = metrics.pixels >= metrics.maxScrollExtent - 100;
          }
          return false;
        },
        child: Stack(
        children: [
          Positioned.fill(
            child: chatState.messages.isEmpty
                ? (chatState.isLoading
                    ? const _SessionLoadingPlaceholder()
                    : const MessagePlaceholder())
                : ListView.builder(
                    controller: _scrollController,
                    padding: EdgeInsets.fromLTRB(
                        16, findVisible ? 56 : 16, 16, chatState.error != null ? 100 : 16),
                    reverse: false,
                    physics: const ClampingScrollPhysics(),
                    itemCount:
                        chatState.messages.length + (chatState.isLoading || chatState.isAgentProcessing ? 1 : 0),
                    itemBuilder: (context, index) {
                      if (index < chatState.messages.length) {
                        final message = chatState.messages[index];
                        // Find matches belonging to this message, with their absolute index
                        // so we can mark the current one.
                        final localMatches = <int>[];
                        for (var i = 0; i < findResult.matches.length; i++) {
                          if (findResult.matches[i].messageIndex == index) {
                            localMatches.add(i);
                          }
                        }
                        // Assign a GlobalKey so scroll-to-message can measure
                        // this bubble's actual rendered position.
                        final key = _messageKeys.putIfAbsent(
                          message.id,
                          () => GlobalKey(),
                        );
                        return KeyedSubtree(
                          key: key,
                          child: ChatMessageBubble(
                            message: message,
                            highlightQuery: findVisible && findQuery.isNotEmpty ? findQuery : null,
                            caseSensitive: findCase,
                            isRegex: findRegex,
                            highlightRanges: localMatches
                                .map((absIdx) => findResult.matches[absIdx])
                                .toList(),
                            currentRangeAbsIndex: findCursor,
                            rangeAbsIndices: localMatches,
                            regexError: findResult.regexError,
                          ),
                        );
                      } else {
                        // Dynamic progress indicator or fallback thinking.
                        // Streaming stage: render the growing assistant
                        // text as a live preview bubble instead of a bare
                        // progress line.
                        final progress = chatState.currentProgress;
                        if (progress != null &&
                            progress.stage == 'streaming' &&
                            (progress.textSoFar?.isNotEmpty ?? false)) {
                          return AnimatedSwitcher(
                            duration: const Duration(milliseconds: 150),
                            switchInCurve: Curves.easeIn,
                            switchOutCurve: Curves.easeOut,
                            child: _StreamingPreview(
                              key: const ValueKey('streaming-preview'),
                              text: progress.textSoFar!,
                            ),
                          );
                        }
                        if (progress != null) {
                          return AnimatedSwitcher(
                            duration: const Duration(milliseconds: 150),
                            switchInCurve: Curves.easeIn,
                            switchOutCurve: Curves.easeOut,
                            child: AgentProgressIndicator(
                              key: ValueKey(
                                '${progress.message}-${progress.timestamp.millisecondsSinceEpoch}',
                              ),
                              progress: progress,
                            ),
                          );
                        } else {
                          return _ThinkingIndicator(
                            startedAt: chatState.thinkingStartedAt,
                          );
                        }
                      }
                    },
                  ),
          ),
          if (chatState.error != null)
            Positioned(
              bottom: 70, // Just above the chat input
              left: 0,
              right: 0,
              child: Padding(
                padding: const EdgeInsets.all(8),
                child: ErrorBanner(
                  message: chatState.error!,
                  onRetry: () => ref
                      .read(chatProvider(widget.sessionId).notifier)
                      .retryLastSend(),
                  onDismiss: () => ref.read(chatProvider(widget.sessionId).notifier).clearError(),
                ),
              ),
            ),
          if (_showScrollToBottom)
            Positioned(
              bottom: 70, // Above the chat input
              right: 16,
              child: Material(
                color: CyberpunkColors.darkGray,
                shape: const CircleBorder(),
                elevation: 4,
                child: IconButton(
                  tooltip: 'scroll to latest',
                  icon: Icon(
                    Icons.arrow_downward,
                    size: 20,
                    color: CyberpunkColors.orangePrimary,
                  ),
                  onPressed: _scrollToBottom,
                ),
              ),
            ),
          if (findVisible)
            Positioned(
              top: 0,
              left: 0,
              right: 0,
              child: FindBar(
                sessionId: sessionId,
                matchCount: findResult.matches.length,
                regexError: findResult.regexError,
              ),
            ),
        ],
      ),
    ),  // NotificationListener
  );  // SelectionArea
  }

  /// Scrolls so the current find match is visible.
  void _scrollToFindMatch(List<FindMatch> matches, int cursor, int messageCount) {
    if (!_scrollController.hasClients) return;
    if (matches.isEmpty || cursor < 0 || cursor >= matches.length) return;
    final target = matches[cursor];
    if (target.messageIndex < 0 || target.messageIndex >= messageCount) return;
    // Approximate: assume each message is ~64 logical pixels tall.
    const estHeight = 64.0;
    final viewport = _scrollController.position.viewportDimension;
    final offset = (target.messageIndex * estHeight) - viewport / 2;
    final clamped = offset.clamp(
      0.0,
      _scrollController.position.maxScrollExtent,
    );
    _scrollController.animateTo(
      clamped,
      duration: const Duration(milliseconds: 150),
      curve: Curves.easeOut,
    );
  }

  /// Scrolls so the message with [messageId] is visible and centered.
  ///
  /// Uses the GlobalKey assigned to each message bubble to measure its
  /// actual rendered position relative to the scroll viewport.  Falls back
  /// to the approximate (64px-per-message) computation if the key has not
  /// been registered yet or the render box is unavailable.
  void _scrollToMessage(String messageId) {
    if (!_scrollController.hasClients) return;
    final key = _messageKeys[messageId];
    if (key == null) return;
    final ctx = key.currentContext;
    if (ctx == null) return;
    final box = ctx.findRenderObject() as RenderBox?;
    if (box == null || !box.attached) return;

    final scrollable = Scrollable.of(ctx);
    final scrollRenderBox = scrollable.context.findRenderObject() as RenderBox?;
    if (scrollRenderBox == null) return;

    // Position of the target message relative to the scrollable's origin.
    final targetOffset = box.localToGlobal(
      Offset.zero,
      ancestor: scrollRenderBox,
    );
    final messageHeight = box.size.height;
    final viewport = _scrollController.position.viewportDimension;

    // Center the target in the viewport.
    final desiredOffset = _scrollController.offset +
        targetOffset.dy -
        (viewport - messageHeight) / 2;
    final clamped = desiredOffset.clamp(
      0.0,
      _scrollController.position.maxScrollExtent,
    );
    _scrollController.animateTo(
      clamped,
      duration: const Duration(milliseconds: 250),
      curve: Curves.easeOut,
    );
  }
}

/// Placeholder widget shown while session messages are being fetched.
///
/// Distinguishes the transient "loading empty" state (during a session swap
/// or initial fetch) from the genuine "no messages yet" state rendered by
/// [MessagePlaceholder].  Without this distinction the UI would briefly
/// flash "no messages yet / start the conversation" while the new session's
/// history is still being fetched from the daemon, making the chat look
/// empty rather than in-flight.
class _SessionLoadingPlaceholder extends StatelessWidget {
  const _SessionLoadingPlaceholder();

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: SizedBox(
        width: 24,
        height: 24,
        child: CircularProgressIndicator(
          strokeWidth: 2,
          valueColor: AlwaysStoppedAnimation<Color>(
            CyberpunkColors.orangePrimary,
          ),
        ),
      ),
    );
  }
}

/// Placeholder widget when no messages exist
class MessagePlaceholder extends StatelessWidget {
  const MessagePlaceholder({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.chat_bubble_outline,
            size: 64,
            color: CyberpunkColors.midGray,
          ),
          const SizedBox(height: 16),
          Text(
            'no messages yet',
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.lightGray,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'start the conversation',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.lightGray,
            ),
          ),
        ],
      ),
    );
  }
}

/// A "thinking Xs..." indicator with a live elapsed timer.
///
/// Renders a spinner next to the word "thinking" followed by an elapsed
/// duration. The widget starts a 1-second [Timer.periodic] on mount so the
/// label updates every second. When [startedAt] is null (unknown start),
/// falls back to the static "thinking..." label.
/// Live preview of the assistant's in-flight streaming response. Rendered
/// in the trailing list slot while progress.stage == "streaming"; replaced
/// by the real message bubble when the final chat_message WS event lands.
class _StreamingPreview extends StatelessWidget {
  final String text;

  const _StreamingPreview({super.key, required this.text});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.6),
        border: Border(
          left: BorderSide(
            color: CyberpunkColors.orangePrimary.withValues(alpha: 0.5),
            width: 2,
          ),
        ),
        borderRadius: const BorderRadius.only(
          topLeft: Radius.circular(2),
          topRight: Radius.circular(8),
          bottomLeft: Radius.circular(8),
          bottomRight: Radius.circular(8),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'assistant',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary.withValues(alpha: 0.7),
            ),
          ),
          const SizedBox(height: 4),
          SelectableText(
            text,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.veryLightGray,
            ),
          ),
        ],
      ),
    );
  }
}

class _ThinkingIndicator extends StatefulWidget {
  final DateTime? startedAt;

  const _ThinkingIndicator({this.startedAt});

  @override
  State<_ThinkingIndicator> createState() => _ThinkingIndicatorState();
}

class _ThinkingIndicatorState extends State<_ThinkingIndicator> {
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _startTimerIfNeeded();
  }

  @override
  void didUpdateWidget(_ThinkingIndicator oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.startedAt != widget.startedAt) {
      _timer?.cancel();
      _startTimerIfNeeded();
    }
  }

  void _startTimerIfNeeded() {
    if (widget.startedAt == null) return;
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  String _formatElapsed(Duration elapsed) {
    final s = max(0, elapsed.inSeconds);
    if (s < 60) return '${s}s';
    final m = s ~/ 60;
    final rs = s % 60;
    return '${m}m${rs}s';
  }

  @override
  Widget build(BuildContext context) {
    final startedAt = widget.startedAt;
    final label = startedAt == null
        ? 'thinking...'
        : 'thinking ${_formatElapsed(DateTime.now().difference(startedAt))}...';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          const SizedBox(
            width: 16,
            height: 16,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              valueColor: AlwaysStoppedAnimation<Color>(
                CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Text(label, style: CyberpunkTypography.bodySmall),
        ],
      ),
    );
  }
}
