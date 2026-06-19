import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../theme/markdown_style.dart';
import '../../theme/syntax_highlighter.dart';
import '../../models/api_models.dart';
import 'find_state.dart';

class ChatMessageBubble extends StatelessWidget {
  final ChatMessage message;
  final String? highlightQuery;
  final bool caseSensitive;
  final bool isRegex;
  final List<FindMatch> highlightRanges;
  final int currentRangeAbsIndex;
  final List<int> rangeAbsIndices;
  final String? regexError;

  const ChatMessageBubble({
    super.key,
    required this.message,
    this.highlightQuery,
    this.caseSensitive = false,
    this.isRegex = false,
    this.highlightRanges = const [],
    this.currentRangeAbsIndex = -1,
    this.rangeAbsIndices = const [],
    this.regexError,
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
            highlightQuery == null
                ? Text(
                    message.content,
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.redAlert,
                    ),
                  )
                : _highlightedText(
                    content: message.content,
                    baseStyle: CyberpunkTypography.bodyMedium
                        .copyWith(color: CyberpunkColors.redAlert),
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
            // Highlight overlay: when a find query is active we render a
            // RichText-on-plaintext below the markdown rendering so matches
            // stay visible. MarkdownBody keeps selectable text intact, but
            // highlighting inside MarkdownBody requires a custom builder
            // delegate which is out of scope for the MVP.
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
            if (highlightQuery != null && highlightRanges.isNotEmpty) ...[
              const SizedBox(height: 4),
              _highlightedText(
                content: message.content,
                baseStyle: CyberpunkTypography.bodySmall.copyWith(
                  color: isUser ? CyberpunkColors.orangeGlow : null,
                ),
                maxLines: 3,
              ),
            ],
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

  /// Builds a Text.rich with matched spans highlighted (orange for current
  /// match, semi-transparent orange for other matches).
  Widget _highlightedText({
    required String content,
    required TextStyle baseStyle,
    int? maxLines,
  }) {
    if (highlightRanges.isEmpty) {
      return Text(
        content,
        style: baseStyle,
        maxLines: maxLines,
        overflow: maxLines == null ? null : TextOverflow.ellipsis,
      );
    }

    final spans = <TextSpan>[];
    var prev = 0;
    for (var i = 0; i < highlightRanges.length; i++) {
      final range = highlightRanges[i];
      final absIdx = rangeAbsIndices.isEmpty ? i : rangeAbsIndices[i];
      final start = range.start.clamp(0, content.length);
      final end = range.end.clamp(start, content.length);
      if (start > prev) {
        spans.add(TextSpan(text: content.substring(prev, start), style: baseStyle));
      }
      final isCurrent = absIdx == currentRangeAbsIndex;
      spans.add(TextSpan(
        text: content.substring(start, end),
        style: baseStyle.copyWith(
          background: Paint()..color = isCurrent
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
          color: isCurrent ? CyberpunkColors.black : null,
        ),
      ));
      prev = end;
    }
    if (prev < content.length) {
      spans.add(TextSpan(text: content.substring(prev), style: baseStyle));
    }
    return Text.rich(
      TextSpan(children: spans, style: baseStyle),
      maxLines: maxLines,
      overflow: maxLines == null ? null : TextOverflow.ellipsis,
    );
  }

  String _formatTime(DateTime time) {
    final hour = time.hour.toString().padLeft(2, '0');
    final minute = time.minute.toString().padLeft(2, '0');
    return '$hour:$minute';
  }
}
