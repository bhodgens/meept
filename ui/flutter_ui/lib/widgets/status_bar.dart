import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../providers/providers.dart';
import '../providers/verbosity_provider.dart';
import '../providers/status_message_provider.dart';
import '../providers/project_provider.dart';

/// Single-line status bar pinned at the bottom of the HomeScreen.
/// Mirrors TUI renderStatusBar (internal/tui/app.go:2236-2289).
class StatusBar extends ConsumerWidget {
  final int selectedTabIndex;
  const StatusBar({super.key, required this.selectedTabIndex});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Transient status messages (e.g. "session archived") take precedence
    // over the multi-part bar and hide all other parts.
    final transient = ref.watch(statusMessageProvider);
    if (transient != null) {
      return _bar(child: Text(transient, style: _mutedStyle));
    }

    final parts = <String>[];
    parts.add(_connectionPart(ref));
    final sessionPart = _sessionPart(ref);
    if (sessionPart.isNotEmpty) parts.add(sessionPart);
    parts.add(_keybindHint(selectedTabIndex));
    final projectPart = _projectPart(ref);
    if (projectPart != null) parts.add(projectPart);
    parts.add('verbosity: ${VerbosityLevel.name(ref.watch(verbosityProvider))}');

    return _bar(
      child: Text(
        parts.join(' · '),
        style: _mutedStyle,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }

  Widget _bar({required Widget child}) => Container(
        height: 22,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.7),
          border: const Border(
              top: BorderSide(color: CyberpunkColors.midGray, width: 1)),
        ),
        alignment: Alignment.centerLeft,
        child: child,
      );

  TextStyle get _mutedStyle => CyberpunkTypography.bodySmall.copyWith(
        color: CyberpunkColors.midGray,
        fontFamily: 'SourceCodePro',
        fontSize: 10,
      );

  String _connectionPart(WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final status = ref.watch(connectionStatusProvider);
    final dot = connected ? '●' : '○';
    return '$dot $status';
  }

  String _sessionPart(WidgetRef ref) {
    final session = ref.watch(activeSessionProvider);
    final name = session?.title;
    if (name == null || name.isEmpty || name == 'default') return '';
    return 'session: ${name.toLowerCase()}';
  }

  String _keybindHint(int tabIndex) {
    switch (tabIndex) {
      case 0:
        // Chat tab — focus hint, slash command, find, verbosity cycle.
        return '^k focus · / cmd · ^f find · ^v verbosity';
      case 1:
        // Sessions tab — double-click to open, backspace to archive.
        return 'dbl-click open · ⌫ archive';
      default:
        return 'j/k navigate · enter select';
    }
  }

  String? _projectPart(WidgetRef ref) {
    final p = ref.watch(currentProjectProvider);
    if (!p.isActive) return null;
    final name = p.name.length > 16 ? '${p.name.substring(0, 13)}...' : p.name;
    if (p.mode == 'git') {
      final branch = p.branch.isNotEmpty ? ' ${p.branch}' : '';
      final dirty = p.dirty ? '*' : '';
      return '[$name$branch$dirty]';
    }
    return '[local:$name]';
  }
}
