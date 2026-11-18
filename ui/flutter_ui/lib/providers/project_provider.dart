import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/api_models.dart' show Project, Session;
import '../services/sdk_client.dart';
import 'providers.dart';

/// Mirrors TUI ProjectInfoUpdatedMsg (internal/tui/app.go:559-565).
///
/// Lightweight value type representing the currently-active project on
/// the daemon side. Empty/idle state is represented by
/// [CurrentProject.empty] (with `isActive == false`).
class CurrentProject {
  final String id;
  final String name;
  final String mode; // "git" | "local" | ""
  final String branch; // git only
  final bool dirty; // git only
  final String localPath; // project directory path

  const CurrentProject({
    required this.id,
    required this.name,
    required this.mode,
    required this.branch,
    required this.dirty,
    this.localPath = '',
  });

  static const empty = CurrentProject(
    id: '',
    name: '',
    mode: '',
    branch: '',
    dirty: false,
    localPath: '',
  );

  bool get isActive => id.isNotEmpty;
}

/// Currently-active project. Loaded on app connect and re-loaded on
/// project switch. Matches TUI app.go:530-554 logic: find entry with
/// status=="active", fetch status for git projects to get branch + dirty.
///
/// The dirty-flag fetch is best-effort: if it fails, the notifier
/// degrades to name-only rather than throwing.
class CurrentProjectNotifier extends StateNotifier<CurrentProject> {
  final SdkApiClient _client;

  CurrentProjectNotifier(this._client) : super(CurrentProject.empty);

  /// Re-fetch the active project from the daemon. Any unexpected error
  /// resets state to [CurrentProject.empty] so consumers render the
  /// "no project" affordance rather than a stale value.
  Future<void> refresh() async {
    try {
      final projects = await _client.listProjects();
      final active = projects.firstWhere(
        (p) => p['status'] == 'active',
        orElse: () => const <String, dynamic>{},
      );
      if (active.isEmpty) {
        state = CurrentProject.empty;
        return;
      }

      // Delegate parsing to the typed Project model so defaults match
      // other surfaces (branches_panel, resolveActiveProjectProvider, etc.):
      //   - name defaults to id when omitted
      //   - mode defaults to 'git' when omitted
      // Hand-rolled casts here previously diverged on both fields.
      final project = Project.fromJson(active);
      final id = project.id;
      final name = project.name;
      final mode = project.mode;

      String branch = '';
      bool dirty = false;
      if (mode == 'git' && id.isNotEmpty) {
        try {
          final status = await _client.getProjectStatus(id);
          branch = status['branch'] as String? ?? '';
          dirty = status['dirty'] as bool? ?? false;
        } catch (e) {
          // Status fetch is best-effort; indicator degrades to name-only.
          debugPrint('[warn] currentProjectProvider status fetch: $e');
        }
      }
      final localPathVal = (active['local_path'] as String?) ?? '';
      state = CurrentProject(
        id: id,
        name: name,
        mode: mode,
        branch: branch,
        dirty: dirty,
        localPath: localPathVal,
      );
    } catch (e) {
      debugPrint('[warn] currentProjectProvider refresh: $e');
      state = CurrentProject.empty;
    }
  }
}

/// Resolves the currently-active project from the daemon.
///
/// Depends on [sdkClientProvider] for transport. Consumers should call
/// `ref.read(currentProjectProvider.notifier).refresh()` after a
/// successful daemon connect and on any project-switch signal.
final currentProjectProvider =
    StateNotifierProvider<CurrentProjectNotifier, CurrentProject>((ref) {
      return CurrentProjectNotifier(ref.watch(sdkClientProvider));
    });

/// Resolves the project bound to a specific [Session] — never the
/// globally-active project.
///
/// Session naming must describe the session's OWN codebase. Reading the
/// globally-active project (the first `status == "active"` entry from
/// [CurrentProjectNotifier.refresh]) names every session after whichever
/// benchmark project happens to sort first (e.g. `bob-work-1`) regardless
/// of the directory the session is actually bound to.
///
/// Resolution order, all scoped to the passed session:
///   1. `session.projectId` → exact `id` match in the project list.
///   2. `session.projectPath` → exact `local_path` match (normalized by
///      stripping a trailing separator), for sessions bound by path before
///      the daemon recorded a project id.
///
/// Returns an empty [CurrentProject] when the session is unbound or the
/// bound project cannot be found, so callers can fall back to no project
/// context rather than a wrong one.
///
/// A pure top-level helper (no widget tree required) so both the naming
/// flow in `chat_input.dart` and unit tests exercise identical logic.
Future<CurrentProject> resolveSessionProject(
  SdkApiClient client,
  Session session,
) async {
  final projects = await client.listProjects();

  Map<String, dynamic>? match;
  final pid = session.projectId;
  if (pid != null && pid.isNotEmpty) {
    for (final p in projects) {
      if (p['id'] == pid) {
        match = p;
        break;
      }
    }
  }

  // Fall back to the session's bound path when there is no id match.
  if (match == null) {
    final ppath = session.projectPath;
    if (ppath != null && ppath.isNotEmpty) {
      final normalized = ppath.endsWith('/')
          ? ppath.substring(0, ppath.length - 1)
          : ppath;
      for (final p in projects) {
        final localPath = p['local_path'] as String? ?? '';
        final candidate = localPath.endsWith('/')
            ? localPath.substring(0, localPath.length - 1)
            : localPath;
        if (candidate.isNotEmpty && candidate == normalized) {
          match = p;
          break;
        }
      }
    }
  }

  if (match == null) return CurrentProject.empty;
  return CurrentProject(
    id: (match['id'] as String?) ?? '',
    name: (match['name'] as String?) ?? (match['id'] as String?) ?? '',
    mode: (match['mode'] as String?) ?? 'git',
    branch: '',
    dirty: false,
    localPath: (match['local_path'] as String?) ?? '',
  );
}

/// Session-scoped project status provider.
///
/// A [FutureProvider.family] keyed by projectId. Fetches the project's
/// metadata (name, mode, localPath) from [listProjects] and its live git
/// state (branch, dirty) from [getProjectStatus], returning a populated
/// [CurrentProject] — the same model used by [currentProjectProvider] — so
/// consumers can treat both surfaces uniformly.
///
/// Errors degrade gracefully to [CurrentProject.empty] so the status bar
/// renders its "no project" affordance rather than throwing.
final projectStatusProvider = FutureProvider.family<CurrentProject, String>((
  ref,
  projectId,
) async {
  final client = ref.watch(sdkClientProvider);
  try {
    // Fetch project details (name, mode, localPath) from the projects list.
    final projects = await client.listProjects();
    final match = projects.firstWhere(
      (p) => p['id'] == projectId,
      orElse: () => const <String, dynamic>{},
    );
    if (match.isEmpty) {
      // Project not found — possibly deleted or stale session binding.
      return CurrentProject.empty;
    }

    final project = Project.fromJson(match);
    String branch = '';
    bool dirty = false;
    if (project.mode == 'git') {
      try {
        final status = await client.getProjectStatus(projectId);
        branch = status['branch'] as String? ?? '';
        dirty = status['dirty'] as bool? ?? false;
      } catch (e) {
        // Best-effort: branch/dirty are optional indicators.
        debugPrint('[warn] projectStatusProvider($projectId) status fetch: $e');
      }
    }

    return CurrentProject(
      id: project.id,
      name: project.name,
      mode: project.mode,
      branch: branch,
      dirty: dirty,
      localPath: project.localPath,
    );
  } catch (e) {
    debugPrint('[warn] projectStatusProvider($projectId): $e');
    return CurrentProject.empty;
  }
});
