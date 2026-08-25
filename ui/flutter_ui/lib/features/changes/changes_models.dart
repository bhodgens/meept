/// Data models for the pending-changes / changes-journal surfaces.
///
/// Shapes mirror the daemon's HTTP contract (leaves 05/06):
///
/// Pending change (`GET /api/v1/sessions/{sid}/pending-changes` entries):
/// ```json
/// {
///   "id": "chg-1",
///   "file_path": "src/main.go",
///   "diff": "@@ -1,3 +1,4 @@ ...",
///   "created_at": "2026-08-25T10:00:00Z",
///   "expires_at": "2026-08-25T10:30:00Z"
/// }
/// ```
///
/// Journal entry (`GET /api/v1/changes/journal` entries — pre-image bytes
/// never leave the server):
/// ```json
/// {
///   "id": "j-1",
///   "session_id": "session-abc",
///   "file_path": "src/main.go",
///   "post_sha": "abc123...",
///   "applied_at": "2026-08-25T10:05:00Z",
///   "change_ids": ["chg-1"],
///   "pre_image_size": 1024
/// }
/// ```
class PendingChange {
  final String id;
  final String filePath;
  final String diff;
  final DateTime? createdAt;
  final DateTime? expiresAt;

  const PendingChange({
    required this.id,
    required this.filePath,
    required this.diff,
    this.createdAt,
    this.expiresAt,
  });

  factory PendingChange.fromJson(Map<String, dynamic> json) {
    return PendingChange(
      id: json['id'] as String? ?? '',
      filePath: json['file_path'] as String? ?? '',
      diff: json['diff'] as String? ?? '',
      createdAt: _parseTime(json['created_at']),
      expiresAt: _parseTime(json['expires_at']),
    );
  }
}

/// A single applied-change record from the journal.
class ChangeJournalEntry {
  final String id;
  final String sessionId;
  final String filePath;
  final String postSha;
  final DateTime? appliedAt;
  final List<String> changeIds;
  final int preImageSize;

  const ChangeJournalEntry({
    required this.id,
    required this.sessionId,
    required this.filePath,
    required this.postSha,
    this.appliedAt,
    this.changeIds = const [],
    this.preImageSize = 0,
  });

  factory ChangeJournalEntry.fromJson(Map<String, dynamic> json) {
    final idsRaw = json['change_ids'];
    final ids = <String>[];
    if (idsRaw is List) {
      for (final v in idsRaw) {
        if (v is String) ids.add(v);
      }
    }
    return ChangeJournalEntry(
      id: json['id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      filePath: json['file_path'] as String? ?? '',
      postSha: json['post_sha'] as String? ?? '',
      appliedAt: _parseTime(json['applied_at']),
      changeIds: ids,
      preImageSize: (json['pre_image_size'] as num?)?.toInt() ?? 0,
    );
  }
}

DateTime? _parseTime(dynamic raw) {
  if (raw is String && raw.isNotEmpty) {
    return DateTime.tryParse(raw);
  }
  return null;
}
