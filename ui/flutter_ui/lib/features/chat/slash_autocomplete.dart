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
///
/// When the query starts with `/skill ` (trailing space), the popup switches
/// to skill-name mode and shows matching entries from [skillNames] instead of
/// the command list.
class SlashAutocomplete extends StatefulWidget {
  final String query;
  /// Parent-owned selection index (0-based within the visible 8-item window).
  final int selectedIndex;
  final void Function(SlashCommand command)? onSelected;
  final VoidCallback? onDismiss;

  /// Skill names injected from the parent; used to suggest skill name
  /// completions after the user types `/skill `.
  final List<String> skillNames;

  /// Called when the user accepts a skill name suggestion.  The parent
  /// typically inserts `/skill <name> ` into the input.
  final void Function(String skillName)? onSkillSelected;

  const SlashAutocomplete({
    super.key,
    required this.query,
    required this.selectedIndex,
    this.onSelected,
    this.onDismiss,
    this.skillNames = const [],
    this.onSkillSelected,
  });

  @override
  State<SlashAutocomplete> createState() => _SlashAutocompleteState();
}

class _SlashAutocompleteState extends State<SlashAutocomplete> {
  static final _registry = SlashCommandRegistry();

  late List<SlashCommand> _matches;
  late List<String> _skillMatches;
  late bool _skillMode;

  @override
  void initState() {
    super.initState();
    _updateMatches();
    _isStateReady = true;
  }

  @override
  void didUpdateWidget(covariant SlashAutocomplete oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.query != widget.query ||
        oldWidget.skillNames != widget.skillNames) {
      _updateMatches();
    }
  }

  /// Returns the argument portion after `/skill ` (or `null` if the query
  /// is not in skill-name mode).
  static String? _skillArg(String query) {
    if (!query.startsWith('/skill ')) return null;
    return query.substring('/skill '.length);
  }

  void _updateMatches() {
    final arg = _skillArg(widget.query);
    _skillMode = arg != null;
    if (_skillMode) {
      // In skill-name mode, filter skill names by the argument prefix.
      final lowerArg = arg!.toLowerCase();
      _skillMatches = widget.skillNames
          .where((n) => n.toLowerCase().startsWith(lowerArg))
          .take(8)
          .toList();
      _matches = const [];
    } else {
      _skillMatches = const [];
      _matches = _registry.match(widget.query);
    }

    final empty = _skillMode ? _skillMatches.isEmpty : _matches.isEmpty;
    if (empty) {
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
    if (_skillMode) {
      if (_skillMatches.isEmpty) return;
      final visible = _skillMatches.take(8).toList();
      final idx = widget.selectedIndex.clamp(0, visible.length - 1);
      widget.onSkillSelected?.call(visible[idx]);
      return;
    }
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
    if (_skillMode) {
      if (_skillMatches.isEmpty) return const SizedBox.shrink();
      final visible = _skillMatches.take(8).toList();
      return _buildPopup(
        itemCount: visible.length,
        itemBuilder: (context, index) {
          final name = visible[index];
          final isSelected = index == widget.selectedIndex;
          return _buildSkillItem(name, isSelected);
        },
      );
    }

    if (_matches.isEmpty) return const SizedBox.shrink();
    final visible = _matches.take(8).toList();
    return _buildPopup(
      itemCount: visible.length,
      itemBuilder: (context, index) {
        final cmd = visible[index];
        final isSelected = index == widget.selectedIndex;
        return _buildItem(cmd, widget.query, isSelected, index);
      },
    );
  }

  /// Shared popup container for both command and skill-name modes.
  Widget _buildPopup({
    required int itemCount,
    required IndexedWidgetBuilder itemBuilder,
  }) {
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
            itemCount: itemCount,
            itemBuilder: itemBuilder,
          ),
        ),
      ),
    );
  }

  /// Build a skill-name autocomplete row.
  Widget _buildSkillItem(String name, bool isSelected) {
    return InkWell(
      onTap: _accept,
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        color: isSelected
            ? CyberpunkColors.orangePrimary.withValues(alpha: 0.15)
            : null,
        child: Row(
          children: [
            Text(
              name.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.greenSuccess,
                fontWeight: FontWeight.bold,
                fontFamily: 'SourceCodePro',
              ),
            ),
          ],
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
