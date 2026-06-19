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
  int _previousMessageCount = 0;

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
    WidgetsBinding.instance.addPostFrameCallback((_) => _loadMessages());
  }

  @override
  void didUpdateWidget(ChatMessageList oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.sessionId != oldWidget.sessionId) {
      _previousMessageCount = 0;
      _loadMessages();
    }
  }

  Future<void> _loadMessages() async {
    await ref.read(chatProvider.notifier).loadMessages(widget.sessionId);
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
    final chatState = ref.watch(chatProvider);
    final sessionId = widget.sessionId;
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

    return NotificationListener<ScrollNotification>(
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
                ? const MessagePlaceholder()
                : ListView.builder(
                    controller: _scrollController,
                    padding: EdgeInsets.fromLTRB(
                        16, findVisible ? 56 : 16, 16, chatState.error != null ? 100 : 16),
                    reverse: false,
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
                        // Dynamic progress indicator or fallback thinking
                        if (chatState.currentProgress != null) {
                          return AnimatedSwitcher(
                            duration: const Duration(milliseconds: 150),
                            switchInCurve: Curves.easeIn,
                            switchOutCurve: Curves.easeOut,
                            child: AgentProgressIndicator(
                              key: ValueKey(
                                '${chatState.currentProgress!.message}-${chatState.currentProgress!.timestamp.millisecondsSinceEpoch}',
                              ),
                              progress: chatState.currentProgress!,
                            ),
                          );
                        } else {
                          return const Padding(
                            padding: EdgeInsets.symmetric(vertical: 8),
                            child: Row(
                              children: [
                                SizedBox(
                                  width: 16,
                                  height: 16,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    valueColor: AlwaysStoppedAnimation<Color>(
                                      CyberpunkColors.orangePrimary,
                                    ),
                                  ),
                                ),
                                SizedBox(width: 8),
                                Text(
                                  'thinking...',
                                  style: CyberpunkTypography.bodySmall,
                                ),
                              ],
                            ),
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
                  onDismiss: () => ref.read(chatProvider.notifier).clearError(),
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
    );
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
