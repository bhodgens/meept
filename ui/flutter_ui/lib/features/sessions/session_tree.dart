import 'package:meept_ui/models/api_models.dart';

/// Bucket name assigned to sessions that are not bound to any project.
///
/// Mirrors the Go TUI's C2 constant so the bucket names match on both surfaces.
const String unboundBucket = 'unbound';

/// C2 grouping helper (shared algorithm, implemented twice).
///
/// ```
/// bucketFor(session) = session.projectId if non-empty,
///                      else session.projectPath if non-empty,
///                      else "unbound"
/// ```
///
/// Go counterpart: `internal/tui/models/sessions_tree.go`.
/// Dart counterpart: this file. Bucket NAMES must match across surfaces.
String bucketFor(Session session) {
  final projectId = session.projectId;
  if (projectId != null && projectId.isNotEmpty) return projectId;

  final projectPath = session.projectPath;
  if (projectPath != null && projectPath.isNotEmpty) return projectPath;

  return unboundBucket;
}

/// Group [sessions] into ordered C2 buckets.
///
/// Insertion order is preserved both for buckets (first session seen wins)
/// and for the sessions inside each bucket.
Map<String, List<Session>> groupSessionsByBucket(List<Session> sessions) {
  final groups = <String, List<Session>>{};
  for (final session in sessions) {
    groups.putIfAbsent(bucketFor(session), () => <Session>[]).add(session);
  }
  return groups;
}
