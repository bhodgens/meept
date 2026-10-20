/// One pending ACP human-relay permission request
/// (acp-human-permission-relay leaf 05, Contract E).
///
/// Frozen wire keys (bus.ACPPermissionEvent, internal/bus/acp_topics.go):
/// id, agent_id, session_id, summary, created_at (+ decision/reason on the
/// resolved twin, which the client only uses to clear the entry by id).
class AcpPendingPermission {
  final String id;
  final String agentId;
  final String sessionId;
  final String summary;
  final String createdAt;

  const AcpPendingPermission({
    required this.id,
    required this.agentId,
    required this.sessionId,
    required this.summary,
    required this.createdAt,
  });

  factory AcpPendingPermission.fromJson(Map<String, dynamic> json) {
    return AcpPendingPermission(
      id: json['id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      createdAt: json['created_at'] as String? ?? '',
    );
  }

  /// Short form of the request id for the dialog's "request <id>" line.
  String get shortId => id.length <= 8 ? id : id.substring(0, 8);

  /// Summary bounded for dialog display (never an unbounded blob).
  String get boundedSummary {
    const max = 240;
    if (summary.length <= max) return summary;
    return '${summary.substring(0, max)}...';
  }

  /// created_at (RFC3339 from the daemon) rendered locally; falls back to
  /// the raw string when it does not parse.
  String get createdDisplay {
    final parsed = DateTime.tryParse(createdAt);
    if (parsed == null) return createdAt;
    final local = parsed.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}:${two(local.second)}';
  }
}
