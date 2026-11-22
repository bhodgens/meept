import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../providers/providers.dart';
import '../../features/tasks/tasks_tab.dart' show activeTaskProvider;
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../theme/markdown_style.dart';
import '../../theme/syntax_highlighter.dart';
import '../../models/api_models.dart';

class ChatMessageBubble extends ConsumerStatefulWidget {
  final ChatMessage message;
  final String? highlightQuery;
  final bool caseSensitive;
  final bool isRegex;
  final List<FindMatch> highlightRanges;
  final int currentRangeAbsIndex;
  final List<int> rangeAbsIndices;
  final String? regexError;

  const ChatMessageBubble({
    super.key,
    required this.message,
    this.highlightQuery,
    this.caseSensitive = false,
    this.isRegex = false,
    this.highlightRanges = const [],
    this.currentRangeAbsIndex = -1,
    this.rangeAbsIndices = const [],
    this.regexError,
  });

  @override
  ConsumerState<ChatMessageBubble> createState() => _ChatMessageBubbleState();
}

class _ChatMessageBubbleState extends ConsumerState<ChatMessageBubble> {
  // 05-navigation T3: lazily-hydrated C4 trace + C5 route evidence,
  // fetched once per expand (never eagerly -- the bubble must not
  // issue an RPC storm while scrolling transcripts).
  bool _traceExpanded = false;
  List<ToolCallRecord>? _traceRows;
  bool _traceLoading = false;
  bool _routeExpanded = false;
  List<Map<String, dynamic>>? _routeRows;
  bool _routeLoading = false;

  ChatMessage get message => widget.message;
  String? get highlightQuery => widget.highlightQuery;
  List<FindMatch> get highlightRanges => widget.highlightRanges;
  List<int> get rangeAbsIndices => widget.rangeAbsIndices;
  int get currentRangeAbsIndex => widget.currentRangeAbsIndex;

  /// A reply is "correlated" when the terminal's C1 identity rode it --
  /// the seam that unlocks the trace expand + route popover affordances.
  bool get _correlated =>
      message.role == 'assistant' &&
      message.taskId.isNotEmpty &&
      message.sessionId != null &&
      message.sessionId!.isNotEmpty;

  void _toggleTrace() {
    setState(() => _traceExpanded = !_traceExpanded);
    if (_traceExpanded && _traceRows == null && !_traceLoading) {
      _loadTrace();
    }
  }

