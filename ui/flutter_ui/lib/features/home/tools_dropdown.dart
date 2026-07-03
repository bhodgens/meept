import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Hamburger menu button for the top-left toolbar.
/// Shows known tools only (no skills section at bottom).
class HamburgerMenu extends StatelessWidget {
  final ValueChanged<String>? onToolSelected;

  const HamburgerMenu({super.key, this.onToolSelected});

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      tooltip: 'tools',
      // Remove animation by using 0ms duration convention
      // PopupMenuButton doesn't have explicit animation control,
      // but using immediate trigger via onSelected
      onSelected: (route) {
        onToolSelected?.call(route);
      },
      itemBuilder: (context) {
        final items = <PopupMenuEntry<String>>[
          const PopupMenuItem<String>(
            enabled: false,
            height: 32,
            child: Text(
              'tools',
              style: TextStyle(
                color: CyberpunkColors.orangePrimary,
                fontWeight: FontWeight.bold,
                fontSize: 12,
              ),
            ),
          ),
          const PopupMenuDivider(height: 1),
        ];

        // Hardcoded tool panels that have implementations
        final knownTools = {
          'memory': Icons.memory,
          'files': Icons.folder,
          'terminal': Icons.terminal,
          'calendar': Icons.calendar_today,
          'metrics': Icons.insights,
          'prompts': Icons.edit_note,
          'settings': Icons.settings,
        };

        for (final entry in knownTools.entries) {
          items.add(
            PopupMenuItem<String>(
              value: entry.key,
              height: 36,
              child: Row(
                children: [
                  Icon(entry.value, size: 16, color: CyberpunkColors.orangeBright),
                  const SizedBox(width: 8),
                  Text(
                    entry.key,
                    style: CyberpunkTypography.bodySmall,
                  ),
                ],
              ),
            ),
          );
        }

        return items;
      },
      child: Container(
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.3),
          border: Border.all(color: CyberpunkColors.midGray, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: const Icon(
          Icons.menu, // Hamburger icon
          size: 20,
          color: CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }
}
