import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'chat_message_list.dart';
import 'chat_input.dart';

/// Chat view - main chat pane with orange header bar, message list, and input
class ChatView extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatView({super.key, required this.sessionId});

  @override
  ConsumerState<ChatView> createState() => _ChatViewState();
}

class _ChatViewState extends ConsumerState<ChatView> {
  @override
  Widget build(BuildContext context) {
    final session = ref.watch(activeSessionProvider);

    // Build header text matching TUI logic:
    // - Both name and description: SessionName │ Description...
    // - Name only (non-default): session-name
    // - Description only: description text...
    // - Nothing: meept
    String headerText;
    final name = session?.title;
    final description = session?.description;

    if (name != null &&
        name.isNotEmpty &&
        name != 'default' &&
        description != null &&
        description.isNotEmpty) {
      final truncated = description.length > 60
          ? '${description.substring(0, 57)}...'
          : description;
      headerText = '$name \u2502 $truncated';
    } else if (name != null && name.isNotEmpty && name != 'default') {
      headerText = name;
    } else if (description != null && description.isNotEmpty) {
      final truncated = description.length > 80
          ? '${description.substring(0, 77)}...'
          : description;
      headerText = truncated;
    } else {
      headerText = 'meept';
    }

    return Container(
      color: CyberpunkColors.black,
      child: Column(
        children: [
          // Orange header bar
          Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            color: const Color(0xFFF97316),
            child: Text(
              headerText.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.black,
                fontWeight: FontWeight.bold,
                fontFamily: 'SourceCodePro',
                fontSize: 13,
              ),
              overflow: TextOverflow.ellipsis,
              maxLines: 1,
            ),
          ),
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
}
