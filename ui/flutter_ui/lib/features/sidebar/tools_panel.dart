import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Tool item data class
class ToolItem {
  final IconData icon;
  final String label;
  final String status;
  final String route;

  const ToolItem({
    required this.icon,
    required this.label,
    required this.status,
    required this.route,
  });
}

class ToolsPanel extends ConsumerStatefulWidget {
  final bool isExpanded;
  final VoidCallback? onCollapseToggle;
  final ValueChanged<String>? onToolSelected;

  const ToolsPanel({
    super.key,
    this.isExpanded = true,
    this.onCollapseToggle,
    this.onToolSelected,
  });

  @override
  ConsumerState<ToolsPanel> createState() => _ToolsPanelState();
}

class _ToolsPanelState extends ConsumerState<ToolsPanel> {
  final List<ToolItem> _tools = [
    const ToolItem(icon: Icons.memory, label: 'memory', status: '128 entries', route: 'memory'),
    const ToolItem(icon: Icons.folder, label: 'files', status: '24 files', route: 'files'),
    const ToolItem(icon: Icons.terminal, label: 'terminal', status: 'active', route: 'terminal'),
    const ToolItem(icon: Icons.calendar_today, label: 'calendar', status: '3 today', route: 'calendar'),
    const ToolItem(icon: Icons.insights, label: 'metrics', status: 'live', route: 'metrics'),
    const ToolItem(icon: Icons.settings, label: 'settings', status: '', route: 'settings'),
  ];

  @override
  Widget build(BuildContext context) {
    return Container(
      width: widget.isExpanded ? 300 : 50,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          left: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Column(
        children: [
          _buildHeader(),
          Expanded(
            child: ListView.builder(
              itemCount: _tools.length,
              itemBuilder: (context, index) {
                return _buildToolItem(_tools[index]);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          IconButton(
            icon: Icon(
              widget.isExpanded ? Icons.chevron_left : Icons.chevron_right,
              color: CyberpunkColors.orangePrimary,
              size: 18,
            ),
            onPressed: widget.onCollapseToggle,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
          ),
          const SizedBox(width: 4),
          const Icon(Icons.apps, color: CyberpunkColors.orangePrimary, size: 20),
          if (widget.isExpanded) ...[
            const SizedBox(width: 8),
            Text(
              'tools',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildToolItem(ToolItem tool) {
    return ListTile(
      leading: Icon(tool.icon, color: CyberpunkColors.orangeBright, size: 20),
      title: widget.isExpanded
          ? Text(
              tool.label.toLowerCase(),
              style: CyberpunkTypography.bodyMedium,
            )
          : null,
      subtitle: widget.isExpanded && tool.status.isNotEmpty
          ? Text(
              tool.status.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(fontSize: 10),
            )
          : null,
      onTap: () {
        if (widget.onToolSelected != null) {
          widget.onToolSelected!(tool.route);
        } else {
          debugPrint('tool selected: ${tool.route}');
        }
      },
    );
  }
}
