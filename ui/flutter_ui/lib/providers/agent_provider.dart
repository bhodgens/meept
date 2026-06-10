import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'providers.dart';

const _unset = Object();

/// State tracked by AgentNotifier
class AgentState {
  final List<Agent> agents;
  final bool isLoading;
  final String? error;

  const AgentState({
    this.agents = const [],
    this.isLoading = false,
    this.error,
  });

  AgentState copyWith({
    List<Agent>? agents,
    bool? isLoading,
    Object? error = _unset,
  }) {
    return AgentState(
      agents: agents ?? this.agents,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

/// StateNotifier that manages agent loading from the daemon
class AgentNotifier extends StateNotifier<AgentState> {
  AgentNotifier({required this.apiClient}) : super(const AgentState());

  final ApiClient apiClient;

  /// Fetch all agents from the daemon configuration
  Future<void> loadAgents() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final agents = await apiClient.listAgents();
      state = state.copyWith(agents: agents, isLoading: false);
    } catch (e) {
      state = state.copyWith(
        isLoading: false,
        error: e.toString(),
      );
    }
  }
}

/// Agent state provider
final agentProvider =
    StateNotifierProvider<AgentNotifier, AgentState>((ref) {
  final client = ref.watch(apiClientProvider);
  return AgentNotifier(apiClient: client);
});
