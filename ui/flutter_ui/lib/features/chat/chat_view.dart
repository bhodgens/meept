import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../widgets/destructive_confirmation_dialog.dart';
import '../../widgets/thread_selector.dart';
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
  void initState() {
    super.initState();
    // Listen for destructive-tool confirmation requests surfaced by the
    // ChatNotifier.  When one arrives, show DestructiveConfirmationDialog
    // and forward the user's decision back via resolveConfirmation.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _listenForConfirmation();
    });
  }

  void _listenForConfirmation() {
    ref.listen<ChatState?>(chatProvider, (previous, next) {
      final pending = next?.pendingConfirmation;
      if (pending == null) return;
      if (!mounted) return;
      // Coalesce: if a dialog is already visible, skip duplicate events for
      // the same action until the user resolves it.
      showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (_) => DestructiveConfirmationDialog(response: pending),
      ).then((confirmed) {
        ref.read(chatProvider.notifier).resolveConfirmation(confirmed ?? false);
      });
    });
  }

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
            child: Row(
              children: [
                Expanded(
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
                // Thread selector — context isolation per topic
                ThreadSelector(sessionId: widget.sessionId),
              ],
            ),
          ),
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
