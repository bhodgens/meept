// Kanban wire models (plan tree: kanban-board 05/04 'flutter-bridge').
//
// These mirror the Go types in internal/kanban (contract C1) exactly as the
// sidecar gateway forwards them: `internal/kanbansvc/gateway.go` passes the
// daemon RPC reply bytes through untouched, so the JSON keys are whatever
// Go's encoding/json produces for the kanban structs. Only `StateEvent`,
// `CardEvent`-adjacent payloads, and `BoardChanged` carry explicit json
// tags (snake_case: kind/id/from/to/reason/ts, board_id/note); `Board`,
// `Column`, `Card`, `WorkRef`, `CardMoved`, `CardEvent` and `ReconcileStats`
// have NO tags in Go, so today's wire shape is Go-default PascalCase
// ("ID", "BoardID", ...). Every [fromJson] below accepts BOTH spellings —
// the snake_case canonical name from C1/C3 first, then the Go default the
// gateway emits until leaf 05/01 pins the handler envelopes. The 05/01
// handler author must not rename any key that already exists here.
//
// No dart:io: this file is shared web + desktop code.

/// Reads [snake] then the Go-default PascalCase fallback from [json].
/// Kept private to this file; models are the only wire readers.
Object? _pick(Map<String, dynamic> json, String snake, String pascal) =>
    json.containsKey(snake) ? json[snake] : json[pascal];

/// Loose map view of a decoded JSON value; null when it is not an object.
Map<String, dynamic>? _asMap(Object? value) =>
    value is Map ? Map<String, dynamic>.from(value) : null;

/// Loose list view; null when the value is not a list.
List<Object?>? _asList(Object? value) => value is List ? value : null;

String _str(Map<String, dynamic> j, String snake, String pascal) {
  final v = _pick(j, snake, pascal);
  return v is String ? v : '';
}

int _int(Map<String, dynamic> j, String snake, String pascal) {
  final v = _pick(j, snake, pascal);
  if (v is int) return v;
  if (v is num) return v.toInt();
  if (v is String) return int.tryParse(v) ?? 0;
  return 0;
}

bool _bool(Map<String, dynamic> j, String snake, String pascal) {
  final v = _pick(j, snake, pascal);
  if (v is bool) return v;
  return false;
}

DateTime? _time(Map<String, dynamic> j, String snake, String pascal) {
  final v = _pick(j, snake, pascal);
  if (v is String) return DateTime.tryParse(v);
  return null;
}

List<String> _strList(Map<String, dynamic> j, String snake, String pascal) {
  final v = _pick(j, snake, pascal);
  final items = _asList(v);
  if (items == null) return const [];
  return items.whereType<String>().toList(growable: false);
}

/// Work-item pointer for a card (C1 WorkRef). Kind: step|goal|job|ticket|plan.
class KanbanWorkRef {
  const KanbanWorkRef({this.kind = '', this.id = ''});

  final String kind;
  final String id;

  factory KanbanWorkRef.fromJson(Map<String, dynamic> json) => KanbanWorkRef(
    kind: _str(json, 'kind', 'Kind'),
    id: _str(json, 'id', 'ID'),
  );

  Map<String, dynamic> toJson() => {
    'kind': kind,
    'id': id,
    // Go-default mirror for a body the daemon may re-marshal.
    'Kind': kind,
    'ID': id,
  };

  bool get isEmpty => kind.isEmpty && id.isEmpty;

  @override
  String toString() => 'KanbanWorkRef($kind/$id)';
}

/// A board (C1 Board).
class KanbanBoard {
  const KanbanBoard({required this.id, required this.name});

  final String id;
  final String name;

  factory KanbanBoard.fromJson(Map<String, dynamic> json) =>
      KanbanBoard(id: _str(json, 'id', 'ID'), name: _str(json, 'name', 'Name'));

  Map<String, dynamic> toJson() => {'id': id, 'name': name};

  @override
  String toString() => 'KanbanBoard($id, $name)';
}

/// A column within a board (C1 Column). [wip] 0 means no limit.
class KanbanColumn {
  const KanbanColumn({
    required this.id,
    required this.boardId,
    required this.name,
    required this.position,
    required this.wip,
  });

  final String id;
  final String boardId;
  final String name;
  final int position;
  final int wip;

  factory KanbanColumn.fromJson(Map<String, dynamic> json) => KanbanColumn(
    id: _str(json, 'id', 'ID'),
    boardId: _str(json, 'board_id', 'BoardID'),
    name: _str(json, 'name', 'Name'),
    position: _int(json, 'position', 'Position'),
    wip: _int(json, 'wip', 'WIP'),
  );

  Map<String, dynamic> toJson() => {
    'id': id,
    'board_id': boardId,
    'name': name,
    'position': position,
    'wip': wip,
  };

