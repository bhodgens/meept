import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'home_screen.dart';

class CyberpunkNavigationRail extends StatelessWidget {
  final HomeTab selectedTab;
  final Function(HomeTab) onTabSelected;
  final bool isCollapsed;
  final VoidCallback onCollapseToggle;

  const CyberpunkNavigationRail({
    super.key,
    required this.selectedTab,
    required this.onTabSelected,
    required this.isCollapsed,
    required this.onCollapseToggle,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Tab buttons
        _buildTabButton(HomeTab.agents, 'agents', Icons.rocket),
        _buildTabButton(HomeTab.tasks, 'tasks', Icons.list_alt),
        _buildTabButton(HomeTab.sessions, 'sessions', Icons.folder_shared),
        const Spacer(),
        // Collapse toggle
        IconButton(
          icon: Icon(
            isCollapsed ? Icons.chevron_right : Icons.chevron_left,
            color: CyberpunkColors.orangePrimary,
          ),
          onPressed: onCollapseToggle,
          tooltip: isCollapsed ? 'expand' : 'collapse',
        ),
        const SizedBox(height: 8),
      ],
    );
  }

  Widget _buildTabButton(HomeTab tab, String label, IconData icon) {
    final isSelected = selectedTab == tab;

    return Container(
      margin: const EdgeInsets.symmetric(vertical: 4, horizontal: 8),
      decoration: BoxDecoration(
        color: isSelected
            ? CyberpunkColors.orangePrimary.withOpacity(0.2)
            : Colors.transparent,
        border: Border(
          left: BorderSide(
            color: isSelected ? CyberpunkColors.orangePrimary : Colors.transparent,
            width: 3,
          ),
        ),
        borderRadius: BorderRadius.circular(4),
      ),
      child: ListTile(
        leading: Icon(
          icon,
          color: isSelected
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.lightGray,
        ),
        title: isCollapsed
            ? null
            : Text(
                label,
                style: TextStyle(
                  fontFamily: 'JetBrainsMono',
                  color: isSelected
                      ? CyberpunkColors.orangePrimary
                      : CyberpunkColors.lightGray,
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                  letterSpacing: 1,
                ),
              ),
        onTap: () => onTabSelected(tab),
      ),
    );
  }
}
