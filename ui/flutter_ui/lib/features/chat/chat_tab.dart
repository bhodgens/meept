import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../providers/providers.dart';
import 'chat_view.dart';
import '../memory/memory_panel.dart';
import '../settings/settings_panel.dart';
import '../files/files_panel.dart';
import '../calendar/calendar_panel.dart';
import '../metrics/metrics_panel.dart';
import '../terminal/terminal_panel.dart';
import '../skills/skill_panel.dart';
import '../projects/branches_panel.dart';
import '../search/search_panel.dart';

/// Chat tab - full-width layout with main chat area only.
/// Tools open in the main content area (replaces chat view).
class ChatTab extends ConsumerStatefulWidget {
  final String sessionId;

  const ChatTab({super.key, required this.sessionId});

  @override
  ConsumerState<ChatTab> createState() => _ChatTabState();
}

class _ChatTabState extends ConsumerState<ChatTab> {
  @override
  Widget build(BuildContext context) {
    final activeTool = ref.watch(activeToolProvider);

    return Container(
      color: CyberpunkColors.black,
      child: activeTool.isNotEmpty ? _buildToolView(activeTool) : ChatView(sessionId: widget.sessionId),
    );
  }

  Widget _buildToolView(String activeTool) {
    switch (activeTool) {
      case 'memory':
        return const MemoryPanel();
      case 'settings':
        return const SettingsPanel();
      case 'files':
        return const FilesPanel();
      case 'calendar':
        return const CalendarPanel();
      case 'metrics':
        return const MetricsPanel();
      case 'terminal':
        return const TerminalPanel();
      case 'skills':
        return const SkillPanel();
      case 'branches':
        return const BranchesPanel();
      case 'search':
        return const SearchPanel();
    }

    // Unknown tool: if it looks like a skill slug, route to skill panel
    return SkillPanel(initialSlug: activeTool);
  }
}
