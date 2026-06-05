import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/providers/async_state.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/services/api_client.dart';

// ===== Stub classes =====

class _StubApiClient extends ApiClient {
  _StubApiClient() : super(host: 'localhost', port: 8081);

  final List<String> recordedPaths = [];

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    recordedPaths.add(path);

    if (path == '/config/agents') {
      return _mockAgentsResponse as T;
    }
    return {} as T;
  }

  @override
  Future<T> post<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

/// An API client that throws on any GET, to simulate failure.
class _FailingApiClient extends ApiClient {
  _FailingApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    throw Exception('network failure');
  }

  @override
  Future<T> post<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

/// An API client that returns an empty agents list.
class _EmptyAgentsApiClient extends ApiClient {
  _EmptyAgentsApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    if (path == '/config/agents') {
      return {'agents': <dynamic>[]}.cast<String, dynamic>() as T;
    }
    return {} as T;
  }

  @override
  Future<T> post<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

/// An API client that returns one agent with null id, to verify error handling.
class _NullIdAgentApiClient extends ApiClient {
  _NullIdAgentApiClient() : super(host: 'localhost', port: 8081);

  @override
  Future<T> get<T>(String path, {Map<String, dynamic>? queryParameters}) async {
    if (path == '/config/agents') {
      return {
        'agents': [
          <String, dynamic>{'id': null, 'name': 'No ID'},
        ],
      }.cast<String, dynamic>() as T;
    }
    return {} as T;
  }

  @override
  Future<T> post<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> put<T>(String path, {dynamic data, Map<String, dynamic>? queryParameters}) async {
    return {} as T;
  }

  @override
  Future<T> delete<T>(String path) async {
    return {} as T;
  }
}

const _mockAgentsResponse = {
  'agents': [
    {
      'id': 'dispatcher',
      'name': 'Dispatcher',
      'description': 'Routes tasks',
      'enabled': true,
    },
    {
      'id': 'coder',
      'name': 'Coder',
      'description': 'Writes code',
      'enabled': true,
      'prompt': 'You are a coding assistant.',
      'frontmatter': {'version': '1.0'},
    },
    {
      'id': 'analyst',
      'name': 'Analyst',
      'description': '', // empty description tests fallback
      'enabled': false,
    },
  ],
};

