import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'chat_message_list.dart';
import 'chat_input.dart';

/// Chat view - main chat pane with header, message list, and input
class ChatView extends StatefulWidget {
  final String sessionId;

  const ChatView({super.key, required this.sessionId});

  @override
  State<ChatView> createState() => _ChatViewState();
}

class _ChatViewState extends State<ChatView> {
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
          Container(
            width: 10,
            height: 10,
            decoration: const BoxDecoration(
              color: CyberpunkColors.greenSuccess,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 8),
          Text(
            'chat active',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.greenSuccess,
            ),
          ),
          const Spacer(),
          Text(
            'session: ${widget.sessionId.substring(0, 8)}',
            style: CyberpunkTypography.bodySmall,
          ),
        ],
      ),
    );
  }
}
