import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class ChatInput extends StatefulWidget {
  final String sessionId;

  const ChatInput({super.key, required this.sessionId});

  @override
  State<ChatInput> createState() => _ChatInputState();
}

class _ChatInputState extends State<ChatInput> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          top: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          // Agent selector
          _buildAgentSelector(),
          const SizedBox(width: 8),
          // Text input
          Expanded(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              decoration: BoxDecoration(
                color: CyberpunkColors.black,
                border: Border.all(color: CyberpunkColors.midGray, width: 1),
                borderRadius: BorderRadius.circular(4),
              ),
              child: TextField(
                controller: _controller,
                style: CyberpunkTypography.bodyMedium,
                decoration: const InputDecoration(
                  hintText: 'enter command...',
                  hintStyle: TextStyle(color: CyberpunkColors.lightGray),
                  border: InputBorder.none,
                ),
                maxLines: null,
                textCapitalization: TextCapitalization.sentences,
              ),
            ),
          ),
          const SizedBox(width: 8),
          // Send button
          _buildSendButton(),
        ],
      ),
    );
  }

  Widget _buildAgentSelector() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: CyberpunkColors.black,
        border: Border.all(color: CyberpunkColors.orangePrimary, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.smart_toy,
            size: 16,
            color: CyberpunkColors.orangePrimary,
          ),
          const SizedBox(width: 6),
          Text(
            'CODER',
            style: CyberpunkTypography.label.copyWith(fontSize: 10),
          ),
          const Icon(Icons.expand_more, size: 16, color: CyberpunkColors.orangePrimary),
        ],
      ),
    );
  }

  Widget _buildSendButton() {
    return GestureDetector(
      onTap: _handleSend,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.orangePrimary,
          borderRadius: BorderRadius.circular(4),
        ),
        child: const Icon(
          Icons.send,
          color: CyberpunkColors.black,
          size: 20,
        ),
      ),
    );
  }

  void _handleSend() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    // TODO: Send message via API
    _controller.clear();
  }
}
