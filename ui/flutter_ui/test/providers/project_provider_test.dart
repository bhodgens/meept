import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:meept_ui/providers/project_provider.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/services/sdk_client.dart';

// ===== Stub SDK clients =====

/// Stub [SdkApiClient] that returns a fixed projects payload with one
/// active git project. Overrides only the two endpoint methods used by
/// [CurrentProjectNotifier.refresh].
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  final List<Map<String, dynamic>> _projects = const [
    {'id': 'p1', 'name': 'project-one', 'mode': 'git', 'status': 'active'},
    {'id': 'p2', 'name': 'project-two', 'mode': 'local', 'status': 'idle'},
  ];

  @override
  Future<List<Map<String, dynamic>>> listProjects() async {
    // Return deep copies so callers can't mutate our fixture.
    return _projects
        .map((p) => Map<String, dynamic>.from(p))
        .toList();
  }

  @override
  Future<Map<String, dynamic>> getProjectStatus(String projectId) async {
    return const {'branch': 'main', 'dirty': true};
  }
}

/// Stub that returns no active project.
class _NoActiveSdkClient extends SdkApiClient {
  _NoActiveSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listProjects() async {
    return [
      {'id': 'p2', 'name': 'project-two', 'mode': 'local', 'status': 'idle'},
    ];
  }
}

/// Stub that throws on listProjects, simulating a network failure.
class _FailingSdkClient extends SdkApiClient {
  _FailingSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listProjects() async {
    throw Exception('network failure');
  }
}

/// Stub where getProjectStatus throws, to verify best-effort degradation.
class _StatusFailsSdkClient extends SdkApiClient {
  _StatusFailsSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<List<Map<String, dynamic>>> listProjects() async {
    return [
      {'id': 'p1', 'name': 'project-one', 'mode': 'git', 'status': 'active'},
    ];
  }

  @override
  Future<Map<String, dynamic>> getProjectStatus(String projectId) async {
    throw Exception('status endpoint down');
  }
}

// ===== Tests =====

void main() {
  group('CurrentProject', () {
    test('empty has isActive == false', () {
      expect(CurrentProject.empty.isActive, isFalse);
      expect(CurrentProject.empty.id, isEmpty);
    });

    test('non-empty has isActive == true', () {
      const project = CurrentProject(
        id: 'x',
        name: 'name',
        mode: 'git',
        branch: 'main',
        dirty: false,
      );
      expect(project.isActive, isTrue);
    });
  });

  group('CurrentProjectNotifier.refresh', () {
    test('populates state from active git project with branch + dirty',
        () async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(_StubSdkClient()),
        ],
      );
      addTearDown(container.dispose);

      // Initial state is empty.
      expect(container.read(currentProjectProvider).isActive, isFalse);

      await container.read(currentProjectProvider.notifier).refresh();

      final state = container.read(currentProjectProvider);
      expect(state.isActive, isTrue);
      expect(state.id, 'p1');
      expect(state.name, 'project-one');
      expect(state.mode, 'git');
      expect(state.branch, 'main');
      expect(state.dirty, isTrue);
    });

    test('resets to empty when no active project is returned', () async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(_NoActiveSdkClient()),
        ],
      );
      addTearDown(container.dispose);

      await container.read(currentProjectProvider.notifier).refresh();

      expect(container.read(currentProjectProvider).isActive, isFalse);
    });

    test('resets to empty on network failure', () async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(_FailingSdkClient()),
        ],
      );
      addTearDown(container.dispose);

      await container.read(currentProjectProvider.notifier).refresh();

      expect(container.read(currentProjectProvider).isActive, isFalse);
    });

    test('degrades gracefully when getProjectStatus fails (best-effort)',
        () async {
      final container = ProviderContainer(
        overrides: [
          sdkClientProvider.overrideWithValue(_StatusFailsSdkClient()),
        ],
      );
      addTearDown(container.dispose);

      await container.read(currentProjectProvider.notifier).refresh();

      // Project still resolves; branch/dirty fall back to defaults.
      final state = container.read(currentProjectProvider);
      expect(state.isActive, isTrue);
      expect(state.id, 'p1');
      expect(state.branch, isEmpty);
      expect(state.dirty, isFalse);
    });
  });
}
