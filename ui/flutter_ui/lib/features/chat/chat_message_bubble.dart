import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../theme/markdown_style.dart';
import '../../theme/syntax_highlighter.dart';
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
    final isSystem = message.role == 'system';

    // System messages have distinct styling for errors and notifications
    if (isSystem) {
      return Container(
        width: double.infinity,
        margin: const EdgeInsets.symmetric(vertical: 8, horizontal: 12),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.redAlert.withValues(alpha: 0.15),
          border: Border.all(
            color: CyberpunkColors.redAlert,
            width: 1,
          ),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(
                  Icons.error_outline,
                  color: CyberpunkColors.redAlert,
                  size: 16,
                ),
                const SizedBox(width: 8),
                Text(
                  'system',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.redAlert,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              message.content,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.redAlert,
              ),
            ),
          ],
        ),
      );
    }

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
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.2)
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
            MarkdownBody(
              data: message.content,
              styleSheet: buildCyberpunkMarkdownStyle(context).copyWith(
                p: isUser
                    ? CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.orangeGlow,
                      )
                    : null,
              ),
              selectable: true,
              syntaxHighlighter: CyberpunkSyntaxHighlighter(),
              softLineBreak: true,
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
