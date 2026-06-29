import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:meept_ui/models/api_models.dart';
import 'package:meept_ui/providers/agent_detail.dart';
import 'package:meept_ui/providers/plan_detail.dart';
import 'package:meept_ui/providers/providers.dart';
import 'package:meept_ui/providers/task_detail.dart';
import 'package:meept_ui/services/sdk_client.dart';

/// Stub [SdkApiClient] that counts per-id invocations of each get* method.
///
/// Each getter returns a deterministic model instance so tests can assert on
/// identity without depending on network shape. Subclasses override the
/// endpoint method directly; the base constructor's host/port are unused
/// but required.
class _CountingSdkClient extends SdkApiClient {
  final Map<String, int> agentCalls = {};
  final Map<String, int> planCalls = {};
  final Map<String, int> taskCalls = {};

  _CountingSdkClient() : super(host: 'localhost', port: 8081);

  @override
  Future<Map<String, dynamic>> getAgent(String id) async {
    agentCalls[id] = (agentCalls[id] ?? 0) + 1;
    return Agent(
      id: id,
      name: 'agent-$id',
      description: 'desc-$id',
    ).toJson();
  }

  @override
  Future<Map<String, dynamic>> getPlan(String id) async {
    planCalls[id] = (planCalls[id] ?? 0) + 1;
    return Plan(
      id: id,
      title: 'plan-$id',
      state: 'pending',
      createdAt: DateTime(2025, 1, 1),
      updatedAt: DateTime(2025, 1, 1),
    ).toJson();
  }

  @override
  Future<Map<String, dynamic>> getTask(String id) async {
    taskCalls[id] = (taskCalls[id] ?? 0) + 1;
    return Task(
      id: id,
      title: 'task-$id',
      description: 'desc-$id',
      status: 'pending',
      createdAt: DateTime(2025, 1, 1),
    ).toJson();
  }
}

void main() {
  late _CountingSdkClient client;
  late ProviderContainer container;

  setUp(() {
    client = _CountingSdkClient();
    container = ProviderContainer(overrides: [
      sdkClientProvider.overrideWithValue(client),
    ]);
    addTearDown(container.dispose);
  });

  group('agentDetailFamily', () {
    test('fetches once per id, returns cached on subsequent reads', () async {
      final first = container.read(agentDetailFamily('a1'));
      expect(first.isLoading, isTrue);

      final agent = await container.read(agentDetailFamily('a1').future);
      expect(agent.id, 'a1');
      expect(client.agentCalls['a1'], 1);

      // Second read of the same id returns the cached value, no refetch.
      final cached = container.read(agentDetailFamily('a1'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 'a1');
      expect(client.agentCalls['a1'], 1);

      // Different id triggers a new fetch.
      final other = await container.read(agentDetailFamily('a2').future);
      expect(other.id, 'a2');
      expect(client.agentCalls['a1'], 1);
      expect(client.agentCalls['a2'], 1);
    });

    test('prefetch (fire-and-forget read) warms the cache', () async {
      container.read(agentDetailFamily('warm'));
      // Let the microtask resolve.
      await Future.delayed(const Duration(milliseconds: 5));

      final cached = container.read(agentDetailFamily('warm'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 'warm');
      expect(client.agentCalls['warm'], 1);
    });
  });

  group('planDetailFamily', () {
    test('fetches once per id, returns cached on subsequent reads', () async {
      final first = container.read(planDetailFamily('p1'));
      expect(first.isLoading, isTrue);

      final plan = await container.read(planDetailFamily('p1').future);
      expect(plan.id, 'p1');
      expect(client.planCalls['p1'], 1);

      final cached = container.read(planDetailFamily('p1'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 'p1');
      expect(client.planCalls['p1'], 1);

      // Different id triggers a new fetch.
      final other = await container.read(planDetailFamily('p2').future);
      expect(other.id, 'p2');
      expect(client.planCalls['p1'], 1);
      expect(client.planCalls['p2'], 1);
    });

    test('prefetch (fire-and-forget read) warms the cache', () async {
      container.read(planDetailFamily('warm'));
      await Future.delayed(const Duration(milliseconds: 5));

      final cached = container.read(planDetailFamily('warm'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 'warm');
      expect(client.planCalls['warm'], 1);
    });
  });

  group('taskDetailFamily', () {
    test('fetches once per id, returns cached on subsequent reads', () async {
      final first = container.read(taskDetailFamily('t1'));
      expect(first.isLoading, isTrue);

      final task = await container.read(taskDetailFamily('t1').future);
      expect(task.id, 't1');
      expect(client.taskCalls['t1'], 1);

      final cached = container.read(taskDetailFamily('t1'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 't1');
      expect(client.taskCalls['t1'], 1);

      // Different id triggers a new fetch.
      final other = await container.read(taskDetailFamily('t2').future);
      expect(other.id, 't2');
      expect(client.taskCalls['t1'], 1);
      expect(client.taskCalls['t2'], 1);
    });

    test('prefetch (fire-and-forget read) warms the cache', () async {
      container.read(taskDetailFamily('warm'));
      await Future.delayed(const Duration(milliseconds: 5));

      final cached = container.read(taskDetailFamily('warm'));
      expect(cached.hasValue, isTrue);
      expect(cached.value?.id, 'warm');
      expect(client.taskCalls['warm'], 1);
    });
  });

  // Cross-family smoke test: ensure families don't interfere with each other
  // when sharing the same sdkClientProvider override (mirrors real usage in
  // HomeScreen where all three are wired simultaneously).
  group('cross-family isolation', () {
    test('families do not share state across models', () async {
      await container.read(agentDetailFamily('x').future);
      await container.read(planDetailFamily('x').future);
      await container.read(taskDetailFamily('x').future);

      expect(client.agentCalls['x'], 1);
      expect(client.planCalls['x'], 1);
      expect(client.taskCalls['x'], 1);

      // Re-reads from cache, no new fetches.
      container.read(agentDetailFamily('x'));
      container.read(planDetailFamily('x'));
      container.read(taskDetailFamily('x'));

      expect(client.agentCalls['x'], 1);
      expect(client.planCalls['x'], 1);
      expect(client.taskCalls['x'], 1);
    });
  });
}
