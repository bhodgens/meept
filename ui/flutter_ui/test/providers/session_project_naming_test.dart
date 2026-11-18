import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/project_provider.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Regression tests for session auto-naming.
///
/// Bug: a new session was named from the FIRST globally-active project
/// (`currentProjectProvider`, which picks the first `status == "active"`
/// entry from `listProjects`). With several benchmark projects active and
/// `bob-work-1` sorting first, every session was named `bob-work-1`
/// regardless of the directory the session was actually bound to.
///
/// Fix: naming resolves the project bound to the session itself
/// ([resolveSessionProject]) by `project_id`, falling back to
/// `project_path`. These tests prove the resolver returns the session's
/// OWN project — never the first active project.

/// Stub that returns several active projects (bob-work-1 first, mirroring
/// the daemon ordering that caused the bug) plus the project the session is
/// actually bound to.
class _ProjectsSdkClient extends SdkApiClient {
  _ProjectsSdkClient(this.projects) : super(host: 'localhost', port: 8081);

  final List<Map<String, dynamic>> projects;

  @override
  Future<List<Map<String, dynamic>>> listProjects() async =>
      projects.map((p) => Map<String, dynamic>.from(p)).toList();
}

const _projects = [
  // First ACTIVE project — the stale buggy source of the session name.
  {
    'id': 'bob-1',
    'name': 'bob-work-1',
    'mode': 'local',
    'status': 'active',
    'local_path': '/tmp/bob-work-1',
  },
  {
    'id': 'alpha-2',
    'name': 'alpha-active',
    'mode': 'git',
    'status': 'active',
    'local_path': '/tmp/alpha-active',
  },
  // The project the session is actually bound to — NOT first, NOT active.
  {
    'id': 'meept-3',
    'name': 'meept',
    'mode': 'git',
    'status': 'idle',
    'local_path': '/Users/caimlas/git/meept',
  },
];

void main() {
  group('resolveSessionProject (session naming)', () {
    test(
      'uses session-bound project_id, NOT the first globally-active project',
      () async {
        final client = _ProjectsSdkClient(_projects);
        // Session bound to the meept project (third, idle), while bob-work-1
        // is the first active project.
        final session = Session(
          id: 'session-1944fbf1a1f3d4a4',
          title: 'new session',
          createdAt: DateTime(2026, 1, 1),
          projectId: 'meept-3',
          projectPath: '/Users/caimlas/git/meept',
        );

        final resolved = await resolveSessionProject(client, session);

        expect(
          resolved.name,
          'meept',
          reason: 'must name from the session-bound project, not bob-work-1',
        );
        expect(resolved.id, 'meept-3');
        expect(resolved.name, isNot('bob-work-1'));
      },
    );

    test(
      'falls back to session-bound project_path when project_id is absent',
      () async {
        final client = _ProjectsSdkClient(_projects);
        final session = Session(
          id: 's2',
          title: 'new session',
          createdAt: DateTime(2026, 1, 1),
          projectPath: '/Users/caimlas/git/meept',
        );

        final resolved = await resolveSessionProject(client, session);

        expect(resolved.name, 'meept');
        expect(resolved.id, 'meept-3');
      },
    );

    test('matches project_path with a trailing separator', () async {
      final client = _ProjectsSdkClient(_projects);
      final session = Session(
        id: 's3',
        title: 'new session',
        createdAt: DateTime(2026, 1, 1),
        projectPath: '/Users/caimlas/git/meept/',
      );

      final resolved = await resolveSessionProject(client, session);

      expect(resolved.name, 'meept');
    });

    test(
      'returns empty for an unbound session (never a global fallback)',
      () async {
        final client = _ProjectsSdkClient(_projects);
        final session = Session(
          id: 's4',
          title: 'new session',
          createdAt: DateTime(2026, 1, 1),
        );

        final resolved = await resolveSessionProject(client, session);

        // Must NOT fall back to bob-work-1 or any active project.
        expect(resolved.isActive, isFalse);
        expect(resolved.name, isEmpty);
      },
    );

    test(
      'returns empty when the bound project_id is no longer present',
      () async {
        final client = _ProjectsSdkClient(_projects);
        final session = Session(
          id: 's5',
          title: 'new session',
          createdAt: DateTime(2026, 1, 1),
          projectId: 'deleted-project',
          projectPath: '/does/not/exist',
        );

        final resolved = await resolveSessionProject(client, session);

        expect(resolved.isActive, isFalse);
        expect(resolved.name, isEmpty);
      },
    );
  });
}
