/// One entry of the agent-offered permission options array on a requested
/// frame (acp-always-allow leaf 01, additive bus key `options`: raw
/// {optionId,name,kind} labels pass through untouched).
class AcpPermissionOption {
  final String optionId;
  final String name;
  final String kind;

  const AcpPermissionOption({
    required this.optionId,
    required this.name,
    required this.kind,
  });

  factory AcpPermissionOption.fromJson(Map<String, dynamic> json) {
    return AcpPermissionOption(
      optionId: json['optionId'] as String? ?? '',
      name: json['name'] as String? ?? '',
      kind: json['kind'] as String? ?? '',
    );
  }
}

/// One pending ACP human-relay permission request
/// (acp-human-permission-relay leaf 05, Contract E).
///
/// Frozen wire keys (bus.ACPPermissionEvent, internal/bus/acp_topics.go):
/// id, agent_id, session_id, summary, created_at, plus the additive options
/// key (agent-offered {optionId,name,kind} entries; absent/empty for agents
/// with no options array) — and decision/reason on the resolved twin, which
/// the client only uses to clear the entry by id.
class AcpPendingPermission {
  final String id;
  final String agentId;
  final String sessionId;
  final String summary;
  final String createdAt;
  final List<AcpPermissionOption> options;

  const AcpPendingPermission({
    required this.id,
    required this.agentId,
    required this.sessionId,
    required this.summary,
    required this.createdAt,
    this.options = const [],
  });

  factory AcpPendingPermission.fromJson(Map<String, dynamic> json) {
    final rawOptions = json['options'];
    final options = <AcpPermissionOption>[];
    if (rawOptions is List) {
      for (final entry in rawOptions) {
        if (entry is Map) {
          final opt = AcpPermissionOption.fromJson(
            Map<String, dynamic>.from(entry),
          );
          if (opt.optionId.isNotEmpty ||
              opt.name.isNotEmpty ||
              opt.kind.isNotEmpty) {
            options.add(opt);
          }
        }
      }
    }
    return AcpPendingPermission(
      id: json['id'] as String? ?? '',
      agentId: json['agent_id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      createdAt: json['created_at'] as String? ?? '',
      options: options,
    );
  }

  /// Whether the agent offered an allow_always-kind option — the
  /// agent-declared capability that gates the dialog's third "always"
  /// button. Matching mirrors internal/acp resolveAlwaysOptionID (the
  /// read side of the same contract): kind == 'allow_always' first (the
  /// ACP spec token), then a kind containing BOTH 'allow' and 'always'
  /// (case-insensitive).
  bool get hasAlwaysOption {
    for (final o in options) {
      if (o.kind == 'allow_always') return true;
    }
    for (final o in options) {
      final kind = o.kind.toLowerCase();
      if (kind.contains('allow') && kind.contains('always')) return true;
    }
    return false;
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
