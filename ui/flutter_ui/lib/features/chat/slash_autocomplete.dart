import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../core/slash_commands.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Slash command autocomplete popup overlay.
///
/// Displays up to 8 matching commands below the input field.
/// Handles arrow/tab/enter navigation internally, esc to cancel.
class SlashAutocomplete extends StatefulWidget {
  final String query;
  final void Function(SlashCommand command)? onSelected;
  final VoidCallback? onDismiss;

  const SlashAutocomplete({
    super.key,
    required this.query,
    this.onSelected,
    this.onDismiss,
  });

  @override
  State<SlashAutocomplete> createState() => _SlashAutocompleteState();
}

class _SlashAutocompleteState extends State<SlashAutocomplete> {
  static final _registry = SlashCommandRegistry();

  late List<SlashCommand> _matches;
  int _selectedIndex = 0;

  @override
  void initState() {
    super.initState();
    _updateMatches();
  }

  @override
  void didUpdateWidget(covariant SlashAutocomplete oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.query != widget.query) {
      _updateMatches();
    }
  }

  void _updateMatches() {
    _matches = _registry.match(widget.query);
    if (_matches.isEmpty) {
      // Defer dismiss to avoid calling parent setState during child build
      // (bug F8: onDismiss during initState/didUpdateWidget triggers parent
      // rebuild while child is still building).
      WidgetsBinding.instance.addPostFrameCallback((_) {
        widget.onDismiss?.call();
      });
      return;
    }
    // Clamp selected index to visible range (max 8 items)
    final visibleCount = _matches.length > 8 ? 8 : _matches.length;
    _selectedIndex = _selectedIndex.clamp(0, visibleCount - 1);
    setState(() {});
  }

  void _accept() {
    if (_matches.isEmpty) return;
    widget.onSelected?.call(_matches[_selectedIndex]);
  }

  void _moveSelection(int delta) {
    final visibleCount = _matches.length > 8 ? 8 : _matches.length;
    setState(() {
      _selectedIndex =
          (_selectedIndex + delta).clamp(0, visibleCount - 1);
    });
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent) {
      if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
        _moveSelection(1);
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
        _moveSelection(-1);
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.tab ||
          event.logicalKey == LogicalKeyboardKey.enter) {
        _accept();
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.escape) {
        widget.onDismiss?.call();
        return KeyEventResult.handled;
      }
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) {
    if (_matches.isEmpty) return const SizedBox.shrink();

    final visible = _matches.take(8).toList();

    return Focus(
      onKeyEvent: _handleKeyEvent,
      child: Container(
        margin: const EdgeInsets.only(bottom: 4),
        constraints: const BoxConstraints(maxHeight: 280, maxWidth: 360),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border.all(color: CyberpunkColors.orangePrimary, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Material(
          color: Colors.transparent,
          child: ListView.builder(
            shrinkWrap: true,
            itemCount: visible.length,
            itemBuilder: (context, index) {
              final cmd = visible[index];
              final isSelected = index == _selectedIndex;
              return _buildItem(cmd, widget.query, isSelected, index);
            },
          ),
        ),
      ),
    );
  }

  Widget _buildItem(SlashCommand cmd, String prefix, bool isSelected, int index) {
    // Highlight the matching portion of the command name
    final prefixLen = prefix.length.clamp(0, cmd.name.length);
    final highlighted = cmd.name.substring(0, prefixLen);
    final rest = cmd.name.substring(prefixLen);

    return InkWell(
      onTap: () {
        setState(() => _selectedIndex = index);
        _accept();
      },
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        color: isSelected
            ? CyberpunkColors.orangePrimary.withValues(alpha: 0.15)
            : null,
        child: Row(
          children: [
            Text.rich(
              TextSpan(
                children: [
                  TextSpan(
                    text: highlighted,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.orangePrimary,
                      fontWeight: FontWeight.bold,
                      fontFamily: 'SourceCodePro',
                    ),
                  ),
                  TextSpan(
                    text: rest,
                    style: CyberpunkTypography.bodySmall.copyWith(
                      color: CyberpunkColors.greenSuccess,
                      fontFamily: 'SourceCodePro',
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                cmd.description,
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontSize: 10,
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
