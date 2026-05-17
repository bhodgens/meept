import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'chat_message_bubble.dart';

class ChatMessageList extends StatelessWidget {
  final String sessionId;

  const ChatMessageList({super.key, required this.sessionId});

  @override
  Widget build(BuildContext context) {
    // Placeholder - will be connected to actual message stream
    return ListView.builder(
      padding: const EdgeInsets.all(16),
      reverse: false,
      itemCount: 0, // Will be populated from actual messages
      itemBuilder: (context, index) {
        return const SizedBox.shrink(); // Placeholder
      },
    );
  }
}

class MessagePlaceholder extends StatelessWidget {
  const MessagePlaceholder({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.chat_bubble_outline,
            size: 64,
            color: CyberpunkColors.midGray,
          ),
          const SizedBox(height: 16),
          Text(
            'NO MESSAGES YET',
            style: TextStyle(
              fontFamily: 'JetBrainsMono',
              color: CyberpunkColors.lightGray,
              fontSize: 14,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'Start the conversation',
            style: TextStyle(
              fontFamily: 'JetBrainsMono',
              color: CyberpunkColors.lightGray,
              fontSize: 12,
            ),
          ),
        ],
      ),
    );
  }
}
