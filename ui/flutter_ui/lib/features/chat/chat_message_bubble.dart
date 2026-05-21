import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';

class ChatMessageBubble extends StatelessWidget {
  final ChatMessage message;

  const ChatMessageBubble({
    super.key,
    required this.message,
  });

  @override
  Widget build(BuildContext context) {
    final isUser = message.role == 'user';

    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.7,
        ),
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: isUser
              ? CyberpunkColors.orangePrimary.withOpacity(0.2)
              : CyberpunkColors.midGray,
          border: Border.all(
            color:
                isUser ? CyberpunkColors.orangePrimary : CyberpunkColors.lightGray,
            width: 1,
          ),
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(8),
            topRight: const Radius.circular(8),
            bottomLeft: Radius.circular(isUser ? 8 : 2),
            bottomRight: Radius.circular(isUser ? 2 : 8),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              message.content,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeGlow,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              _formatTime(message.timestamp),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
                fontSize: 10,
              ),
            ),
          ],
        ),
      ),
    );
  }

  String _formatTime(DateTime time) {
    final hour = time.hour.toString().padLeft(2, '0');
    final minute = time.minute.toString().padLeft(2, '0');
    return '$hour:$minute';
  }
}
