// Kanban board view (plan tree: kanban-board 05/05 'flutter-board-view').
//
// GUI twin of the TUI kanban panel (internal/tui/kanban_view*.go): a
// horizontal column list, each column a vertical card list, with the same
// lowercase wip legend semantics ("doing(1/2 wip)" → rendered here as
// "doing (1/2 wip)" per the leaf spec). Card moves go through
// KanbanService.moveCard (tap-to-move primary; drag-and-drop deliberately
// skipped to avoid widget-test flakiness — the leaf allows either). C10
// refusals surface as snackbars: lease-held shows holder + humanized age
// with a takeover action; wip-full names the column and limit; forbidden
// names the grant.
//
// Reactivity follows the C8 interim rule: the view consumes only
// KanbanService.boardEvents() (progress-class raw frames today) and never
// inspects WSBoard-typed payload keys. Frames are coalesced through a
// ~250 ms debounce into ONE board reload, and the subscription is
// cancelled on dispose.
//
// All UI text lowercase per the repo convention (data like card titles is
// shown verbatim; column NAMES are lowercased at render like the TUI's
// strings.ToLower). No dart:io, web-safe by construction.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'card_detail_sheet.dart';
import 'card_tile.dart';
import 'kanban_models.dart';
import 'kanban_provider.dart';
import 'kanban_service.dart';

/// Debounce window for reactive reloads (leaf spec: ~250 ms coalescing).
const Duration kKanbanRefreshDebounce = Duration(milliseconds: 250);

class KanbanBoardView extends ConsumerStatefulWidget {
  const KanbanBoardView({super.key});

  @override
  ConsumerState<KanbanBoardView> createState() => _KanbanBoardViewState();
}

class _KanbanBoardViewState extends ConsumerState<KanbanBoardView> {
  List<KanbanBoard> _boards = const [];
  String? _boardId;
  List<KanbanColumn> _columns = const [];
  List<KanbanCard> _cards = const [];
  bool _loading = false;
  String? _error;
  String? _linkNote;

