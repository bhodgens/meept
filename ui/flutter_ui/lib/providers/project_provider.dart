import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/api_models.dart' show Project;
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

  const CurrentProject({
    required this.id,
    required this.name,
    required this.mode,
    required this.branch,
    required this.dirty,
  });

  static const empty =
      CurrentProject(id: '', name: '', mode: '', branch: '', dirty: false);

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
      state = CurrentProject(
        id: id,
        name: name,
        mode: mode,
        branch: branch,
        dirty: dirty,
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
