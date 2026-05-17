import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

class ToolsPanel extends StatelessWidget {
  final bool isExpanded;

  const ToolsPanel({super.key, this.isExpanded = true});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: isExpanded ? 300 : 50,
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
            child: ListView(
              children: [
                _buildToolItem(Icons.memory, 'Memory', '128 entries'),
                _buildToolItem(Icons.folder, 'Files', '24 files'),
                _buildToolItem(Icons.terminal, 'Terminal', 'Active'),
                _buildToolItem(Icons.calendar_today, 'Calendar', '3 events today'),
                _buildToolItem(Icons.insights, 'Metrics', 'Live'),
                _buildToolItem(Icons.settings, 'Settings', ''),
              ],
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
          Icon(Icons.apps, color: CyberpunkColors.orangePrimary, size: 20),
          if (isExpanded) ...[
            const SizedBox(width: 8),
            Text(
              'TOOLS',
              style: CyberpunkTypography.label,
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildToolItem(IconData icon, String label, String status) {
    return ListTile(
      leading: Icon(icon, color: CyberpunkColors.orangeBright, size: 20),
      title: isExpanded
          ? Text(
              label,
              style: CyberpunkTypography.bodyMedium,
            )
          : null,
      subtitle: isExpanded && status.isNotEmpty
          ? Text(
              status,
              style: CyberpunkTypography.bodySmall.copyWith(fontSize: 10),
            )
          : null,
      onTap: () {
        // TODO: Handle tool selection
      },
    );
  }
}
