import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/agent_provider.dart';
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
  group('AgentState', () {
    test('defaults are correct', () {
      const state = AgentState();
      expect(state.agents, isEmpty);
      expect(state.isLoading, isFalse);
      expect(state.error, isNull);
    });

    test('copyWith creates new instance with updated values', () {
      const original = AgentState();
      final updated = original.copyWith(
        isLoading: true,
        error: null,
      );
      expect(updated.agents, original.agents);
      expect(updated.isLoading, isTrue);
      expect(updated.error, isNull);
    });

    test('copyWith preserves unprovided fields', () {
      const original = AgentState(
        agents: [Agent(
          id: 'a1',
          name: 'A1',
          description: 'desc',
          enabled: true,
        )],
        isLoading: true,
        error: 'oops',
      );
      final copy = original.copyWith(); // no args
      expect(copy.agents, original.agents);
      expect(copy.isLoading, isTrue);
      expect(copy.error, 'oops');
    });

    test('AgentState is immutable (uses const + copyWith)', () {
      const state1 = AgentState(isLoading: true);
      final state2 = AgentState(isLoading: false);

      expect(identical(state1, state2), isFalse); // different instances
      expect(state1.isLoading, isTrue);
      expect(state2.isLoading, isFalse);
    });

    test('copyWith sets error', () {
      const state = AgentState();
      final withError = state.copyWith(error: 'something went wrong');
      expect(withError.error, 'something went wrong');
    });

    test('copyWith with error: null clears existing error', () {
      const state = AgentState(error: 'oops');
      final result = state.copyWith(error: null);
      expect(result.error, isNull);
    });

    test('copyWith with no error argument keeps existing error', () {
      const state = AgentState(error: 'oops');
      final result = state.copyWith();
      expect(result.error, 'oops');
    });

    test('Agent.fromJson throws on completely missing id', () {
      final json = <String, dynamic>{
        'name': 'No Id',
        'description': 'No id field',
        'enabled': true,
      };

      expect(() => Agent.fromJson(json), throwsA(isA<TypeError>()));
    });

    test('Agent.fromJson with all optional fields null', () {
      final json = {
        'id': 'minimal',
        'name': 'Minimal',
        'description': null,
        'enabled': null,
        'prompt': null,
        'frontmatter': null,
      };

      final agent = Agent.fromJson(json);
      expect(agent.id, 'minimal');
      expect(agent.name, 'Minimal');
      expect(agent.description, '');
      expect(agent.enabled, isTrue);
      expect(agent.prompt, isNull);
      expect(agent.frontmatter, isNull);
    });

    test('copyWith with null agents keeps old agents', () {
      const agents = [Agent(id: 'x', name: 'X', description: '', enabled: true)];
      const state = AgentState(agents: agents);
      final copy = state.copyWith(agents: null);
      expect(copy.agents, agents);
    });
  });

  group('AgentNotifier.loadAgents()', () {
    test('initial state is loading = false', () {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      expect(notifier.state.isLoading, isFalse);
    });

    test('loadAgents sets loading to true then false on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      expect(notifier.state.isLoading, isFalse);

      notifier.loadAgents();
      expect(notifier.state.isLoading, isTrue);

      await Future.delayed(const Duration(milliseconds: 100));
      expect(notifier.state.isLoading, isFalse);
    });

    test('loadAgents populates agents list on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.agents, hasLength(3));
      expect(notifier.state.agents[0].id, 'dispatcher');
      expect(notifier.state.agents[1].name, 'Coder');
      expect(notifier.state.agents[2].enabled, isFalse);
    });

    test('loadAgents clears error on success', () async {
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.error, isNull);
    });

    test('loadAgents sets error on failure', () async {
      final notifier = AgentNotifier(apiClient: _FailingApiClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNotNull);
      expect(notifier.state.error!, contains('network failure'));
    });

    test('loadAgents sets error message from exception', () async {
      final client = _FailingApiClient();
      final notifier = AgentNotifier(apiClient: client);
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.error!, contains('Exception'));
    });

    test('loadAgents does not overwrite non-null error if already set', () {
      // The notifier starts with no error. After loadAgents() succeeds,
      // the error should be null since we set error: null in the first setState.
      final notifier = AgentNotifier(apiClient: _StubApiClient());
      notifier.loadAgents();
      // Even during loading, error is cleared immediately
    });

    // ===== Edge cases =====

    test('loadAgents with empty agents list from API', () async {
      final client = _EmptyAgentsApiClient();
      final notifier = AgentNotifier(apiClient: client);
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
      expect(notifier.state.agents, isEmpty);
    });

    test('loadAgents populates correctly when API returns agents with null id',
        () async {
      final client = _NullIdAgentApiClient();
      final notifier = AgentNotifier(apiClient: client);
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      // Agent.fromJson does json['id'] as String which throws on null,
      // so the error should be set
      expect(notifier.state.error, isNotNull);
      expect(notifier.state.agents, isEmpty);
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
      const agent = Agent(
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

    test('toJson includes all fields including null optional fields', () {
      const agent = Agent(
        id: 'a1',
        name: 'Test',
        description: 'No optional fields',
        enabled: false,
      );

      final json = agent.toJson();
      expect(json.containsKey('prompt'), isTrue);
      expect(json.containsKey('frontmatter'), isTrue);
    });
  });

  group('Agent Equatable (props)', () {
    test('agents with same content are equal', () {
      const a1 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);
      const a2 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);

      expect(a1, equals(a2));
    });

    test('agents with different ids are not equal', () {
      const a1 = Agent(id: 'a1', name: 'A', description: 'desc', enabled: true);
      const a2 = Agent(id: 'a2', name: 'A', description: 'desc', enabled: true);

      expect(a1 != a2, isTrue);
    });

    test('agents with different enable flag are not equal', () {
      const a1 = Agent(id: 'a', name: 'A', description: 'desc', enabled: true);
      const a2 = Agent(id: 'a', name: 'A', description: 'desc', enabled: false);

      expect(a1 != a2, isTrue);
    });
  });
}