  @override
  String toString() => 'KanbanColumn($id, $name, pos=$position, wip=$wip)';
}

/// A board card (C1 Card) with provenance fields (goal/employee/plan/
/// session/trigger — may all be empty for manually created cards).
class KanbanCard {
  const KanbanCard({
    required this.id,
    required this.boardId,
    required this.columnId,
    required this.title,
    this.ref = const KanbanWorkRef(),
    this.labels = const [],
    this.goalId = '',
    this.employeeId = '',
    this.planId = '',
    this.sessionId = '',
    this.triggerRef = '',
    this.order = 0,
  });

  final String id;
  final String boardId;
  final String columnId;
  final String title;
  final KanbanWorkRef ref;
  final List<String> labels;

  /// Provenance (C1): derived by the projector from the source work-item row.
  final String goalId;
  final String employeeId;
  final String planId;
  final String sessionId;
  final String triggerRef;

  /// Manual ordering within the column.
  final int order;

  factory KanbanCard.fromJson(Map<String, dynamic> json) {
    final rawRef = _asMap(_pick(json, 'ref', 'Ref'));
    return KanbanCard(
      id: _str(json, 'id', 'ID'),
      boardId: _str(json, 'board_id', 'BoardID'),
      columnId: _str(json, 'column_id', 'ColumnID'),
      title: _str(json, 'title', 'Title'),
      ref: rawRef == null
          ? const KanbanWorkRef()
          : KanbanWorkRef.fromJson(rawRef),
      labels: _strList(json, 'labels', 'Labels'),
      goalId: _str(json, 'goal_id', 'GoalID'),
      employeeId: _str(json, 'employee_id', 'EmployeeID'),
      planId: _str(json, 'plan_id', 'PlanID'),
      sessionId: _str(json, 'session_id', 'SessionID'),
      triggerRef: _str(json, 'trigger_ref', 'TriggerRef'),
      order: _int(json, 'order', 'Order'),
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'board_id': boardId,
    'column_id': columnId,
    'title': title,
    'ref': {'kind': ref.kind, 'id': ref.id},
    'labels': labels,
    'goal_id': goalId,
    'employee_id': employeeId,
    'plan_id': planId,
    'session_id': sessionId,
    'trigger_ref': triggerRef,
    'order': order,
  };

  @override
  String toString() => 'KanbanCard($id, "$title", col=$columnId)';
}

/// Work-item state delta (C1 StateEvent). This is the ONE struct with json
/// tags in Go (kind/id/from/to/reason/ts — all-lowercase, verbatim).
class KanbanStateEvent {
  const KanbanStateEvent({
    required this.kind,
    required this.id,
    required this.from,
    required this.to,
    required this.reason,
    this.ts,
  });

  final String kind;
  final String id;
  final String from;
  final String to;
  final String reason;
  final DateTime? ts;

  factory KanbanStateEvent.fromJson(Map<String, dynamic> json) =>
      KanbanStateEvent(
        kind: _str(json, 'kind', 'Kind'),
        id: _str(json, 'id', 'ID'),
        from: _str(json, 'from', 'From'),
        to: _str(json, 'to', 'To'),
        reason: _str(json, 'reason', 'Reason'),
        ts: _time(json, 'ts', 'TS'),
      );

  Map<String, dynamic> toJson() => {
    'kind': kind,
    'id': id,
    'from': from,
    'to': to,
    'reason': reason,
    'ts': ts?.toIso8601String(),
  };

  @override
  String toString() => 'KanbanStateEvent($kind/$id $from->$to)';
}

/// A completed or refused human card move (C1 CardMoved).
class KanbanCardMoved {
  const KanbanCardMoved({
    required this.cardId,
    required this.fromColumnId,
    required this.toColumnId,
    required this.actor,
    required this.leaseBlocked,
  });

  final String cardId;
  final String fromColumnId;
  final String toColumnId;
  final String actor;

  /// True when the move was refused under lease rule A (C10) but still
  /// reported on the bus.
  final bool leaseBlocked;

  factory KanbanCardMoved.fromJson(Map<String, dynamic> json) =>
      KanbanCardMoved(
        cardId: _str(json, 'card_id', 'CardID'),
        fromColumnId: _str(json, 'from_column_id', 'FromColumnID'),
        toColumnId: _str(json, 'to_column_id', 'ToColumnID'),
        actor: _str(json, 'actor', 'Actor'),
        leaseBlocked: _bool(json, 'lease_blocked', 'LeaseBlocked'),
      );

  Map<String, dynamic> toJson() => {
    'card_id': cardId,
    'from_column_id': fromColumnId,
    'to_column_id': toColumnId,
    'actor': actor,
    'lease_blocked': leaseBlocked,
  };

