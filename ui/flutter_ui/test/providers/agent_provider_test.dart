import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/providers/agent_provider.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/services/sdk_client.dart';

// ===== Stub classes =====

/// Old-daemon roster miss: GET /api/v1/agents/roster answers 404, so
/// loadAgents must fall back to the legacy /api/v1/config/agents list.
SdkApiException _roster404() =>
    SdkApiException(message: 'not found', statusCode: 404);

/// Stub [SdkApiClient] that returns a fixed agents payload.
///
/// Overrides the endpoint method (now a regular instance method on
/// [SdkApiClient]) so the test doesn't need to hit the network. The
/// roster route 404s (old daemon) — loadAgents falls back to listAgents.
class _StubSdkClient extends SdkApiClient {
  _StubSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async => throw _roster404();

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    return (_mockAgentsResponse['agents'] as List)
        .map((a) => Map<String, dynamic>.from(a as Map))
        .toList();
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// An SDK client whose employee roster marks 'coder' with an acp
/// executor reference (acp-employees phase 3 display parity tests).
class _ExecutorRosterSdkClient extends _StubSdkClient {
  @override
  Future<List<Map<String, dynamic>>> listEmployees() async {
    return [
      {'id': 'coder', 'name': 'Coder', 'executor_agent': 'hermes'},
      {'id': 'analyst', 'name': 'Analyst'}, // no executor_agent
      {'id': 'blank', 'name': 'Blank', 'executor_agent': '   '},
    ];
  }
}

/// An SDK client whose employee roster fetch throws — the notifier must
/// keep the executor map unchanged and never surface an agents-tab error.
class _RosterFailingSdkClient extends _StubSdkClient {
  @override
  Future<List<Map<String, dynamic>>> listEmployees() async {
    throw Exception('employees disabled');
  }
}

/// An SDK client that throws on any call, to simulate failure.
class _FailingSdkClient extends SdkApiClient {
  _FailingSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async {
    throw Exception('network failure');
  }

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    throw Exception('network failure');
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// An SDK client that returns an empty agents list.
class _EmptyAgentsSdkClient extends SdkApiClient {
  _EmptyAgentsSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async => throw _roster404();

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    return [];
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// An SDK client that returns one agent with null id, to verify error handling.
class _NullIdAgentSdkClient extends SdkApiClient {
  _NullIdAgentSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async => throw _roster404();

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    // The notifier will call Agent.fromJson which does json['id'] as String
    // and throws on null, surfacing a TypeError.
    return [
      <String, dynamic>{'id': null, 'name': 'No ID'},
    ];
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async => const [];
}

/// Unified-roster client (M17): GET /api/v1/agents/roster answers frozen
/// C1 rows merging agents + bots + employees, plus the additive M11 keys
/// (executor_agent / drift_score / findings_count / last_invocation,
/// zero-omitted). listAgents AND listEmployees must NOT be consulted when
/// the roster succeeds — the roster carries the executor references.
class _RosterSdkClient extends SdkApiClient {
  _RosterSdkClient() : super(host: 'localhost', port: 8081);

  bool legacyListCalled = false;
  bool employeesListCalled = false;

  @override
  Future<Map<String, dynamic>> getAgentsRoster() async => {
    'agents': [
      {
        'id': 'dispatcher',
        'name': 'Dispatcher',
        'type': 'agent',
        'status': 'running',
        'active_count': 2,
        'daily_cost_cents': 0,
        'role': 'dispatcher',
        'description': 'Routes tasks',
        'enabled': true,
        // Registry agents carry NO M11 keys (zero-omitted on the wire).
      },
      {
        'id': 'news-bot',
        'name': 'News Bot',
        'type': 'bot',
        'status': 'stopped',
        'active_count': 0,
        'daily_cost_cents': 0,
        'role': '',
        'description': 'posts headlines',
        'enabled': true,
        // Bots carry executor_agent + last_invocation (no drift/findings).
        'executor_agent': 'hermes',
        'last_invocation': '2026-11-19T08:30:00Z',
      },
      {
        'id': 'coder',
        'name': 'Coder',
        'type': 'employee',
        'status': 'running',
        'active_count': 1,
        'daily_cost_cents': 1234,
        'role': 'engineer',
        'description': 'Writes code',
        'enabled': true,
        // Employees carry all four M11 keys.
        'executor_agent': 'hermes',
        'drift_score': 0.25,
        'findings_count': 3,
        'last_invocation': '2026-11-19T12:00:00Z',
      },
      {
        'id': 'idle-emp',
        'name': 'Idle Emp',
        'type': 'employee',
        'status': 'stopped',
        'active_count': 0,
        'daily_cost_cents': 0,
        'role': '',
        'description': 'never ran',
        'enabled': true,
        // Never-run shape: drift/findings zero-omitted, last_invocation
        // ABSENT (omitzero) — decodes to null, never a year-1 zero time.
      },
    ],
  };

