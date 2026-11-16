// Typed error mapping for the kanban bridge (leaf 05/04). Split from
// kanban_service.dart for the <700-line file rule; the service re-exports
// this so `import '.../kanban_service.dart'` keeps covering both.
//
// Mapping table mirrors internal/kanbansvc/gateway.go: authorize()
// (401/403), serveAPI() (400/404/405), writeRPCError() (404/502 with the
// "rpc" key). Domain errors that the daemon forwards inside an RPC error
// message are recognized by their kanban/types.go Error() phrasing and
// land on the structured subclasses.

/// Base for every failure the kanban bridge surfaces. [message] is the
/// gateway-provided text (already safe to show; all UI text lowercased by
/// the caller if rendered).
class KanbanApiException implements Exception {
  const KanbanApiException(this.message, {this.statusCode, this.body});

  final String message;
  final int? statusCode;
  final Map<String, dynamic>? body;

  @override
  String toString() => 'KanbanApiException($statusCode): $message';
}

/// 401 — no bearer token presented, or the token was never issued (C5).
class KanbanUnauthorizedException extends KanbanApiException {
  const KanbanUnauthorizedException(super.message, {super.body})
    : super(statusCode: 401);
}

/// 403 — known token missing a required grant; [grant] names the lane to
/// pair for (the gateway always names the first missing grant).
class KanbanForbiddenException extends KanbanApiException {
  const KanbanForbiddenException(super.message, {this.grant = '', super.body})
    : super(statusCode: 403);

  final String grant;
}

/// 400 — the gateway rejected the request before the daemon (bad body).
class KanbanBadRequestException extends KanbanApiException {
  const KanbanBadRequestException(super.message, {super.body})
    : super(statusCode: 400);
}

/// 404 — unknown card / unknown method / unknown path.
class KanbanNotFoundException extends KanbanApiException {
  const KanbanNotFoundException(super.message, {this.cardId = '', super.body})
    : super(statusCode: 404);

  final String cardId;
}

/// Lease rule A (C1/C10): a human move onto a projected target column was
/// refused because a live claim holds the card's work ref. The UI must show
/// [holder] and [age]; only card.takeover bypasses.
class KanbanLeaseHeldException extends KanbanApiException {
  const KanbanLeaseHeldException(
    super.message, {
    required this.holder,
    this.since,
    super.body,
  }) : super(statusCode: null);

  final String holder;
  final DateTime? since;

  /// Age of the claim at error time (mirrors kanban.LeaseAge).
  Duration? get age => since == null ? null : DateTime.now().difference(since!);
}

/// wip_mode:"block" refusal (C10): [columnId] is at its [limit].
class KanbanWipFullException extends KanbanApiException {
  const KanbanWipFullException(
    super.message, {
    required this.columnId,
    required this.limit,
    super.body,
  }) : super(statusCode: null);

  final String columnId;
  final int limit;
}

/// 502 — the sidecar reached the daemon and the RPC itself failed with a
/// message that maps to none of the domain errors above.
class KanbanUpstreamException extends KanbanApiException {
  const KanbanUpstreamException(super.message, {this.rpc = '', super.body})
    : super(statusCode: 502);

  final String rpc;
}

/// Transport-level failure (daemon/sidecar unreachable, timeout, bad TLS).
class KanbanConnectionException extends KanbanApiException {
  const KanbanConnectionException(super.message, {super.statusCode});
}