  StreamSubscription<KanbanBoardEvent>? _eventsSub;
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    _subscribeBoardEvents(ref.read(kanbanServiceProvider));
    _refresh();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _eventsSub?.cancel();
    super.dispose();
  }

  /// C8 interim: subscribe to boardEvents() only. The bridge's stream is
  /// single-subscription by construction (one socket per service, 05/05 is
  /// the sole consumer), so a remount while a previous listener is still
  /// active would throw StateError — tolerated here with a skip: manual
  /// refresh still works, and steady-state usage mounts once per service.
  /// Wanted seam (reported, not edited — bridge is frozen): make
  /// [KanbanService.boardEvents] return a broadcast stream.
  void _subscribeBoardEvents(KanbanService service) {
    try {
      _eventsSub = service.boardEvents().listen(_onBoardEvent);
    } on StateError {
      _eventsSub = null;
    }
  }

  void _onBoardEvent(KanbanBoardEvent event) {
    if (!mounted) return;
    if (event.linkError != null) {
      setState(() => _linkNote = event.linkError);
      return;
    }
    // A live frame means the link recovered since the last error note.
    if (_linkNote != null) setState(() => _linkNote = null);
    // relevance filter is defensive-only: today frames carry no typed keys.
    final boardId = event.boardId;
    if (boardId != null && _boardId != null && boardId != _boardId) return;
    // coalesce: each frame re-arms the debounce; one reload per burst.
    _debounce?.cancel();
    _debounce = Timer(kKanbanRefreshDebounce, () {
      if (mounted) _refresh();
    });
  }

  Future<void> _refresh() async {
    final service = ref.read(kanbanServiceProvider);
    if (_columns.isEmpty) setState(() => _loading = true);
    try {
      final boards = await service.listBoards();
      if (boards.isEmpty) {
        if (mounted) {
          setState(() {
            _loading = false;
            _error = 'no boards found';
            _boards = const [];
          });
        }
        return;
      }
      final keep = _boardId;
      final boardId = boards.any((b) => b.id == keep) ? keep! : boards.first.id;
      final columns = await service.listColumns(boardId);
      final cards = await service.listCards(boardId: boardId);
      if (!mounted) return;
      // sort a copy: the bridge returns fixed-length lists and test fakes
      // may hand back const ones — never mutate an input list in place.
      final sortedColumns = [...columns]
        ..sort((a, b) => a.position.compareTo(b.position));
      setState(() {
        _boards = boards;
        _boardId = boardId;
        _columns = sortedColumns;
        _cards = cards;
        _loading = false;
        _error = null;
      });
    } on KanbanApiException catch (e) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = e.message.toLowerCase();
        });
      }
    }
  }

  Future<void> _selectBoard(String boardId) async {
    setState(() => _boardId = boardId);
    await _refresh();
  }

  KanbanColumn? _columnById(String id) {
    for (final c in _columns) {
      if (c.id == id) return c;
    }
    return null;
  }

  List<KanbanCard> _cardsIn(String columnId) {
    final inColumn = _cards.where((c) => c.columnId == columnId).toList();
    inColumn.sort((a, b) => a.order.compareTo(b.order));
    return inColumn;
  }

  /// Column header legend with TUI semantics: "<name> (<count>)" or
  /// "<name> (<count>/<limit> wip)" when the column has a wip limit
  /// (internal/tui/kanban_view_render.go columnLegend).
  String _legend(KanbanColumn column) {
    final count = _cardsIn(column.id).length;
    final wip = column.wip > 0 ? '/${column.wip} wip' : '';
    return '${column.name.toLowerCase()} ($count$wip)';
  }

  // ------------------------------------------------------------------
  // Move / takeover (C10)
  // ------------------------------------------------------------------

  Future<void> _moveCard(String cardId, String targetColumnId) async {
    final service = ref.read(kanbanServiceProvider);
    final card = _cards.where((c) => c.id == cardId).firstOrNull;
    final target = _columnById(targetColumnId);
    try {
      await service.moveCard(cardId, targetColumnId);
      _showMessage(
        'moved${card == null ? '' : ' "${card.title}"'} to '
        '${(target?.name ?? targetColumnId).toLowerCase()}',
      );
      await _refresh();
    } on KanbanLeaseHeldException catch (e) {
      _showLeaseSnackbar(card, target, e, service);
    } on KanbanWipFullException catch (e) {
      final name = _columnById(e.columnId)?.name ?? target?.name ?? e.columnId;
      _showMessage('wip full: column ${name.toLowerCase()} limit ${e.limit}');
    } on KanbanForbiddenException catch (e) {
      _showMessage(
        'forbidden: token is missing the ${e.grant.isEmpty ? 'board' : e.grant} grant',
      );
    } on KanbanApiException catch (e) {
      _showMessage('move failed: ${e.message.toLowerCase()}');
    }
  }

  void _showLeaseSnackbar(
    KanbanCard? card,
    KanbanColumn? target,
    KanbanLeaseHeldException e,
    KanbanService service,
  ) {
    final holder = e.holder.isEmpty ? 'an episode' : e.holder.toLowerCase();
    final age = humanizeLeaseAge(e.age);
    ScaffoldMessenger.of(context)
      ..removeCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Text('lease held by $holder ($age) — takeover overrides'),
          action: card == null || target == null
              ? null
              : SnackBarAction(
                  label: 'takeover',
                  onPressed: () => _takeoverCard(service, card.id, target.id),
                ),
        ),
      );
  }

  Future<void> _takeoverCard(
    KanbanService service,
    String cardId,
    String toColumnId,
  ) async {
    final target = _columnById(toColumnId);
    try {
      await service.takeoverCard(cardId, toColumnId: toColumnId);
      _showMessage(
        'took over and moved to '
        '${(target?.name ?? toColumnId).toLowerCase()}',
      );
      await _refresh();
    } on KanbanForbiddenException catch (e) {
      _showMessage(
        'forbidden: takeover needs the ${e.grant.isEmpty ? 'board.takeover' : e.grant} grant',
      );
    } on KanbanApiException catch (e) {
      _showMessage('takeover failed: ${e.message.toLowerCase()}');
    }
  }

  Future<void> _openDetail(KanbanCard card) async {
    final service = ref.read(kanbanServiceProvider);
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: const Color(0xFF1E1E24),
      builder: (sheetContext) => CardDetailSheet(
        service: service,
        card: card,
        columnName: _columnById(card.columnId)?.name ?? '',
        onChanged: _refresh,
      ),
    );
  }

  void _showMessage(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..removeCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  // ------------------------------------------------------------------
  // Build
  // ------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _Header(
          boards: _boards,
          selectedBoardId: _boardId,
          linkNote: _linkNote,
          loading: _loading,
          onRefresh: _refresh,
          onSelectBoard: _selectBoard,
        ),
        const Divider(height: 1),
        Expanded(child: _buildBody()),
      ],
    );
  }

  Widget _buildBody() {
    if (_loading && _columns.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null && _columns.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(
              'kanban error: $_error',
              style: const TextStyle(color: Colors.red, fontSize: 12),
            ),
            const SizedBox(height: 8),
            FilledButton.tonal(
              onPressed: _refresh,
              child: const Text('retry', style: TextStyle(fontSize: 12)),
            ),
          ],
        ),
      );
    }
    if (_columns.isEmpty) {
      return const Center(
        child: Text(
          'no columns yet',
          style: TextStyle(color: Colors.white54, fontSize: 12),
        ),
      );
    }
    return ListView.builder(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.all(8),
      itemCount: _columns.length,
      itemBuilder: (context, index) => _buildColumn(_columns[index]),
    );
  }

  Widget _buildColumn(KanbanColumn column) {
    final cards = _cardsIn(column.id);
    final moveTargets = _columns.where((c) => c.id != column.id).toList();
    return SizedBox(
      width: 300,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              _legend(column),
              style: const TextStyle(
                color: Colors.orange,
                fontSize: 13,
                fontWeight: FontWeight.bold,
              ),
            ),
            const SizedBox(height: 6),
            Expanded(
              child: cards.isEmpty
                  ? const Center(
                      child: Text(
                        'no cards',
                        style: TextStyle(color: Colors.white38, fontSize: 12),
                      ),
                    )
                  : ListView.builder(
                      key: ValueKey('kanban-column-${column.id}'),
                      itemCount: cards.length,
                      itemBuilder: (context, index) => KanbanCardTile(
                        card: cards[index],
                        moveTargets: moveTargets,
                        onMove: (targetId) =>
                            _moveCard(cards[index].id, targetId),
                        onOpenDetail: () => _openDetail(cards[index]),
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Header row: lowercase title, optional board selector, refresh button,
/// and the event-link note (linkError frames only — the stream otherwise
/// stays invisible when healthy).
class _Header extends StatelessWidget {
  const _Header({
    required this.boards,
    required this.selectedBoardId,
    required this.linkNote,
    required this.loading,
    required this.onRefresh,
    required this.onSelectBoard,
  });

  final List<KanbanBoard> boards;
  final String? selectedBoardId;
  final String? linkNote;
  final bool loading;
  final VoidCallback onRefresh;
  final void Function(String boardId) onSelectBoard;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 12, 4),
      child: Row(
        children: [
          const Text(
            'kanban board',
            style: TextStyle(
              color: Colors.orange,
              fontSize: 16,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(width: 12),
          if (boards.length > 1 && selectedBoardId != null)
            DropdownButton<String>(
              key: const ValueKey('kanban-board-selector'),
              value: selectedBoardId,
              dropdownColor: const Color(0xFF24242A),
              style: const TextStyle(color: Colors.white70, fontSize: 12),
              items: [
                for (final b in boards)
                  DropdownMenuItem(
                    value: b.id,
                    child: Text(
                      b.name.toLowerCase(),
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
              ],
              onChanged: loading
                  ? null
                  : (String? id) {
                      if (id != null) onSelectBoard(id);
                    },
            ),
          if (linkNote != null) ...[
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                linkNote!.toLowerCase(),
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(color: Colors.amber, fontSize: 11),
              ),
            ),
          ] else
            const Spacer(),
          IconButton(
            key: const ValueKey('kanban-refresh'),
            tooltip: 'refresh',
            iconSize: 16,
            color: Colors.white70,
            onPressed: loading ? null : onRefresh,
            icon: loading
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.refresh),
          ),
        ],
      ),
    );
  }
}

/// Humanized lease age for the C10 snackbar (lowercase, short form):
/// "45 s" / "12 m" / "3 h 20 m" / "2 d". Null age reads "age unknown".
String humanizeLeaseAge(Duration? age) {
  if (age == null) return 'age unknown';
  final inSeconds = age.inSeconds;
  if (inSeconds < 60) return '${inSeconds}s';
  if (age.inMinutes < 60) return '${age.inMinutes}m';
  if (age.inHours < 24) return '${age.inHours}h ${age.inMinutes % 60}m';
  return '${age.inDays}d';
}
