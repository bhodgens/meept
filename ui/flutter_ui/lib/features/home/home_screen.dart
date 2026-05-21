import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/effects.dart';
import '../../widgets/tab_bar.dart';
import 'tab_content.dart';

/// Home tab enum - 4 tabs as specified
enum HomeTab { chat, sessions, tasks, agents }

/// Home screen - main app screen with top tab navigation
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  HomeTab _selectedTab = HomeTab.chat;

  final List<String> _tabLabels = ['chat', 'sessions', 'tasks', 'agents'];

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
              // Top tab bar
              OrangeVoidTabBar(
                tabs: _tabLabels,
                selectedIndex: _selectedTab.index,
                onTabSelected: (index) =>
                    setState(() => _selectedTab = HomeTab.values[index]),
              ),
              // Main content area
              Expanded(
                child: _buildTabContent(),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTabContent() {
    return TabContent(
      selectedTab: _selectedTab,
    );
  }
}
