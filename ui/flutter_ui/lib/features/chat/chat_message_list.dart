import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../widgets/error_banner.dart';
import '../../providers/chat_provider.dart';
import 'chat_message_bubble.dart';

/// Chat message list - displays chat messages with auto-scroll
class ChatMessageList extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatMessageList({super.key, required this.sessionId});

  @override
  ConsumerState<ChatMessageList> createState() => _ChatMessageListState();
}

class _ChatMessageListState extends ConsumerState<ChatMessageList> {
  final ScrollController _scrollController = ScrollController();
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
    final isSending = ref.read(chatProvider.notifier).isSending;

    return chatState.when(
      initial: () => const MessagePlaceholder(),
      loading: () => const Center(
        child: CircularProgressIndicator(
          valueColor: AlwaysStoppedAnimation<Color>(
            CyberpunkColors.orangePrimary,
          ),
        ),
      ),
      error: (error, _) => _ChatError(
        message: error.toString(),
        onDismiss: () => ref.read(chatProvider.notifier).clearError(),
      ),
      data: (messages) {
        // Auto-scroll when new messages arrive and user is at bottom
        if (messages.isNotEmpty &&
            _isAtBottom &&
            messages.length != _previousMessageCount) {
          _previousMessageCount = messages.length;
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted) _scrollToBottom();
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
                child: messages.isEmpty
                    ? const MessagePlaceholder()
                    : ListView.builder(
                        controller: _scrollController,
                        padding: const EdgeInsets.all(16),
                        reverse: false,
                        itemCount: messages.length + (isSending ? 1 : 0),
                        itemBuilder: (context, index) {
                          if (index < messages.length) {
                            final message = messages[index];
                            return ChatMessageBubble(message: message);
                          } else {
                            // Loading indicator for pending message
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
                        },
                      ),
              ),
            ],
          ),
        );
      },
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

class _ChatError extends StatelessWidget {
  final String message;
  final VoidCallback onDismiss;

  const _ChatError({required this.message, required this.onDismiss});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ErrorBanner(message: message, onDismiss: onDismiss),
            const SizedBox(height: 12),
            FilledButton.tonal(
              onPressed: onDismiss,
              child: const Text('dismiss', style: CyberpunkTypography.bodySmall),
            ),
          ],
        ),
      ),
    );
  }
}
