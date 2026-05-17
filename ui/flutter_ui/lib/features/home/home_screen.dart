import 'package:flutter/material.dart';
import '../../theme/cyberpunk_theme.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import 'navigation_rail.dart';
import 'tab_content.dart';

enum HomeTab { agents, tasks, sessions }

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  HomeTab _selectedTab = HomeTab.sessions;
  bool _isSidebarCollapsed = false;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      body: Container(
        decoration: BoxDecoration(
          gradient: CyberpunkEffects.angularGradient,
        ),
        child: SafeArea(
          child: Column(
            children: [
              // Top header bar
              _buildHeaderBar(),
              // Main content area
              Expanded(
                child: Row(
                  children: [
                    // Left pane: Navigation tabs
                    _buildNavigationPane(),
                    // Divider
                    _buildVerticalDivider(),
                    // Center/Right: Tab content
                    Expanded(
                      child: _buildTabContent(),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHeaderBar() {
    return Container(
      height: 60,
      margin: const EdgeInsets.all(8),
      padding: const EdgeInsets.symmetric(horizontal: 16),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border.all(
          color: CyberpunkColors.orangePrimary.withOpacity(0.3),
          width: 1,
        ),
        boxShadow: CyberpunkEffects.borderGlow(),
      ),
      clipBehavior: Clip.antiAlias,
      child: Row(
        children: [
          // Logo/title
          Text(
            'MEEPT',
            style: Theme.of(context).textTheme.displayMedium?.copyWith(
                  fontSize: 28,
                  shadows: [
                    Shadow(
                      color: CyberpunkColors.orangeGlow,
                      blurRadius: 15,
                    ),
                  ],
                ),
          ),
          const SizedBox(width: 32),
          // Connection status
          _buildConnectionIndicator(),
          const Spacer(),
          // Metrics summary
          _buildMetricsSummary(),
          const SizedBox(width: 16),
          // Settings button
          IconButton(
            icon: const Icon(Icons.settings,
                color: CyberpunkColors.orangePrimary),
            onPressed: () {
              // Open settings
            },
          ),
        ],
      ),
    );
  }

  Widget _buildConnectionIndicator() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.midGray,
        border: Border.all(color: CyberpunkColors.greenSuccess, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: const BoxDecoration(
              color: CyberpunkColors.greenSuccess,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: 8),
          Text(
            'CONNECTED',
            style: Theme.of(context).textTheme.labelLarge?.copyWith(
                  color: CyberpunkColors.greenSuccess,
                  fontSize: 10,
                ),
          ),
        ],
      ),
    );
  }

  Widget _buildMetricsSummary() {
    return Row(
      children: [
        _buildMetricChip('AGENTS', '8', CyberpunkColors.blueInfo),
        const SizedBox(width: 8),
        _buildMetricChip('TASKS', '12', CyberpunkColors.orangePrimary),
        const SizedBox(width: 8),
        _buildMetricChip('TOKENS', '2.4K', Colors.purple),
      ],
    );
  }

  Widget _buildMetricChip(String label, String value, Color color) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withOpacity(0.1),
        border: Border.all(color: color, width: 1),
        borderRadius: BorderRadius.circular(2),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            value,
            style: Theme.of(context).textTheme.labelLarge?.copyWith(
                  color: color,
                  fontSize: 14,
                  fontWeight: FontWeight.bold,
                ),
          ),
          Text(
            label,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(fontSize: 8),
          ),
        ],
      ),
    );
  }

  Widget _buildNavigationPane() {
    return Container(
      width: _isSidebarCollapsed ? 60 : 280,
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withOpacity(0.8),
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withOpacity(0.3),
            width: 1,
          ),
        ),
      ),
      child: CyberpunkNavigationRail(
        selectedTab: _selectedTab,
        onTabSelected: (tab) => setState(() => _selectedTab = tab),
        isCollapsed: _isSidebarCollapsed,
        onCollapseToggle: () =>
            setState(() => _isSidebarCollapsed = !_isSidebarCollapsed),
      ),
    );
  }

  Widget _buildVerticalDivider() {
    return Container(
      width: 1,
      color: CyberpunkColors.orangeDark.withOpacity(0.3),
      height: double.infinity,
    );
  }

  Widget _buildTabContent() {
    return CyberpunkTabContent(
      selectedTab: _selectedTab,
      isSidebarCollapsed: _isSidebarCollapsed,
    );
  }
}
