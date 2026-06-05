import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'async_state.dart';
import 'providers.dart';

/// StateNotifier that manages agent loading from the daemon
class AgentNotifier extends StateNotifier<AsyncState<List<Agent>>> {
  AgentNotifier({required this.apiClient}) : super(const AsyncState.initial());

  final ApiClient apiClient;

  /// Fetch all agents from the daemon configuration
  Future<void> loadAgents() async {
    state = const AsyncState.loading();
    try {
      final agents = await apiClient.listAgents();
      state = AsyncState.data(agents);
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }
}

/// Agent state provider
final agentProvider =
    StateNotifierProvider<AgentNotifier, AsyncState<List<Agent>>>((ref) {
  final client = ref.read(apiClientProvider);
  return AgentNotifier(apiClient: client);
});
