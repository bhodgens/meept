// Card detail sheet (leaf 05/05): timeline over KanbanService.cardTimeline
// rendered oldest-first, plus a comment box that posts through
// KanbanService.commentCard and prepends the reply event to the timeline.
// Modest by design — the leaf asks for detail = timeline + comment.
//
// The service is passed in (not read from the provider) so this sheet is
// testable with a fake and reusable from any host; all UI text lowercase.

import 'package:flutter/material.dart';

import 'kanban_models.dart';
import 'kanban_service.dart';

class CardDetailSheet extends StatefulWidget {
  const CardDetailSheet({
    super.key,
    required this.service,
    required this.card,
    required this.columnName,
    this.onChanged,
  });

  final KanbanService service;
  final KanbanCard card;

  /// Column the card currently sits in (for the header legend line).
  final String columnName;

  /// Called after a successful comment so the board can reload.
  final Future<void> Function()? onChanged;

  @override
  State<CardDetailSheet> createState() => _CardDetailSheetState();
}

class _CardDetailSheetState extends State<CardDetailSheet> {
  final _commentController = TextEditingController();
  List<KanbanCardEvent> _timeline = const [];
  bool _loading = false;
  bool _posting = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadTimeline();
  }

  @override
  void dispose() {
    _commentController.dispose();
    super.dispose();
  }

  Future<void> _loadTimeline() async {
    setState(() => _loading = true);
    try {
      final events = await widget.service.cardTimeline(widget.card.id);
      if (!mounted) return;
      setState(() {
        _timeline = events;
        _loading = false;
      });
    } on KanbanApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.message.toLowerCase();
      });
    }
  }

  Future<void> _postComment() async {
    final text = _commentController.text.trim();
    if (text.isEmpty || _posting) return;
    setState(() => _posting = true);
    try {
      final event = await widget.service.commentCard(widget.card.id, text);
      if (!mounted) return;
      _commentController.clear();
      setState(() {
        // Append the reply event; the build sorts oldest-first by
        // timestamp, so the comment lands at the bottom of the history.
        _timeline = [..._timeline, event];
        _posting = false;
      });
      await widget.onChanged?.call();
    } on KanbanForbiddenException catch (e) {
      if (mounted) {
        setState(() => _posting = false);
        _showError(
          'forbidden: comment needs the ${e.grant.isEmpty ? 'board.mutate' : e.grant} grant',
        );
      }
    } on KanbanApiException catch (e) {
      if (mounted) {
        setState(() => _posting = false);
        _showError('comment failed: ${e.message.toLowerCase()}');
      }
    }
  }

  void _showError(String message) {
    ScaffoldMessenger.of(context)
      ..removeCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    // Timeline renders oldest-first (the gateway returns chronological
    // events; sort defensively by timestamp when present).
    final sorted = [..._timeline];
    sorted.sort((a, b) {
      final at = a.createdAt;
      final bt = b.createdAt;
      if (at == null || bt == null) return 0;
      return at.compareTo(bt);
    });

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 4, 16, 16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            widget.card.title,
            style: const TextStyle(
              color: Colors.white,
              fontSize: 15,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            _subtitle(),
            style: const TextStyle(color: Colors.white54, fontSize: 11),
          ),
          const SizedBox(height: 8),
          const Text(
            'timeline',
            style: TextStyle(
              color: Colors.orange,
              fontSize: 12,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 4),
          Flexible(
            child: _loading && sorted.isEmpty
                ? const Center(
                    child: Padding(
                      padding: EdgeInsets.all(16),
                      child: CircularProgressIndicator(),
                    ),
                  )
                : ListView.builder(
                    key: const ValueKey('kanban-timeline'),
                    shrinkWrap: true,
                    itemCount: sorted.length,
                    itemBuilder: (context, index) =>
                        _TimelineRow(event: sorted[index]),
                  ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: Text(
                'timeline error: $_error',
                style: const TextStyle(color: Colors.red, fontSize: 11),
              ),
            ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: TextField(
                  key: const ValueKey('kanban-comment-input'),
                  controller: _commentController,
                  enabled: !_posting,
                  style: const TextStyle(color: Colors.white, fontSize: 12),
                  decoration: const InputDecoration(
                    isDense: true,
                    hintText: 'add a comment…',
                    hintStyle: TextStyle(color: Colors.white38, fontSize: 12),
                    border: OutlineInputBorder(),
                  ),
                  onSubmitted: (_) => _postComment(),
                ),
              ),
              const SizedBox(width: 8),
              FilledButton(
                key: const ValueKey('kanban-comment-send'),
                onPressed: _posting ? null : _postComment,
                style: FilledButton.styleFrom(
                  backgroundColor: Colors.orange.shade700,
                ),
                child: const Text('comment', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
        ],
      ),
    );
  }

  String _subtitle() {
    final parts = <String>[
      if (widget.columnName.isNotEmpty) 'in ${widget.columnName.toLowerCase()}',
      if (!widget.card.ref.isEmpty)
        'ref ${widget.card.ref.kind.toLowerCase()}/${widget.card.ref.id}',
      if (widget.card.employeeId.isNotEmpty) 'emp ${widget.card.employeeId}',
      if (widget.card.triggerRef.isNotEmpty)
        'trigger ${widget.card.triggerRef}',
    ];
    return parts.isEmpty ? 'manual card' : parts.join(' · ');
  }
}

class _TimelineRow extends StatelessWidget {
  const _TimelineRow({required this.event});

  final KanbanCardEvent event;

  @override
  Widget build(BuildContext context) {
    final ts = event.createdAt;
    final time = ts == null ? '' : _formatTs(ts);
    final kind = event.kind.isEmpty ? 'event' : event.kind.toLowerCase();
    final actor = event.actor.isEmpty ? 'system' : event.actor;
    final body = event.body.isEmpty ? '' : ' ${event.body}';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Text(
        '$time $kind by ${actor.toLowerCase()}$body',
        style: const TextStyle(color: Colors.white70, fontSize: 11),
      ),
    );
  }

  static String _formatTs(DateTime ts) {
    final local = ts.toLocal();
    String two(int v) => v.toString().padLeft(2, '0');
    return '${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }
}
