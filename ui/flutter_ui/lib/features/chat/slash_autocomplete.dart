import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../core/slash_commands.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';

/// Slash command autocomplete popup overlay.
///
/// Displays up to 8 matching commands below the input field.
/// The selected index is owned by the parent (ChatInput) and passed down so
/// that both the parent's key handler and this widget's own Focus tree stay
/// in sync. Arrow-key navigation is handled by the parent; this widget only
/// handles click selection, Tab/Enter to accept, and Escape to dismiss.
class SlashAutocomplete extends StatefulWidget {
  final String query;
  /// Parent-owned selection index (0-based within the visible 8-item window).
  final int selectedIndex;
  final void Function(SlashCommand command)? onSelected;
  final VoidCallback? onDismiss;

  const SlashAutocomplete({
    super.key,
    required this.query,
    required this.selectedIndex,
    this.onSelected,
    this.onDismiss,
  });

  @override
  State<SlashAutocomplete> createState() => _SlashAutocompleteState();
}

class _SlashAutocompleteState extends State<SlashAutocomplete> {
  static final _registry = SlashCommandRegistry();

  late List<SlashCommand> _matches;

  @override
  void initState() {
    super.initState();
    _updateMatches();
    _isStateReady = true;
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
    if (!_isStateReady) return;
    setState(() {});
  }

  /// Set to true once initState has finished, so _updateMatches can
  /// safely call setState on subsequent invocations (e.g. from didUpdateWidget).
  bool _isStateReady = false;

  void _accept() {
    if (_matches.isEmpty) return;
    final visible = _matches.take(8).toList();
    final idx = widget.selectedIndex.clamp(0, visible.length - 1);
    widget.onSelected?.call(visible[idx]);
  }

  KeyEventResult _handleKeyEvent(FocusNode node, KeyEvent event) {
    if (event is KeyDownEvent) {
      // Arrow-key navigation is handled by the parent (ChatInput) which owns
      // the selected index. We only handle accept/cancel here.
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
              final isSelected = index == widget.selectedIndex;
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
