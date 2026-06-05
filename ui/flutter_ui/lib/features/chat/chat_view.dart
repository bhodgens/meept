import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import 'chat_message_list.dart';
import 'chat_input.dart';

/// Chat view - main chat pane with message list and input.
/// Header (session name) is now in the shared toolbar at home_screen level.
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
