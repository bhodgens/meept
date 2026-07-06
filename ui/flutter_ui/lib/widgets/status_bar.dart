import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';
import '../providers/providers.dart';
import '../providers/verbosity_provider.dart';
import '../providers/status_message_provider.dart';

/// Single-line status bar pinned at the bottom of the HomeScreen.
/// Mirrors TUI renderStatusBar (internal/tui/app.go:2236-2289).
///
/// Styling:
/// - Connection status: green (connected), red (disconnected), orange (connecting)
/// - Session: orange
/// - Keybind hints: very light gray (almost white)
/// - Project path: orange
/// - Verbosity: very light gray
class StatusBar extends ConsumerWidget {
  final int selectedTabIndex;
  const StatusBar({super.key, required this.selectedTabIndex});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Transient status messages (e.g. "session archived") take precedence
    // over the multi-part bar and hide all other parts.
    final transient = ref.watch(statusMessageProvider);
    if (transient != null) {
      return _bar(child: Text(transient, style: _baseStyle));
    }

    // Build rich text with contextual colors for each part
    final spans = <InlineSpan>[];

    // Connection part (contextual color)
    spans.add(_connectionSpan(ref));

    // Session part (orange)
    final sessionPart = _sessionPart(ref);
    if (sessionPart.isNotEmpty) {
      spans.add(_separator());
      spans.add(TextSpan(text: sessionPart, style: _orangeStyle));
    }

    // Keybind hint (very light gray)
    spans.add(_separator());
    spans.add(TextSpan(text: _keybindHint(selectedTabIndex), style: _lightStyle));

    // Project part (orange)
    final projectPart = _projectPart(ref);
    if (projectPart != null) {
      spans.add(_separator());
      spans.add(TextSpan(text: projectPart, style: _orangeStyle));
    }

    // Verbosity (very light gray)
    spans.add(_separator());
    spans.add(TextSpan(
      text: 'verbosity: ${VerbosityLevel.name(ref.watch(verbosityProvider))}',
      style: _lightStyle,
    ));

    return _bar(
      child: RichText(
        text: TextSpan(children: spans),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }

  Widget _bar({required Widget child}) => Container(
        height: 28, // Increased from 22 for better readability
        padding: const EdgeInsets.symmetric(horizontal: 12),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.7),
          border: const Border(
              top: BorderSide(color: CyberpunkColors.midGray, width: 1)),
        ),
        alignment: Alignment.centerLeft,
        child: child,
      );

  // Base style - increased font size from 10 to 12
  TextStyle get _baseStyle => CyberpunkTypography.bodySmall.copyWith(
        color: CyberpunkColors.veryLightGray,
        fontFamily: 'SourceCodePro',
        fontSize: 12,
      );

  // Orange style for session and project
  TextStyle get _orangeStyle => _baseStyle.copyWith(
        color: CyberpunkColors.orangePrimary,
      );

  // Very light gray for keybind hints and verbosity
  TextStyle get _lightStyle => _baseStyle.copyWith(
        color: CyberpunkColors.veryLightGray,
      );

  InlineSpan _separator() => const TextSpan(text: ' · ', style: TextStyle(color: CyberpunkColors.midGray));

  InlineSpan _connectionSpan(WidgetRef ref) {
    final connected = ref.watch(connectionStateProvider);
    final isConnecting = ref.watch(isConnectingProvider);
    final statusText = ref.watch(connectionStatusProvider);

    // Contextual color: green (connected), red (disconnected), orange (connecting)
    final color = isConnecting
        ? CyberpunkColors.orangePrimary
        : connected
            ? CyberpunkColors.greenSuccess
            : CyberpunkColors.redAlert;

    final dot = connected ? '●' : '○';
    return TextSpan(
      text: '$dot $statusText',
      style: _baseStyle.copyWith(color: color),
    );
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
        // Sessions tab — double-click to open, archive icon to archive.
        return 'dbl-click open · tap archive icon';
      default:
        return 'j/k navigate · enter select';
    }
  }

  String? _projectPart(WidgetRef ref) {
    final p = ref.watch(currentProjectProvider);
    if (!p.isActive) return '[no project]';
    // Show localPath if available, otherwise fall back to name
    final pathToShow = p.localPath.isNotEmpty ? p.localPath : p.name;
    // Use grapheme-cluster-aware truncation for long paths
    final chars = pathToShow.characters;
    final displayPath = chars.length > 30 ? '...${chars.takeLast(27).toString()}' : pathToShow;
    if (p.mode == 'git') {
      final branch = p.branch.isNotEmpty ? ' ${p.branch}' : '';
      final dirty = p.dirty ? '*' : '';
      return '[$displayPath$branch$dirty]';
    }
    return '[local:$displayPath]';
  }
}
