import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import 'chat_view.dart';
import '../memory/memory_panel.dart';
import '../settings/settings_panel.dart';
import '../files/files_panel.dart';
import '../calendar/calendar_panel.dart';
import '../metrics/metrics_panel.dart';
import '../terminal/terminal_panel.dart';

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
    return Focus(
      autofocus: true,
      onKeyEvent: (node, event) {
        if (event is KeyDownEvent && event.logicalKey == LogicalKeyboardKey.escape) {
          ref.read(activeToolProvider.notifier).state = '';
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: Stack(
        children: [
          _buildToolContent(activeTool),
          // Close button in top-right corner
          Positioned(
            top: 8,
            right: 8,
            child: GestureDetector(
              onTap: () => ref.read(activeToolProvider.notifier).state = '',
              child: Container(
                padding: const EdgeInsets.all(6),
                decoration: BoxDecoration(
                  color: CyberpunkColors.blackTransparent(0.7),
                  border: Border.all(color: CyberpunkColors.midGray, width: 1),
                  borderRadius: BorderRadius.circular(4),
                ),
                child: const Icon(
                  Icons.close,
                  size: 16,
                  color: CyberpunkColors.orangePrimary,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildToolContent(String activeTool) {
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
    }

    // Unknown tool: placeholder
    return Container(
      color: CyberpunkColors.darkGray,
      child: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(
              Icons.tune,
              size: 48,
              color: CyberpunkColors.orangeBright,
            ),
            const SizedBox(height: 16),
            Text(
              '${activeTool.toLowerCase()} view',
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
}
