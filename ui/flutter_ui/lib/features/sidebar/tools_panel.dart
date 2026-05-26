import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../models/api_models.dart';
import '../../../providers/providers.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Maps skill slugs/icons to display labels and known Flutter icons
class ToolItem {
  final IconData icon;
  final String label;
  final String status;
  final String route;

  const ToolItem({
    required this.icon,
    required this.label,
    required this.status,
    required this.route,
  });
}

/// Icon mapping for well-known skill slugs
final _skillIconMap = <String, IconData>{
  'memory': Icons.memory,
  'episodic_memory': Icons.memory,
  'task_memory': Icons.assignment,
  'files': Icons.folder,
  'read_file': Icons.insert_drive_file,
  'write_file': Icons.save,
  'search_files': Icons.search,
  'code_search': Icons.code,
  'shell': Icons.terminal,
  'execute': Icons.play_arrow,
  'browser': Icons.language,
  'calendar': Icons.calendar_today,
  'metrics': Icons.insights,
  'settings': Icons.settings,
  'config': Icons.tune,
  'skills': Icons.extension,
};

/// Display label for a skill slug (human-readable)
String _slugToLabel(String slug) => slug
    .split('_')
    .map((w) => w.isEmpty ? '' : w[0].toUpperCase() + w.substring(1))
    .join(' ')
    .trim();

/// Icon for a skill slug, falling back to a generic tool icon
IconData _slugToIcon(String slug) =>
    _skillIconMap[slug] ?? Icons.tune;

/// Status string derived from a Skill model
String _skillToStatus(Skill skill) {
  final parts = <String>[];
  if (!skill.enabled) parts.add('disabled');
  if (skill.capabilities.isNotEmpty) {
    parts.add(skill.capabilities.join(', '));
  }
  if (skill.category.isNotEmpty) {
    parts.add(skill.category);
  }
  return parts.join(' · ');
}

/// Fallback hardcoded tool items used when the API is unavailable
List<ToolItem> _fallbackTools() => [
      const ToolItem(
          icon: Icons.memory,
          label: 'memory',
          status: 'service unavailable',
          route: 'memory'),
      const ToolItem(
          icon: Icons.extension,
          label: 'skills',
          status: 'service unavailable',
          route: 'skills'),
    ];

class ToolsPanel extends ConsumerStatefulWidget {
  final bool isExpanded;
  final VoidCallback? onCollapseToggle;
  final ValueChanged<String>? onToolSelected;

  const ToolsPanel({
    super.key,
    this.isExpanded = true,
    this.onCollapseToggle,
    this.onToolSelected,
  });

  @override
  ConsumerState<ToolsPanel> createState() => _ToolsPanelState();
}

class _ToolsPanelState extends ConsumerState<ToolsPanel> {
  List<ToolItem> _tools = [];

  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadTools();
  }

  Future<void> _loadTools() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final skills =
          await ref.read(apiClientProvider).getSkills();
      if (!mounted) return;
      setState(() {
        _tools = skills
            .where((s) => s.enabled)
            .map((s) => ToolItem(
                  icon: _slugToIcon(s.slug),
                  label: _slugToLabel(s.slug),
                  status: _skillToStatus(s),
                  route: s.slug,
                ))
            .toList();
        _loading = false;
        if (_tools.isEmpty) {
          _tools = _fallbackTools();
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _loading = false;
        _tools = _fallbackTools();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: widget.isExpanded ? 300 : 50,
      decoration: const BoxDecoration(
        color: CyberpunkColors.darkGray,
        border: Border(
          left: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null && widget.isExpanded)
            Container(
              padding:
                  const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              decoration: const BoxDecoration(
                color: Color(0x80FF3030),
                border: Border(
                  bottom: BorderSide(
                    color: Color(0x80FF3080),
                    width: 1,
                  ),
                ),
              ),
              child: Row(
                children: [
                  const Icon(Icons.error_outline,
                      color: Color(0xFFFF3030), size: 16),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      _error!,
                      style: const TextStyle(
                        color: Color(0xFFFF6060),
                        fontSize: 10,
                      ),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  GestureDetector(
                    onTap: _loadTools,
                    child: const Icon(Icons.refresh,
                        color: Color(0xFFFF6060), size: 16),
                  ),
                ],
              ),
            ),
          Expanded(
            child: _loading
                ? const Center(
                    child: SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        valueColor: AlwaysStoppedAnimation<Color>(
                            Color(0xFFFFAA00)),
                      ),
                    ),
                  )
                : ListView.builder(
                    itemCount: _tools.length,
                    itemBuilder: (context, index) {
                      return _buildToolItem(_tools[index]);
                    },
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          IconButton(
            icon: Icon(
              widget.isExpanded ? Icons.chevron_left : Icons.chevron_right,
              color: CyberpunkColors.orangePrimary,
              size: 18,
            ),
            onPressed: widget.onCollapseToggle,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
          ),
          const SizedBox(width: 4),
          const Icon(Icons.apps, color: CyberpunkColors.orangePrimary, size: 20),
          if (widget.isExpanded) ...[
            const SizedBox(width: 8),
            Text(
              'tools',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildToolItem(ToolItem tool) {
    return ListTile(
      leading: Icon(tool.icon, color: CyberpunkColors.orangeBright, size: 20),
      title: widget.isExpanded
          ? Text(
              tool.label.toLowerCase(),
              style: CyberpunkTypography.bodyMedium,
            )
          : null,
      subtitle: widget.isExpanded && tool.status.isNotEmpty
          ? Text(
              tool.status.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(fontSize: 10),
            )
          : null,
      onTap: () {
        if (widget.onToolSelected != null) {
          widget.onToolSelected!(tool.route);
        } else {
          debugPrint('tool selected: ${tool.route}');
        }
      },
    );
  }
}
