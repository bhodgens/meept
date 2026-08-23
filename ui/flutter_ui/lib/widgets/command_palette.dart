import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// One entry in the command palette. Mirrors the TUI modal.go:192-205 command
/// modal, adapted to Flutter surfaces.
class CommandPaletteItem {
  final String keybinding;
  final String label;
  final String description;
  const CommandPaletteItem({
    required this.keybinding,
    required this.label,
    required this.description,
  });
}

/// Keyboard-navigable command palette overlay. Renders the project's lowercase
/// UI convention; navigation mirrors TUI modal.go (j/k arrows + enter).
class CommandPalette extends StatefulWidget {
  final List<CommandPaletteItem> items;
  final void Function(CommandPaletteItem item) onSelected;

  const CommandPalette({
    super.key,
    required this.items,
    required this.onSelected,
  });

  /// Matches TUI modal.go:192-205, adapted to Flutter surfaces.
  /// Omitted TUI items: queue view, memory view, toggle sidebar (no Flutter route).
  static List<CommandPaletteItem> get defaultItems => const [
    CommandPaletteItem(
      keybinding: 'c',
      label: 'chat',
      description: 'switch to chat view',
    ),
    CommandPaletteItem(
      keybinding: 's',
      label: 'sessions',
      description: 'switch to sessions view',
    ),
    CommandPaletteItem(
      keybinding: 'p',
      label: 'plans',
      description: 'switch to plans view',
    ),
    CommandPaletteItem(
      keybinding: 't',
      label: 'tasks',
      description: 'switch to tasks view',
    ),
    CommandPaletteItem(
      keybinding: 'a',
      label: 'agents',
      description: 'switch to employees view',
    ),
    CommandPaletteItem(
      keybinding: 'f',
      label: 'find…',
      description: 'search sessions and tasks',
    ),
    CommandPaletteItem(
      keybinding: 'n',
      label: 'new session',
      description: 'create a new session',
    ),
    CommandPaletteItem(
      keybinding: 'e',
      label: 'edit description',
      description: 'edit session description',
    ),
    CommandPaletteItem(
      keybinding: 'o',
      label: 'projects',
      description: 'manage projects',
    ),
  ];

  @override
  State<CommandPalette> createState() => _CommandPaletteState();
}

class _CommandPaletteState extends State<CommandPalette> {
  int _selected = 0;
  final _focusNode = FocusNode();
  final _filterController = TextEditingController();

  /// Items matching the current filter query. Empty query => all items.
  /// Matching is case-insensitive substring against label + description,
  /// mirroring the TUI fuzzy finder's intent (modal.go getFilteredItems).
  List<CommandPaletteItem> get _filteredItems {
    final query = _filterController.text.trim().toLowerCase();
    if (query.isEmpty) return widget.items;
    return widget.items
        .where(
          (i) =>
              i.label.toLowerCase().contains(query) ||
              i.description.toLowerCase().contains(query),
        )
        .toList();
  }

  @override
  void initState() {
    super.initState();
    _filterController.addListener(_onFilterChanged);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _focusNode.requestFocus();
    });
  }

  void _onFilterChanged() {
    if (!mounted) return;
    setState(() {
      // Clamp/reset selection into the new filtered range.
      if (_selected >= _filteredItems.length) {
        _selected = (_filteredItems.length - 1).clamp(0, 1 << 31);
      }
    });
  }

  @override
  void didUpdateWidget(covariant CommandPalette oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_selected >= widget.items.length) {
      _selected = widget.items.length - 1;
    }
    if (_selected < 0 && widget.items.isNotEmpty) {
      _selected = 0;
    }
  }

  @override
  void dispose() {
    _filterController.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (widget.items.isEmpty) return KeyEventResult.ignored;
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final filtered = _filteredItems;
    if (filtered.isEmpty) {
      // Allow typing to continue updating the filter; swallow navigation.
      if (key == LogicalKeyboardKey.escape) {
        Navigator.of(context).maybePop();
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    if (key == LogicalKeyboardKey.arrowDown) {
      setState(() => _selected = (_selected + 1) % filtered.length);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowUp) {
      setState(
        () => _selected = (_selected - 1 + filtered.length) % filtered.length,
      );
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.enter) {
      widget.onSelected(filtered[_selected]);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.escape) {
      Navigator.of(context).maybePop();
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    final filtered = _filteredItems;
    return Focus(
      focusNode: _focusNode,
      onKeyEvent: _handleKeyEvent,
      child: Container(
        color: CyberpunkColors.darkGray,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            // Type-to-filter field. Keys typed here update the filter; the
            // Focus above handles navigation keys (arrows/enter/escape).
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
              child: TextField(
                controller: _filterController,
                autofocus: true,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.veryLightGray,
                  fontFamily: 'SourceCodePro',
                ),
                decoration: InputDecoration(
                  isDense: true,
                  hintText: 'filter commands…',
                  hintStyle: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.midGray,
                  ),
                  prefixIcon: const Icon(
                    Icons.search,
                    size: 16,
                    color: CyberpunkColors.midGray,
                  ),
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
            ),
            Flexible(
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: filtered.length,
                itemBuilder: (context, index) {
                  final item = filtered[index];
                  final isSel = index == _selected;
                  return InkWell(
                    onTap: () => widget.onSelected(item),
                    onHover: (h) {
                      if (h && mounted) setState(() => _selected = index);
                    },
                    child: Container(
                      color: isSel
                          ? CyberpunkColors.orangePrimary.withValues(
                              alpha: 0.15,
                            )
                          : null,
                      padding: const EdgeInsets.symmetric(
                        horizontal: 16,
                        vertical: 10,
                      ),
                      child: Row(
                        children: [
                          SizedBox(
                            width: 30,
                            child: Text(
                              item.keybinding,
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.midGray,
                                fontFamily: 'SourceCodePro',
                              ),
                            ),
                          ),
                          SizedBox(
                            width: 130,
                            child: Text(
                              item.label,
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: isSel
                                    ? CyberpunkColors.orangePrimary
                                    : CyberpunkColors.greenSuccess,
                                fontFamily: 'SourceCodePro',
                              ),
                            ),
                          ),
                          Expanded(
                            child: Text(
                              item.description,
                              style: CyberpunkTypography.bodySmall.copyWith(
                                color: CyberpunkColors.lightGray,
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}
