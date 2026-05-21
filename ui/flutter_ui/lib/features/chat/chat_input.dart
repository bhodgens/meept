import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/chat_provider.dart';

/// Chat input widget - fixed height bottom pane for text entry
class ChatInput extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatInput({super.key, required this.sessionId});

  @override
  State<ChatInput> createState() => _ChatInputState();
}

class _ChatInputState extends ConsumerState<ChatInput> {
  final _controller = TextEditingController();
  String _selectedAgent = 'coder';

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _handleSend() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;

    final chatNotifier = ref.read(chatProvider.notifier);
    chatNotifier.sendMessage(
      message: text,
      sessionId: widget.sessionId,
      agentId: _selectedAgent,
    );

    _controller.clear();
  }

  @override
  Widget build(BuildContext context) {
    final chatState = ref.watch(chatProvider);

    return Container(
      height: 80,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(color: CyberpunkColors.orangePrimary, width: 1),
        ),
      ),
      child: Row(
        children: [
          _buildAgentSelector(),
          const SizedBox(width: 8),
          Expanded(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              decoration: BoxDecoration(
                color: CyberpunkColors.black,
                border: Border.all(color: CyberpunkColors.midGray, width: 1),
                borderRadius: BorderRadius.circular(4),
              ),
              child: TextField(
                controller: _controller,
                enabled: !chatState.isLoading,
                style: CyberpunkTypography.bodyMedium.copyWith(
                  color: CyberpunkColors.greenSuccess,
                  fontFamily: 'SourceCodePro',
                ),
                cursorColor: CyberpunkColors.orangePrimary,
                decoration: InputDecoration(
                  hintText: chatState.isLoading ? 'sending...' : 'enter command...',
                  hintStyle: CyberpunkTypography.bodySmall,
                  border: InputBorder.none,
                  contentPadding: EdgeInsets.zero,
                ),
                maxLines: 3,
                minLines: 1,
                textCapitalization: TextCapitalization.sentences,
                onSubmitted: (value) => _handleSend(),
              ),
            ),
          ),
          const SizedBox(width: 8),
          _buildSendButton(),
        ],
      ),
    );
  }

  Widget _buildAgentSelector() {
    return GestureDetector(
      onTap: () {
        // TODO: Show agent selection dropdown
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          border: Border.all(color: CyberpunkColors.orangePrimary, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.smart_toy,
              size: 16,
              color: CyberpunkColors.orangePrimary,
            ),
            const SizedBox(width: 6),
            Text(
              _selectedAgent,
              style: CyberpunkTypography.label.copyWith(
                fontSize: 10,
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Icon(Icons.expand_more, size: 14, color: CyberpunkColors.orangePrimary),
          ],
        ),
      ),
    );
  }

  Widget _buildSendButton() {
    return GestureDetector(
      onTap: chatState.isLoading ? null : _handleSend,
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: chatState.isLoading
              ? CyberpunkColors.midGray
              : CyberpunkColors.orangePrimary,
          borderRadius: BorderRadius.circular(4),
        ),
        child: chatState.isLoading
            ? SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  valueColor: AlwaysStoppedAnimation<Color>(CyberpunkColors.black),
                ),
              )
            : const Icon(
                Icons.send,
                color: CyberpunkColors.black,
                size: 18,
              ),
      ),
    );
  }
}