void main() {
  group('AgentNotifier initial state', () {
    test('starts as initial', () {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      expect(notifier.state.whenOrNull(initial: () => true), isTrue);
    });
  });

  group('AgentNotifier.loadAgents()', () {
    test('loadAgents transitions to loading then data on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      expect(notifier.state.whenOrNull(initial: () => true), isTrue);

      notifier.loadAgents();
      expect(notifier.state.whenOrNull(loading: () => true), isTrue);

      await Future.delayed(const Duration(milliseconds: 100));
      final agents = notifier.state.whenOrNull(data: (a) => a);
      expect(agents, isNotNull);
      expect(agents!.length, 3);
    });

    test('loadAgents populates agents list on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      await notifier.loadAgents();

      final agents = notifier.state.whenOrNull(data: (a) => a);
      expect(agents, isNotNull);
      expect(agents![0].id, 'dispatcher');
      expect(agents[1].name, 'Coder');
      expect(agents[2].enabled, isFalse);
    });

    test('loadAgents clears error on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      await notifier.loadAgents();

      expect(notifier.state.whenOrNull(error: (_, __) => true), isNull);
    });

    test('loadAgents sets error on failure', () async {
      final notifier = AgentNotifier(apiClient: _FailingApiClient());
      await notifier.loadAgents();

      final isLoading = notifier.state.whenOrNull(loading: () => true);
      final error = notifier.state.whenOrNull(error: (e, _) => e.toString());
      expect(isLoading ?? false, isFalse);
      expect(error, isNotNull);
      expect(error!, contains('network failure'));
    });

    test('loadAgents sets error message from exception', () async {
      final client = _FailingApiClient();
      final notifier = AgentNotifier(apiClient: client);
      await notifier.loadAgents();

      final error = notifier.state.whenOrNull(error: (e, _) => e.toString());
      expect(error, isNotNull);
      expect(error!, contains('Exception'));
    });

    // ===== Edge cases =====

    test('loadAgents with empty agents list from API', () async {
      final client = _EmptyAgentsApiClient();
      final notifier = AgentNotifier(apiClient: client);
      await notifier.loadAgents();

      expect(notifier.state.whenOrNull(loading: () => true), isNull);
      expect(notifier.state.whenOrNull(error: (_, __) => true), isNull);
      expect(notifier.state.whenOrNull(data: (a) => a), isEmpty);
    });

    test('loadAgents populates correctly when API returns agents with null id',
        () async {
      final client = _NullIdAgentApiClient();
      final notifier = AgentNotifier(apiClient: client);
      await notifier.loadAgents();

      // Agent.fromJson does json['id'] as String which throws on null,
      // so the error should be set
      expect(notifier.state.whenOrNull(error: (_, __) => true), isNotNull);
      expect(notifier.state.whenOrNull(data: (a) => a), isNull);
    });
  });

  group('Agent model parsing', () {
    test('Agent.fromJson handles missing optional fields', () {
      final json = {
        'id': 'minimal',
        'name': 'Minimal',
        'description': null, // backend sends null
        'enabled': null,
      };

      final agent = Agent.fromJson(json);
      expect(agent.id, 'minimal');
      expect(agent.name, 'Minimal');
      expect(agent.description, ''); // fallback
      expect(agent.enabled, isTrue); // fallback
      expect(agent.prompt, isNull);
    });

    test('Agent.fromJson parses prompt when present', () {
      final json = {
        'id': 'a1',
        'name': 'Assistant',
        'description': 'Helps',
        'enabled': true,
        'prompt': 'You are helpful.',
      };
      final agent = Agent.fromJson(json);
      expect(agent.prompt, 'You are helpful.');
    });

    test('Agent.fromJson parses frontmatter when present', () {
      final json = {
        'id': 'a2',
        'name': 'Frontmatter Agent',
        'description': 'Has fm',
        'enabled': true,
        'frontmatter': {'version': '2.0', 'author': 'test'},
      };
      final agent = Agent.fromJson(json);
      expect(agent.frontmatter?['version'], '2.0');
    });
  });

  group('Agent toJson', () {
    test('toJson includes all fields', () {
      final agent = Agent(
        id: 'a1',
        name: 'Test',
        description: 'A test agent',
        enabled: true,
        prompt: 'Be nice.',
        frontmatter: {'key': 'val'},
      );

      final json = agent.toJson();
      expect(json['id'], 'a1');
      expect(json['name'], 'Test');
      expect(json['description'], 'A test agent');
      expect(json['enabled'], true);
      expect(json['prompt'], 'Be nice.');
      expect(json['frontmatter'], {'key': 'val'});
    });

    test('toJson omits null optional fields', () {
      final agent = Agent(
        id: 'a1',
        name: 'Test',
        description: 'No optional fields',
        enabled: false,
      );

      final json = agent.toJson();
      expect(json['prompt'], isNull);
      expect(json['frontmatter'], isNull);
    });
  });

  group('Agent equality', () {
    test('agents with same content are equal', () {
      final a1 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);
      final a2 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);

      expect(a1, equals(a2));
    });

    test('agents with different ids are not equal', () {
      final a1 = Agent(id: 'a1', name: 'A', description: 'desc', enabled: true);
      final a2 = Agent(id: 'a2', name: 'A', description: 'desc', enabled: true);

      expect(a1 != a2, isTrue);
    });

    test('agents with different enable flag are not equal', () {
      final a1 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);
      final a2 = Agent(id: 'a', name: 'A', description: 'desc', enabled: false);

      expect(a1 != a2, isTrue);
    });
  });
}