  @override
  String toString() =>
      'KanbanCardMoved($cardId $fromColumnId->$toColumnId by $actor)';
}

/// Append-only timeline row: card comment or timeline entry (C3 table
/// kanban_card_events: card_id, kind, actor, body, created_at). Go's
/// kanban.CardEvent struct has no json tags, so both spellings are read.
class KanbanCardEvent {
  const KanbanCardEvent({
    required this.id,
    required this.cardId,
    required this.kind,
    required this.actor,
    required this.body,
    this.createdAt,
  });

  final String id;
  final String cardId;

  /// 'comment' | other timeline kinds.
  final String kind;
  final String actor;
  final String body;
  final DateTime? createdAt;

  factory KanbanCardEvent.fromJson(Map<String, dynamic> json) =>
      KanbanCardEvent(
        id: _str(json, 'id', 'ID'),
        cardId: _str(json, 'card_id', 'CardID'),
        kind: _str(json, 'kind', 'Kind'),
        actor: _str(json, 'actor', 'Actor'),
        body: _str(json, 'body', 'Body'),
        createdAt: _time(json, 'created_at', 'CreatedAt'),
      );

  Map<String, dynamic> toJson() => {
    'id': id,
    'card_id': cardId,
    'kind': kind,
    'actor': actor,
    'body': body,
    'created_at': createdAt?.toIso8601String(),
  };

  @override
  String toString() => 'KanbanCardEvent($kind by $actor on $cardId)';
}

/// Board-change broadcast payload (C2 kanban.board.changed; Go struct with
/// json tags board_id + note).
class KanbanBoardChanged {
  const KanbanBoardChanged({required this.boardId, this.note = ''});

  final String boardId;

  /// Optional human note (e.g. the wip_mode:"warn" move note, C10).
  final String note;

  factory KanbanBoardChanged.fromJson(Map<String, dynamic> json) =>
      KanbanBoardChanged(
        boardId: _str(json, 'board_id', 'BoardID'),
        note: _str(json, 'note', 'Note'),
      );

  Map<String, dynamic> toJson() => {'board_id': boardId, 'note': note};

  @override
  String toString() => 'KanbanBoardChanged($boardId, note="$note")';
}

/// Result of a kanban.reconcile.now pass (Go kanban.ReconcileStats).
class KanbanReconcileStats {
  const KanbanReconcileStats({
    this.created = 0,
    this.moved = 0,
    this.fixed = 0,
    this.deleted = 0,
  });

  final int created;
  final int moved;
  final int fixed;
  final int deleted;

  int get total => created + moved + fixed + deleted;

  factory KanbanReconcileStats.fromJson(Map<String, dynamic> json) =>
      KanbanReconcileStats(
        created: _int(json, 'created', 'Created'),
        moved: _int(json, 'moved', 'Moved'),
        fixed: _int(json, 'fixed', 'Fixed'),
        deleted: _int(json, 'deleted', 'Deleted'),
      );

  Map<String, dynamic> toJson() => {
    'created': created,
    'moved': moved,
    'fixed': fixed,
    'deleted': deleted,
  };

  @override
  String toString() =>
      'KanbanReconcileStats(+$created ~$moved !$fixed x$deleted)';
}

/// One frame from the board-events stream.
///
/// INTERIM (master.md C8): the WSBoard wire class (`board_event`) does not
/// exist yet — it lands in leaf 05/07. Until then board deltas ride the
/// PROGRESS class, exactly like the daemon's own agent_progress frames, so
/// this placeholder carries the raw decoded frame and reports [isProgress].
/// Consumers (leaf 05/05) MUST NOT depend on WSBoard-typed payload keys;
/// read [payload] defensively. When 05/07 ships, this class gains the
/// typed board_event variants and [isProgress] narrows accordingly.
class KanbanBoardEvent {
  const KanbanBoardEvent(this.payload, {this.linkError});

  /// Raw decoded relay frame (may be empty for link-level notifications).
  final Map<String, dynamic> payload;

  /// Set only for non-frame notifications the stream surfaces (e.g. the
  /// connection dropping) so listeners can react without error channels.
  final String? linkError;

  /// The relay wire type. Absent or non-'chat_message' frames are progress
  /// class by contract (only chat_message topics classify chat; C8).
  String get wireType =>
      payload['type'] is String ? payload['type'] as String : _progressWireType;

  static const String _progressWireType = 'agent_progress';

  /// True while the C8 interim rule holds: everything on this stream is
  /// progress-class, never chat.
  bool get isProgress => wireType == _progressWireType;

  /// Board the delta concerns, when the frame names one (kanban.board.changed
  /// and card payloads carry board_id).
  String? get boardId {
    final v = payload['board_id'] ?? payload['BoardID'];
    return v is String && v.isNotEmpty ? v : null;
  }

  @override
  String toString() => 'KanbanBoardEvent($wireType, board=$boardId)';
}
