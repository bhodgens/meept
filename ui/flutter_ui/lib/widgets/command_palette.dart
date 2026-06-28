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
            description: 'switch to chat view'),
        CommandPaletteItem(
            keybinding: 's',
            label: 'sessions',
            description: 'switch to sessions view'),
        CommandPaletteItem(
            keybinding: 'p',
            label: 'plans',
            description: 'switch to plans view'),
        CommandPaletteItem(
            keybinding: 't',
            label: 'tasks',
            description: 'switch to tasks view'),
        CommandPaletteItem(
            keybinding: 'a',
            label: 'agents',
            description: 'switch to employees view'),
        CommandPaletteItem(
            keybinding: 'f',
            label: 'find…',
            description: 'search sessions and tasks'),
        CommandPaletteItem(
            keybinding: 'n',
            label: 'new session',
            description: 'create a new session'),
        CommandPaletteItem(
            keybinding: 'e',
            label: 'edit description',
            description: 'edit session description'),
        CommandPaletteItem(
            keybinding: 'o',
            label: 'projects',
            description: 'manage projects'),
      ];

  @override
  State<CommandPalette> createState() => _CommandPaletteState();
}

class _CommandPaletteState extends State<CommandPalette> {
  int _selected = 0;
  final _focusNode = FocusNode();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _focusNode.requestFocus();
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
    _focusNode.dispose();
    super.dispose();
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (widget.items.isEmpty) return KeyEventResult.ignored;
    if (event is! KeyDownEvent) return KeyEventResult.ignored;
    final key = event.logicalKey;
    if (key == LogicalKeyboardKey.arrowDown) {
      setState(() => _selected = (_selected + 1) % widget.items.length);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.arrowUp) {
      setState(() =>
          _selected = (_selected - 1 + widget.items.length) % widget.items.length);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.enter) {
      widget.onSelected(widget.items[_selected]);
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
    return Focus(
      focusNode: _focusNode,
      onKeyEvent: _handleKeyEvent,
      child: Container(
        color: CyberpunkColors.darkGray,
        child: ListView.builder(
          itemCount: widget.items.length,
          itemBuilder: (context, index) {
            final item = widget.items[index];
            final isSel = index == _selected;
            return InkWell(
              onTap: () => widget.onSelected(item),
              onHover: (h) {
                if (h && mounted) setState(() => _selected = index);
              },
              child: Container(
                color: isSel
                    ? CyberpunkColors.orangePrimary.withValues(alpha: 0.15)
                    : null,
                padding:
                    const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
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
    );
  }
}
