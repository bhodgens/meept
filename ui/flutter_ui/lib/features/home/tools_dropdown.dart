import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../models/api_models.dart';

/// Tools dropdown button for the toolbar.
/// Fetches available skills/tools from the daemon API.
class ToolsDropdown extends ConsumerStatefulWidget {
  final ValueChanged<String>? onToolSelected;

  const ToolsDropdown({super.key, this.onToolSelected});

  @override
  ConsumerState<ToolsDropdown> createState() => _ToolsDropdownState();
}

class _ToolsDropdownState extends ConsumerState<ToolsDropdown> {
  List<Skill> _skills = [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _loadSkills();
  }

  Future<void> _loadSkills() async {
    try {
      final skills = await ref.read(apiClientProvider).getSkills();
      if (!mounted) return;
      setState(() {
        _skills = skills.where((s) => s.enabled).toList();
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      tooltip: 'tools',
      onSelected: (route) {
        widget.onToolSelected?.call(route);
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

        if (_skills.isNotEmpty) {
          items.add(const PopupMenuDivider(height: 1));
          for (final skill in _skills) {
            items.add(
              PopupMenuItem<String>(
                value: skill.slug,
                height: 36,
                child: Row(
                  children: [
                    Icon(
                      Icons.tune,
                      size: 16,
                      color: CyberpunkColors.orangeBright,
                    ),
                    const SizedBox(width: 8),
                    Text(
                      skill.slug.toLowerCase(),
                      style: CyberpunkTypography.bodySmall,
                    ),
                  ],
                ),
              ),
            );
          }
        }

        return items;
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: CyberpunkColors.blackTransparent(0.3),
          border: Border.all(color: CyberpunkColors.midGray, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(
              Icons.build,
              size: 14,
              color: CyberpunkColors.orangePrimary,
            ),
            const SizedBox(width: 6),
            Text(
              'tools',
              style: CyberpunkTypography.label.copyWith(
                fontSize: 11,
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const Icon(
              Icons.arrow_drop_down,
              size: 14,
              color: CyberpunkColors.orangePrimary,
            ),
          ],
        ),
      ),
    );
  }
}
