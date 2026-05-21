import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'chat_view.dart';
import '../sidebar/tools_panel.dart';

/// Chat tab - 3-pane layout with message list, main view, and collapsible sidebar
class ChatTab extends StatefulWidget {
  final String sessionId;

  const ChatTab({super.key, required this.sessionId});

  @override
  State<ChatTab> createState() => _ChatTabState();
}

class _ChatTabState extends State<ChatTab> {
  bool _isSidebarCollapsed = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          // Main chat pane (transcript + input)
          Expanded(
            flex: 3,
            child: Container(
              decoration: BoxDecoration(
                border: Border(
                  right: BorderSide(
                    color: CyberpunkColors.orangeDark.withOpacity(0.3),
                    width: 1,
                  ),
                ),
              ),
              child: ChatView(sessionId: widget.sessionId),
            ),
          ),
          // Right sidebar (tools panel) - collapsible
          if (!_isSidebarCollapsed)
            ToolsPanel(
              isExpanded: !_isSidebarCollapsed,
              onCollapseToggle: () =>
                  setState(() => _isSidebarCollapsed = !_isSidebarCollapsed),
            ),
        ],
      ),
    );
  }
}
