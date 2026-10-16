import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../features/memory/memory_review_panel.dart';
import '../features/settings/users_panel.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

/// One entry in the command palette. Mirrors the TUI modal.go:192-205 command
/// modal, adapted to Flutter surfaces.
class CommandPaletteItem {
  final String keybinding;
  final String label;
  final String description;

  /// go_router location to navigate to on selection (null for items the
  /// host screen dispatches itself, e.g. tab switches).
  final String? route;

  /// Widget to push when [route] is null but the item has a direct page
  /// target (no registered route yet — memory review, users).
  final WidgetBuilder? page;

  const CommandPaletteItem({
    required this.keybinding,
    required this.label,
    required this.description,
    this.route,
    this.page,
  });
}

/// Keyboard-navigable command palette overlay. Renders the project's lowercase
/// UI convention; navigation mirrors TUI modal.go (j/k arrows + enter).
///
/// Items with a [CommandPaletteItem.route] push that go_router route; items
/// with a [CommandPaletteItem.page] push that widget directly (used where no
/// route is registered yet, e.g. memory review and users).
class CommandPalette extends StatefulWidget {
  final List<CommandPaletteItem> items;
  final void Function(CommandPaletteItem item) onSelected;

  const CommandPalette({
    super.key,
    required this.items,
    required this.onSelected,
  });

  /// Matches TUI modal.go:192-205, adapted to Flutter surfaces.
  /// Omitted TUI items: queue view, toggle sidebar (no Flutter route; see
  /// internal/tui/testdata/gui_inventory.json deviations).
  static List<CommandPaletteItem> get defaultItems => [
    const CommandPaletteItem(
      keybinding: 'c',
      label: 'chat',
      description: 'switch to chat view',
    ),
    const CommandPaletteItem(
      keybinding: 's',
      label: 'sessions',
      description: 'switch to sessions view',
    ),
    const CommandPaletteItem(
      keybinding: 'p',
      label: 'plans',
      description: 'switch to plans view',
    ),
    const CommandPaletteItem(
      keybinding: 't',
      label: 'tasks',
      description: 'switch to tasks view',
    ),
    // TUI ViewAgents default key (internal/tui/config.go:172) — parity.
    const CommandPaletteItem(
      keybinding: 'e',
      label: 'agents',
      description: 'switch to employees view',
    ),
    const CommandPaletteItem(
      keybinding: 'f',
      label: 'find…',
      description: 'search sessions and tasks',
    ),
    const CommandPaletteItem(
      keybinding: 'n',
      label: 'new session',
      description: 'create a new session',
    ),
    // TUI RenameSession default key (internal/tui/config.go:176) — parity.
    const CommandPaletteItem(
      keybinding: 'r',
      label: 'edit description',
      description: 'edit session description',
    ),
    const CommandPaletteItem(
      keybinding: 'o',
      label: 'projects',
      description: 'manage projects',
    ),
    // MemoryPanel is routed at core/router.dart (/tools/memory).
    const CommandPaletteItem(
      keybinding: 'm',
      label: 'memory',
      description: 'switch to memory view',
      route: '/tools/memory',
    ),
    // TUI memory-review modal key (ViewMemoryReview 'v') — parity.
    CommandPaletteItem(
      keybinding: 'v',
      label: 'memory review',
      description: 'review pending auto-claims (promote/reject)',
      page: MemoryReviewPanel.builder,
    ),
    // TUI users modal key (ViewUsers 'u') — parity; read-only awareness.
    CommandPaletteItem(
      keybinding: 'u',
      label: 'users',
      description: 'multi-user awareness (manage via cli)',
      page: UsersPanel.builder,
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
                  prefixIcon: Icon(
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