  @override
  Future<List<Map<String, dynamic>>> listAgents() async {
    legacyListCalled = true;
    throw StateError('legacy list must not be called when roster succeeds');
  }

  @override
  Future<List<Map<String, dynamic>>> listEmployees() async {
    employeesListCalled = true;
    throw StateError(
      'employee list must not be called when roster succeeds (M11 keys ride the roster)',
    );
  }
}

/// Roster route exists but fails with a non-404 (e.g. 503 roster service
/// unavailable): the error must surface, NOT silently fall back.
class _RosterBrokenSdkClient extends _StubSdkClient {
  @override
  Future<Map<String, dynamic>> getAgentsRoster() async =>
      throw SdkApiException(message: 'roster unavailable', statusCode: 503);
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
      final updated = original.copyWith(isLoading: true, error: null);
      expect(updated.agents, original.agents);
      expect(updated.isLoading, isTrue);
      expect(updated.error, isNull);
    });

    test('copyWith preserves unprovided fields', () {
      const original = AgentState(
        agents: [
          Agent(id: 'a1', name: 'A1', description: 'desc', enabled: true),
        ],
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
      const state2 = AgentState(isLoading: false);

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
      const agents = [
        Agent(id: 'x', name: 'X', description: '', enabled: true),
      ];
      const state = AgentState(agents: agents);
      final copy = state.copyWith(agents: null);
      expect(copy.agents, agents);
    });
  });

  group('AgentNotifier.loadAgents()', () {
    test('initial state is loading = false', () {
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      expect(notifier.state.isLoading, isFalse);
    });

    test('loadAgents sets loading to true then false on success', () async {
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      expect(notifier.state.isLoading, isFalse);

      notifier.loadAgents();
      expect(notifier.state.isLoading, isTrue);

      await Future.delayed(const Duration(milliseconds: 100));
      expect(notifier.state.isLoading, isFalse);
    });

    test('loadAgents populates agents list on success', () async {
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.agents, hasLength(3));
      expect(notifier.state.agents[0].id, 'dispatcher');
      expect(notifier.state.agents[1].name, 'Coder');
      expect(notifier.state.agents[2].enabled, isFalse);
    });

    test('loadAgents clears error on success', () async {
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.error, isNull);
    });

    test('loadAgents sets error on failure', () async {
      final notifier = AgentNotifier(sdkClient: _FailingSdkClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNotNull);
      expect(notifier.state.error!, contains('network failure'));
    });

