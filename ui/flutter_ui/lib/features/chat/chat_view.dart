import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'chat_message_list.dart';
import 'chat_input.dart';

/// Chat view - main chat pane with header, message list, and input
class ChatView extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatView({super.key, required this.sessionId});

  @override
  ConsumerState<ChatView> createState() => _ChatViewState();
}

class _ChatViewState extends ConsumerState<ChatView> {
  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          // Chat header
          _buildHeader(),
          const Divider(height: 1, color: CyberpunkColors.midGray),
          // Message list
          Expanded(
            child: ChatMessageList(sessionId: widget.sessionId),
          ),
          // Input area
          ChatInput(sessionId: widget.sessionId),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      color: CyberpunkColors.darkGray,
      child: Row(
        children: [
          Consumer(
            builder: (context, ref, _) {
              final connected = ref.watch(connectionStateProvider);
              return Container(
                width: 10,
                height: 10,
                decoration: BoxDecoration(
                  color: connected
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
                  shape: BoxShape.circle,
                ),
              );
            },
          ),
          const SizedBox(width: 8),
          Consumer(
            builder: (context, ref, _) {
              final connected = ref.watch(connectionStateProvider);
              return Text(
                connected ? 'chat active' : 'daemon disconnected',
                style: CyberpunkTypography.label.copyWith(
                  color: connected
                      ? CyberpunkColors.greenSuccess
                      : CyberpunkColors.redAlert,
                ),
              );
            },
          ),
          const Spacer(),
          Text(
            widget.sessionId == 'default' ? 'new session' : 'session: ${widget.sessionId.length >= 8 ? widget.sessionId.substring(0, 8) : widget.sessionId}',
            style: CyberpunkTypography.bodySmall,
          ),
        ],
      ),
    );
  }
}
