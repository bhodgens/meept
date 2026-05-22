import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/api_models.dart';
import '../../providers/providers.dart';

/// Chat input widget - fixed height bottom pane for text entry
class ChatInput extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatInput({super.key, required this.sessionId});

  @override
  ConsumerState<ChatInput> createState() => _ChatInputState();
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
      sessionId: widget.sessionId,
      text: text,
    );

    _controller.clear();
  }

  @override
  Widget build(BuildContext context) {
    final chatState = ref.watch(chatProvider);

    return Container(
      height: 80,
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
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
    final agents = ref.watch(agentProvider);
    final activeAgent = ref.watch(activeAgentProvider);

    final selectedAgentId = activeAgent?.id ?? _selectedAgent;

    return PopupMenuButton<String>(
      onSelected: (String agentId) {
        final agent = agents.agents.firstWhere(
          (a) => a.id == agentId,
          orElse: () => Agent(
            id: agentId,
            name: agentId,
            description: '',
            prompt: '',
            enabled: true,
          ),
        );
        ref.read(activeAgentProvider.notifier).state = agent;
        setState(() {
          _selectedAgent = agentId;
        });
      },
      itemBuilder: (BuildContext context) {
        if (agents.isLoading) {
          return [
            const PopupMenuItem<String>(
              enabled: false,
              value: '__loading__',
              child: SizedBox(
                width: 120,
                child: LinearProgressIndicator(),
              ),
            ),
          ];
        }

        // Always include the fallback hardcoded agent
        final allAgents = <Agent>[
          Agent(
            id: _selectedAgent,
            name: _selectedAgent,
            description: '',
            prompt: '',
            enabled: true,
          ),
          ...agents.agents,
        ];

        return allAgents.map((Agent agent) {
          return PopupMenuItem<String>(
            value: agent.id,
            child: Row(
              children: [
                Icon(
                  _getAgentIcon(agent.id),
                  size: 16,
                  color: agent.id == selectedAgentId
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.greenSuccess,
                ),
                const SizedBox(width: 8),
                Text(
                  agent.name,
                  style: CyberpunkTypography.bodySmall.copyWith(
                    fontFamily: 'SourceCodePro',
                    color: agent.id == selectedAgentId
                        ? CyberpunkColors.orangePrimary
                        : null,
                  ),
                ),
              ],
            ),
          );
        }).toList();
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
            Icon(
              _getAgentIcon(selectedAgentId),
              size: 16,
              color: CyberpunkColors.orangePrimary,
            ),
            const SizedBox(width: 6),
            Text(
              activeAgent?.name ?? _selectedAgent,
              style: CyberpunkTypography.label.copyWith(
                fontSize: 10,
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Icon(
              Icons.expand_more,
              size: 14,
              color: CyberpunkColors.orangePrimary,
            ),
          ],
        ),
      ),
    );
  }

  IconData _getAgentIcon(String id) {
    final lower = id.toLowerCase();
    switch (lower) {
      case 'coder':
        return Icons.code;
      case 'debugger':
        return Icons.bug_report;
      case 'planner':
        return Icons.account_tree;
      case 'analyst':
        return Icons.analytics;
      case 'chat':
        return Icons.chat;
      case 'committer':
        return Icons.history;
      case 'scheduler':
        return Icons.schedule;
      default:
        return Icons.smart_toy;
    }
  }

  Widget _buildSendButton() {
    final chatState = ref.watch(chatProvider);
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
            ? const SizedBox(
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
