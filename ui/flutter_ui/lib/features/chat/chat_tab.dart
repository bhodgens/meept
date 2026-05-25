import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'chat_view.dart';
import '../sidebar/tools_panel.dart';
import '../memory/memory_panel.dart';
import '../settings/settings_panel.dart';
import '../files/files_panel.dart';

/// Chat tab - 3-pane layout with message list, main view, and collapsible sidebar
class ChatTab extends StatefulWidget {
  final String sessionId;

  const ChatTab({super.key, required this.sessionId});

  @override
  State<ChatTab> createState() => _ChatTabState();
}

class _ChatTabState extends State<ChatTab> {
  bool _isSidebarCollapsed = false;
  String _activeTool = '';

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          // Main chat pane (transcript + input)
          Expanded(
            flex: _activeTool.isEmpty ? 3 : 2,
            child: Container(
              decoration: BoxDecoration(
                border: Border(
                  right: BorderSide(
                    color: CyberpunkColors.orangeDark.withValues(alpha: 0.3),
                    width: 1,
                  ),
                ),
              ),
              child: _buildMainContent(),
            ),
          ),
          // Tool detail pane (when a tool is selected)
          if (_activeTool.isNotEmpty && _activeTool != 'memory' && _activeTool != 'settings')
            Expanded(
              flex: 1,
              child: _buildToolDetail(),
            ),
          // Right sidebar (tools panel) - collapsible
          if (!_isSidebarCollapsed && _activeTool.isEmpty)
            ToolsPanel(
              isExpanded: !_isSidebarCollapsed,
              onCollapseToggle: () =>
                  setState(() => _isSidebarCollapsed = !_isSidebarCollapsed),
            ),
          if (!_isSidebarCollapsed && _activeTool.isNotEmpty)
            ToolsPanel(
              isExpanded: !_isSidebarCollapsed,
              onCollapseToggle: () =>
                  setState(() => _isSidebarCollapsed = !_isSidebarCollapsed),
              onToolSelected: (route) => setState(() => _activeTool = route),
            ),
        ],
      ),
    );
  }

  Widget _buildMainContent() {
    if (_activeTool.isNotEmpty) {
      return _buildToolView();
    }
    return ChatView(sessionId: widget.sessionId);
  }

  Widget _buildToolView() {
    // Fully implemented panels
    if (_activeTool == 'memory') {
      return const MemoryPanel();
    }
    if (_activeTool == 'settings') {
      return const SettingsPanel();
    }
    if (_activeTool == 'files') {
      return const FilesPanel();
    }

    // Other tools show placeholder
    return Container(
      color: CyberpunkColors.darkGray,
      child: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              _toolIconFor(_activeTool),
              size: 48,
              color: CyberpunkColors.orangeBright,
            ),
            const SizedBox(height: 16),
            Text(
              '${_activeTool.toLowerCase()} view',
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'coming soon',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.orangeDark,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildToolDetail() {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Center(
        child: Text(
          'tool details: ${_activeTool.toLowerCase()}',
          style: CyberpunkTypography.bodySmall.copyWith(
            color: CyberpunkColors.orangeDark,
          ),
        ),
      ),
    );
  }

  IconData _toolIconFor(String route) {
    final item = _tools.firstWhere(
      (t) => t.route == route,
      orElse: () => const ToolItem(icon: Icons.help, label: '', status: '', route: ''),
    );
    return item.icon;
  }

  final List<ToolItem> _tools = [
    const ToolItem(icon: Icons.memory, label: 'memory', status: 'ready', route: 'memory'),
    const ToolItem(icon: Icons.folder, label: 'files', status: 'beta', route: 'files'),
    const ToolItem(icon: Icons.terminal, label: 'terminal', status: 'coming soon', route: 'terminal'),
    const ToolItem(icon: Icons.calendar_today, label: 'calendar', status: 'coming soon', route: 'calendar'),
    const ToolItem(icon: Icons.insights, label: 'metrics', status: 'live', route: 'metrics'),
    const ToolItem(icon: Icons.settings, label: 'settings', status: 'ready', route: 'settings'),
  ];
}
