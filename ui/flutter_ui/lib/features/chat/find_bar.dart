import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'find_state.dart';

/// A compact horizontal bar for in-session find.
///
/// Sits at the top of the chat content area, below the session header.
/// Renders a text input, match count, case/regex toggles, and a close button.
/// On any change, callers recompute matches via [computeFindMatches] and
/// update [findMatchesProvider] / [findCursorProvider].
class FindBar extends ConsumerStatefulWidget {
  final String sessionId;
  final int matchCount;
  final String? regexError;

  const FindBar({
    super.key,
    required this.sessionId,
    required this.matchCount,
    this.regexError,
  });

  @override
  ConsumerState<FindBar> createState() => _FindBarState();
}

class _FindBarState extends ConsumerState<FindBar> {
  late final TextEditingController _controller;
  late final FocusNode _focusNode;

  @override
  void initState() {
    super.initState();
    final initial = ref.read(findQueryProvider(widget.sessionId));
    _controller = TextEditingController(text: initial);
    _controller.addListener(() {
      ref
          .read(findQueryProvider(widget.sessionId).notifier)
          .state = _controller.text;
      ref.read(findCursorProvider(widget.sessionId).notifier).state = 0;
    });
    _focusNode = FocusNode();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        _focusNode.requestFocus();
      }
    });
  }

  @override
  void dispose() {
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final sessionId = widget.sessionId;
    final cursor = ref.watch(findCursorProvider(sessionId));
    final caseSensitive = ref.watch(findCaseSensitiveProvider(sessionId));
    final regex = ref.watch(findRegexProvider(sessionId));

    final countText = widget.matchCount == 0
        ? '0/0'
        : '${(cursor % widget.matchCount) + 1}/${widget.matchCount}';

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      color: const Color(0xFF1F2937),
      child: Row(
        children: [
          Expanded(
            child: TextField(
              controller: _controller,
              focusNode: _focusNode,
              style: CyberpunkTypography.bodyMedium
                  .copyWith(color: Colors.white),
              cursorColor: CyberpunkColors.orangePrimary,
              decoration: InputDecoration(
                isDense: true,
                contentPadding:
                    const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                hintText: 'find...',
                hintStyle: CyberpunkTypography.bodySmall
                    .copyWith(color: CyberpunkColors.lightGray),
                filled: true,
                fillColor: CyberpunkColors.black,
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(4),
                  borderSide: BorderSide(
                    color: regex
                        ? CyberpunkColors.orangePrimary
                        : CyberpunkColors.midGray,
                  ),
                ),
                focusedBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(4),
                  borderSide: const BorderSide(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
              ),
              onSubmitted: (_) => _next(widget.matchCount, sessionId),
            ),
          ),
          const SizedBox(width: 8),
          SizedBox(
            width: 56,
            child: Text(
              countText,
              textAlign: TextAlign.center,
              style: CyberpunkTypography.bodySmall
                  .copyWith(color: CyberpunkColors.lightGray),
            ),
          ),
          _toggleButton(
            label: 'Aa',
            active: caseSensitive,
            tooltip: 'case sensitive (alt+c)',
            onTap: () {
              ref
                  .read(findCaseSensitiveProvider(sessionId).notifier)
                  .state = !caseSensitive;
            },
          ),
          _toggleButton(
            label: '.*',
            active: regex,
            tooltip: 'regex mode (alt+r)',
            onTap: () {
              ref.read(findRegexProvider(sessionId).notifier).state = !regex;
            },
          ),
          IconButton(
            icon: const Icon(Icons.close, size: 16),
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
            color: CyberpunkColors.lightGray,
            tooltip: 'close (esc)',
            onPressed: () => _close(sessionId),
          ),
          if (widget.regexError != null)
            Padding(
              padding: const EdgeInsets.only(left: 8),
              child: Text(
                'regex error',
                style: CyberpunkTypography.bodySmall
                    .copyWith(color: CyberpunkColors.redAlert),
              ),
            ),
        ],
      ),
    );
  }

  Widget _toggleButton({
    required String label,
    required bool active,
    required String tooltip,
    required VoidCallback onTap,
  }) {
    return Tooltip(
      message: tooltip,
      child: GestureDetector(
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          margin: const EdgeInsets.symmetric(horizontal: 2),
          decoration: BoxDecoration(
            color: active
                ? CyberpunkColors.orangePrimary
                : Colors.transparent,
            border: Border.all(
              color: active
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.midGray,
            ),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Text(
            label,
            style: CyberpunkTypography.bodySmall.copyWith(
              color: active ? CyberpunkColors.black : CyberpunkColors.lightGray,
              fontWeight: FontWeight.bold,
              fontFamily: 'SourceCodePro',
            ),
          ),
        ),
      ),
    );
  }

  void _next(int count, String sessionId) {
    if (count == 0) return;
    final cur = ref.read(findCursorProvider(sessionId));
    ref.read(findCursorProvider(sessionId).notifier).state = (cur + 1) % count;
  }

  void _close(String sessionId) {
    ref.read(findBarVisibleProvider(sessionId).notifier).state = false;
    ref.read(findQueryProvider(sessionId).notifier).state = '';
    ref.read(findCursorProvider(sessionId).notifier).state = 0;
  }
}