  Future<void> _loadTrace() async {
    setState(() => _traceLoading = true);
    final client = ref.read(sdkClientProvider);
    try {
      final page = await client.getMessagesPage(
        message.sessionId!,
        limit: 200,
        includeToolCalls: true,
      );
      final rows = <ToolCallRecord>[];
      for (final raw in page.messages) {
        final calls = raw['tool_calls'];
        if (calls is! List) continue;
        // Correlate on the reply content when the message carries it;
        // otherwise surface every persisted call on the page (bounded).
        for (final entry in calls.whereType<Object>()) {
          rows.add(ToolCallRecord.fromLegacy(entry));
        }
      }
      if (!mounted) return;
      setState(() {
        _traceLoading = false;
        _traceRows = rows;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _traceLoading = false);
    }
  }

  void _toggleRoute() {
    setState(() => _routeExpanded = !_routeExpanded);
    if (_routeExpanded && _routeRows == null && !_routeLoading) {
      _loadRoute();
    }
  }

  Future<void> _loadRoute() async {
    setState(() => _routeLoading = true);
    final client = ref.read(sdkClientProvider);
    try {
      final raw = await client.getDispatchTrace(message.sessionId!);
      final entries = (raw['entries'] as List?)?.whereType<Map>() ?? [];
      final rows = entries.map((e) => Map<String, dynamic>.from(e)).toList();
      if (!mounted) return;
      setState(() {
        _routeLoading = false;
        _routeRows = rows;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _routeLoading = false);
    }
  }

  void _openTaskDetail() {
    // Reply task chip -> task detail (gate tasks_detail_test chip seam:
    // the same activeTaskProvider the list tap writes). Navigation is
    // best-effort: outside a GoRouter context the selection still lands.
    ref.read(activeTaskProvider.notifier).state = Task(
      id: message.taskId,
      title: message.taskId,
      description: '',
      status: 'pending',
      createdAt: message.timestamp,
    );
    final router = GoRouter.maybeOf(context);
    if (router != null) {
      router.goNamed('tasks');
    }
  }

  @override
  Widget build(BuildContext context) {
    final isUser = message.role == 'user';
    final isSystem = message.role == 'system';
    // Live rendering prefs: markdown on/off + word wrap toggle.
    final prefs = ref.watch(renderingPrefsProvider);

    // Snapshot of image parts for this message, computed once per build.
    final imageParts = message.parts
        .where((p) => p.type == 'image_url')
        .toList(growable: false);

    // System messages have distinct styling for errors and notifications
    if (isSystem) {
      return Container(
        width: double.infinity,
        margin: const EdgeInsets.symmetric(vertical: 8, horizontal: 12),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.redAlert.withValues(alpha: 0.15),
          border: Border.all(color: CyberpunkColors.redAlert, width: 1),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.error_outline,
                  color: CyberpunkColors.redAlert,
                  size: 16,
                ),
                const SizedBox(width: 8),
                Text(
                  'system',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.redAlert,
                    fontWeight: FontWeight.bold,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            highlightQuery == null
                ? Text(
                    message.content,
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.redAlert,
                    ),
                  )
                : _highlightedText(
                    content: message.content,
                    baseStyle: CyberpunkTypography.bodyMedium.copyWith(
                      color: CyberpunkColors.redAlert,
                    ),
                  ),
          ],
        ),
      );
    }

    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.7,
        ),
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: isUser
              ? CyberpunkColors.orangePrimary.withValues(alpha: 0.6)
              : CyberpunkColors.midGray.withValues(alpha: 0.6),
          border: Border.all(
            color: isUser
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.lightGray,
            width: 1,
          ),
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(8),
            topRight: const Radius.circular(8),
            bottomLeft: Radius.circular(isUser ? 8 : 2),
            bottomRight: Radius.circular(isUser ? 2 : 8),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Highlight overlay: when a find query is active we render a
            // RichText-on-plaintext below the markdown rendering so matches
            // stay visible. MarkdownBody keeps selectable text intact, but
            // highlighting inside MarkdownBody requires a custom builder
            // delegate which is out of scope for the MVP.
            //
            // 05-navigation T1/T3 (mode-chip TODO closed): a correlated
            // reply surfaces its C1 dispatch evidence as chips -- task id
            // (tap -> task detail), the mapped mode from handler_case,
            // plus the trace-expand (C4) and why-this-route (C5)
            // affordances -- instead of relying on the markdown
            // `**mode:**` line.
            if (!isUser &&
                (message.taskId.isNotEmpty ||
                    message.handlerCase.isNotEmpty ||
                    message.suggestedMode.isNotEmpty)) ...[
              const SizedBox(height: 6),
              _correlationChips(context),
            ],
            if (prefs.markdown)
              MarkdownBody(
                data: message.content,
                styleSheet: buildCyberpunkMarkdownStyle(context).copyWith(
                  p: isUser
                      ? CyberpunkTypography.bodyMedium.copyWith(
                          color: CyberpunkColors.orangeGlow,
                        )
                      : null,
                ),
                syntaxHighlighter: CyberpunkSyntaxHighlighter(),
                // word_wrap=false keeps long tokens on one line with a
                // horizontal scroll; true (default) wraps naturally.
                softLineBreak: prefs.wordWrap,
              )
            else
              SelectableText(
                message.content,
                style: isUser
                    ? CyberpunkTypography.bodyMedium.copyWith(
                        color: CyberpunkColors.orangeGlow,
                      )
                    : CyberpunkTypography.bodyMedium,
              ),
            if (highlightQuery != null && highlightRanges.isNotEmpty) ...[
              const SizedBox(height: 4),
              _highlightedText(
                content: message.content,
                baseStyle: CyberpunkTypography.bodySmall.copyWith(
                  color: isUser ? CyberpunkColors.orangeGlow : null,
                ),
                maxLines: 3,
              ),
            ],
            // Multimodal image attachment chips.  The backend sends the
            // user's message content as plain text plus structured `parts`
            // for images; render the image parts here as removable chips.
            if (imageParts.isNotEmpty) ...[
              const SizedBox(height: 4),
              Wrap(
                spacing: 4,
                runSpacing: 2,
                children: [
                  for (final part in imageParts)
                    Text(
                      '[${_labelForImagePart(part)}]',
                      style: CyberpunkTypography.bodySmall.copyWith(
                        color: isUser
                            ? CyberpunkColors.orangeGlow
                            : CyberpunkColors.greenSuccess,
                        fontSize: 11,
                      ),
                    ),
                ],
              ),
            ],
            const SizedBox(height: 4),
            Text(
              _formatTime(message.timestamp),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.lightGray,
                fontSize: 10,
              ),
            ),
            // C1 correlation chips + C4/C5 expand affordances (rendered
            // below the timestamp so long transcripts keep scanning).
            if (!isUser && _hasCorrelationSurface) _correlationChips(context),
            if (_traceExpanded) _tracePanel(),
            if (_routeExpanded) _routePanel(),
          ],
        ),
      ),
    );
  }

  bool get _hasCorrelationSurface =>
      message.taskId.isNotEmpty ||
      message.handlerCase.isNotEmpty ||
      message.suggestedMode.isNotEmpty;

  /// Map a raw suggested_mode / handler_case token to its user-facing
  /// chip label. Never leaks the raw underscored enum for the mode
  /// signal (06-planning gate T4); the dispatch-evidence chip keeps the
  /// handler_case verbatim because it IS the evidence string the C1
  /// correlation gate pins (chat_correlation_test).
  static String _modeLabel(String mode) {
    switch (mode) {
      case 'plan':
        return 'plan';
      case 'quick_plan':
        return 'quick';
      case 'direct':
        return 'direct';
      case 'recall':
        return 'recall';
      default:
        return mode.replaceAll('_', ' ');
    }
  }

  Widget _chip({
    required String label,
    Color? color,
    VoidCallback? onTap,
    String? tooltip,
  }) {
    final tint = color ?? CyberpunkColors.lightGray;
    final chip = Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: tint.withValues(alpha: 0.15),
        border: Border.all(color: tint, width: 1),
        borderRadius: BorderRadius.circular(3),
      ),
      child: Text(
        label,
        style: CyberpunkTypography.bodySmall.copyWith(
          color: tint,
          fontSize: 10,
          fontFamily: 'SourceCodePro',
        ),
      ),
    );
    Widget result = chip;
    if (onTap != null) {
      result = InkWell(onTap: onTap, child: chip);
    }
    if (tooltip != null) {
      result = Tooltip(message: tooltip, child: result);
    }
    return result;
  }

  Widget _correlationChips(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Wrap(
        spacing: 6,
        runSpacing: 4,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          if (message.taskId.isNotEmpty)
            _chip(
              label: 'task ${message.taskId}',
              color: CyberpunkColors.greenSuccess,
              onTap: _openTaskDetail,
              tooltip: 'open task detail',
            ),
          if (message.suggestedMode.isNotEmpty)
            _chip(
              label: _modeLabel(message.suggestedMode),
              color: CyberpunkColors.blueInfo,
              tooltip: 'dispatch mode',
            ),
          if (message.handlerCase.isNotEmpty)
            _chip(
              label: message.handlerCase,
              color: CyberpunkColors.yellowWarning,
              tooltip: 'handler case (dispatch evidence)',
            ),
          if (_correlated)
            _chip(
              label: _traceExpanded ? 'trace (hide)' : 'trace',
              color: CyberpunkColors.orangePrimary,
              onTap: _toggleTrace,
              tooltip: 'show persisted tool calls (C4)',
            ),
          if (_correlated)
            _chip(
              label: _routeExpanded ? 'route (hide)' : 'route',
              color: CyberpunkColors.orangePrimary,
              onTap: _toggleRoute,
              tooltip: 'why this route (dispatch trace, C5)',
            ),
        ],
      ),
    );
  }

  Widget _tracePanel() {
    final rows = _traceRows;
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border.all(color: CyberpunkColors.lightGray, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'tool trace',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 4),
          if (_traceLoading)
            const Text(
              'loading trace...',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else if (rows == null)
            const Text(
              'trace unavailable',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else if (rows.isEmpty)
            const Text(
              'no persisted tool calls',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else
            for (final call in rows)
              Padding(
                padding: const EdgeInsets.only(bottom: 2),
                child: Text(
                  '${call.name}  ${call.argsSummary}'
                  '${call.resultSummary.isEmpty ? '' : ' -> ${call.resultSummary}'}',
                  style: TextStyle(
                    color: CyberpunkColors.greenSuccess,
                    fontSize: 11,
                    fontFamily: 'SourceCodePro',
                  ),
                ),
              ),
        ],
      ),
    );
  }

  Widget _routePanel() {
    final rows = _routeRows;
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: CyberpunkColors.darkGray.withValues(alpha: 0.5),
        border: Border.all(color: CyberpunkColors.lightGray, width: 1),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'why this route',
            style: CyberpunkTypography.bodySmall.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 4),
          if (_routeLoading)
            const Text(
              'loading dispatch trace...',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else if (rows == null)
            const Text(
              'dispatch trace unavailable',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else if (rows.isEmpty)
            const Text(
              'no dispatch evidence recorded',
              style: TextStyle(color: Colors.white70, fontSize: 11),
            )
          else
            for (final row in rows)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if ((row['input_summary'] as String? ?? '').isNotEmpty)
                      Text(
                        row['input_summary'] as String,
                        style: const TextStyle(
                          color: Colors.white70,
                          fontSize: 11,
                        ),
                      ),
                    Text(
                      'intent: ${row['intent_type'] ?? ''}  '
                      'method: ${row['classifier_method'] ?? ''}  '
                      'confidence: ${row['confidence'] ?? ''}',
                      style: TextStyle(
                        color: CyberpunkColors.greenSuccess,
                        fontSize: 11,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                    Text(
                      'handler: ${row['handler_case'] ?? ''}  '
                      'task: ${row['task_id'] ?? ''}  '
                      'turn: ${row['turn_id'] ?? ''}',
                      style: TextStyle(
                        color: CyberpunkColors.lightGray,
                        fontSize: 11,
                        fontFamily: 'SourceCodePro',
                      ),
                    ),
                  ],
                ),
              ),
        ],
      ),
    );
  }

  /// Builds a Text.rich with matched spans highlighted (orange for current
  /// match, semi-transparent orange for other matches).
  Widget _highlightedText({
    required String content,
    required TextStyle baseStyle,
    int? maxLines,
  }) {
    if (highlightRanges.isEmpty) {
      return Text(
        content,
        style: baseStyle,
        maxLines: maxLines,
        overflow: maxLines == null ? null : TextOverflow.ellipsis,
      );
    }

    final spans = <TextSpan>[];
    var prev = 0;
    for (var i = 0; i < highlightRanges.length; i++) {
      final range = highlightRanges[i];
      final absIdx = rangeAbsIndices.isEmpty ? i : rangeAbsIndices[i];
      final start = range.start.clamp(0, content.length);
      final end = range.end.clamp(start, content.length);
      if (start > prev) {
        spans.add(
          TextSpan(text: content.substring(prev, start), style: baseStyle),
        );
      }
      final isCurrent = absIdx == currentRangeAbsIndex;
      spans.add(
        TextSpan(
          text: content.substring(start, end),
          style: baseStyle.copyWith(
            background: Paint()
              ..color = isCurrent
                  ? CyberpunkColors.orangePrimary
                  : CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
            color: isCurrent ? CyberpunkColors.black : null,
          ),
        ),
      );
      prev = end;
    }
    if (prev < content.length) {
      spans.add(TextSpan(text: content.substring(prev), style: baseStyle));
    }
    return Text.rich(
      TextSpan(children: spans, style: baseStyle),
      maxLines: maxLines,
      overflow: maxLines == null ? null : TextOverflow.ellipsis,
    );
  }

  String _formatTime(DateTime time) {
    final hour = time.hour.toString().padLeft(2, '0');
    final minute = time.minute.toString().padLeft(2, '0');
    return '$hour:$minute';
  }

  /// Derive a display label for an image attachment chip from its URL.
  /// Falls back to 'image' when the URL is empty or unparseable.
  String _labelForImagePart(ChatMessagePart part) {
    final url = part.imageUrl?.url ?? '';
    if (url.isEmpty) return 'image';
    final withoutQuery = url.split('?').first;
    final segs = withoutQuery.split('/');
    final candidate = segs.isEmpty ? '' : segs.last;
    if (candidate.isEmpty) return 'image';
    // strip the 'file://' prefix when present so we show the upload id only.
    if (candidate.startsWith('file://')) {
      final rest = candidate.substring('file://'.length);
      return rest.isEmpty ? 'image' : rest;
    }
    return candidate;
  }
}