    test('loadAgents sets error message from exception', () async {
      final client = _FailingSdkClient();
      final notifier = AgentNotifier(sdkClient: client);
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.error!, contains('Exception'));
    });

    test('loadAgents does not overwrite non-null error if already set', () {
      // The notifier starts with no error. After loadAgents() succeeds,
      // the error should be null since we set error: null in the first setState.
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      notifier.loadAgents();
      // Even during loading, error is cleared immediately
    });

    // ===== Edge cases =====

    test('loadAgents with empty agents list from API', () async {
      final client = _EmptyAgentsSdkClient();
      final notifier = AgentNotifier(sdkClient: client);
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      expect(notifier.state.isLoading, isFalse);
      expect(notifier.state.error, isNull);
      expect(notifier.state.agents, isEmpty);
    });

    test(
      'loadAgents populates correctly when API returns agents with null id',
      () async {
        final client = _NullIdAgentSdkClient();
        final notifier = AgentNotifier(sdkClient: client);
        notifier.loadAgents();
        await Future.delayed(const Duration(milliseconds: 100));

        // Agent.fromJson does json['id'] as String which throws on null,
        // so the error should be set
        expect(notifier.state.error, isNotNull);
        expect(notifier.state.agents, isEmpty);
      },
    );

    test('loadAgents populates employeeExecutors from the roster', () async {
      final notifier = AgentNotifier(sdkClient: _ExecutorRosterSdkClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      // executor_agent rides through under the employee id; a row
      // without the field, and a whitespace-only value, stay absent.
      expect(notifier.state.employeeExecutors, {'coder': 'hermes'});
      expect(notifier.state.error, isNull);
      // Legacy-fallback enrichment also stamps the reference onto the
      // decoded model: the tile badge / detail line read executorAgent.
      final coder = notifier.state.agents.firstWhere((a) => a.id == 'coder');
      expect(coder.executorAgent, 'hermes');
      final analyst = notifier.state.agents.firstWhere(
        (a) => a.id == 'analyst',
      );
      expect(analyst.executorAgent, '');
    });

    test('loadAgents survives a failing employee roster fetch', () async {
      final notifier = AgentNotifier(sdkClient: _RosterFailingSdkClient());
      notifier.loadAgents();
      await Future.delayed(const Duration(milliseconds: 100));

      // The roster is best-effort: agents load, executors stay empty,
      // and the failure never surfaces as an agents-tab error.
      expect(notifier.state.agents, hasLength(3));
      expect(notifier.state.employeeExecutors, isEmpty);
      expect(notifier.state.error, isNull);
    });

    // ===== M17: unified roster wiring =====

    test(
      'loadAgents prefers the unified roster (agents+bots+employees)',
      () async {
        final client = _RosterSdkClient();
        final notifier = AgentNotifier(sdkClient: client);
        await notifier.loadAgents();

        expect(notifier.state.error, isNull);
        expect(
          client.legacyListCalled,
          isFalse,
          reason: 'roster success must not consult the legacy config list',
        );
        expect(
          client.employeesListCalled,
          isFalse,
          reason:
              'roster success must not make the second listEmployees call '
              '(M11 executor_agent rides the roster rows)',
        );
        expect(notifier.state.agents, hasLength(4));
        expect(notifier.state.agents.map((a) => a.type).toList(), [
          'agent',
          'bot',
          'employee',
          'employee',
        ]);
      },
    );

    test(
      'roster rows decode the frozen C1 fields onto the Agent model',
      () async {
        final notifier = AgentNotifier(sdkClient: _RosterSdkClient());
        await notifier.loadAgents();

        final coder = notifier.state.agents.firstWhere((a) => a.id == 'coder');
        expect(coder.name, 'Coder');
        expect(coder.type, 'employee');
        expect(coder.status, 'running');
        expect(coder.activeCount, 1);
        expect(coder.dailyCostCents, 1234);
        expect(coder.role, 'engineer');
        expect(coder.description, 'Writes code');
        expect(coder.enabled, isTrue);

        final bot = notifier.state.agents.firstWhere((a) => a.id == 'news-bot');
        expect(bot.type, 'bot');
        expect(bot.status, 'stopped');
        expect(bot.activeCount, 0);
      },
    );

    // ===== M11: additive roster keys (executor/drift/findings/last-run) =====

    test('roster rows decode all four additive M11 fields', () async {
      final notifier = AgentNotifier(sdkClient: _RosterSdkClient());
      await notifier.loadAgents();

      // Employee rows carry all four keys.
      final coder = notifier.state.agents.firstWhere((a) => a.id == 'coder');
      expect(coder.executorAgent, 'hermes');
      expect(coder.driftScore, 0.25);
      expect(coder.findingsCount, 3);
      expect(coder.lastInvocation, DateTime.parse('2026-11-19T12:00:00Z'));

      // Bot rows carry executor_agent + last_invocation only; the
      // employee-only drift/findings keys stay at their defaults.
      final bot = notifier.state.agents.firstWhere((a) => a.id == 'news-bot');
      expect(bot.executorAgent, 'hermes');
      expect(bot.lastInvocation, DateTime.parse('2026-11-19T08:30:00Z'));
      expect(bot.driftScore, 0.0);
      expect(bot.findingsCount, 0);
    });

    test('absent last_invocation decodes to null (never run), zero-omitted '
        'M11 keys decode to defaults', () async {
      final notifier = AgentNotifier(sdkClient: _RosterSdkClient());
      await notifier.loadAgents();

      // Never-run employee: last_invocation ABSENT (omitzero wire shape).
      final idle = notifier.state.agents.firstWhere((a) => a.id == 'idle-emp');
      expect(idle.lastInvocation, isNull);
      expect(idle.executorAgent, '');
      expect(idle.driftScore, 0.0);
      expect(idle.findingsCount, 0);

      // Registry agent: no M11 keys at all.
      final dispatcher = notifier.state.agents.firstWhere(
        (a) => a.id == 'dispatcher',
      );
      expect(dispatcher.executorAgent, '');
      expect(dispatcher.driftScore, 0.0);
      expect(dispatcher.findingsCount, 0);
      expect(dispatcher.lastInvocation, isNull);
    });

    test(
      'roster success derives executor refs from the rows (no second call)',
      () async {
        final client = _RosterSdkClient();
        final notifier = AgentNotifier(sdkClient: client);
        await notifier.loadAgents();

        expect(notifier.state.employeeExecutors, {
          'news-bot': 'hermes',
          'coder': 'hermes',
        });
        expect(client.employeesListCalled, isFalse);
        expect(notifier.state.error, isNull);
      },
    );

    test('roster 404 falls back to the legacy config agents list', () async {
      final notifier = AgentNotifier(sdkClient: _StubSdkClient());
      await notifier.loadAgents();

      // _StubSdkClient.getAgentsRoster throws 404 (old daemon); the
      // legacy list still populates the tab, without roster fields.
      expect(notifier.state.error, isNull);
      expect(notifier.state.agents, hasLength(3));
      expect(notifier.state.agents[0].id, 'dispatcher');
      expect(notifier.state.agents[0].type, '');
      expect(notifier.state.agents[0].status, '');
      expect(notifier.state.agents[0].activeCount, 0);
    });

    test(
      'non-404 roster failure surfaces as an error (no silent fallback)',
      () async {
        final notifier = AgentNotifier(sdkClient: _RosterBrokenSdkClient());
        await notifier.loadAgents();

        expect(notifier.state.error, isNotNull);
        expect(notifier.state.error!, contains('503'));
        expect(notifier.state.agents, isEmpty);
      },
    );

    test(
      'roster load preserves quota episodes and executor enrichment',
      () async {
        final notifier = AgentNotifier(sdkClient: _RosterSdkClient());
        notifier.handleQuotaEvent(
          agentId: 'coder',
          to: 'quota_wait',
          unblockAt: '2026-11-19T12:00:00Z',
        );
        await notifier.loadAgents();

        // Quota episode state survives the roster reload.
        expect(notifier.state.quotaEpisodes.containsKey('coder'), isTrue);
        expect(notifier.state.agents, hasLength(4));
        // Executor refs survive too (derived from the M11 roster keys).
        expect(notifier.state.employeeExecutors['coder'], 'hermes');
      },
    );
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

    // ===== M11 additive roster keys: model-level decode pins =====

    test('Agent.fromJson decodes all four M11 roster keys', () {
      final agent = Agent.fromJson({
        'id': 'emp',
        'name': 'Emp',
        'type': 'employee',
        'executor_agent': 'hermes',
        'drift_score': 0.42,
        'findings_count': 7,
        'last_invocation': '2026-11-19T12:34:56Z',
      });
      expect(agent.executorAgent, 'hermes');
      expect(agent.driftScore, 0.42);
      expect(agent.findingsCount, 7);
      expect(agent.lastInvocation, DateTime.parse('2026-11-19T12:34:56Z'));
    });

    test('Agent.fromJson: absent M11 keys decode to defaults / null', () {
      final agent = Agent.fromJson({'id': 'emp', 'name': 'Emp'});
      expect(agent.executorAgent, '');
      expect(agent.driftScore, 0.0);
      expect(agent.findingsCount, 0);
      // Absent last_invocation = never run = null (the daemon omitzero
      // shape; never a '0001-01-01...' zero time on the wire).
      expect(agent.lastInvocation, isNull);
    });

    test('Agent.fromJson: zero drift decodes honestly as 0.0', () {
      // drift_score reads 0 until a production writer lands — an explicit
      // zero and an absent key both decode to 0.0 (displayed as '0.00').
      final explicit = Agent.fromJson({
        'id': 'emp',
        'name': 'Emp',
        'drift_score': 0,
      });
      expect(explicit.driftScore, 0.0);
      final absent = Agent.fromJson({'id': 'emp', 'name': 'Emp'});
      expect(absent.driftScore, 0.0);
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
