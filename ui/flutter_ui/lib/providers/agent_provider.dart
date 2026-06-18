import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
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
  AgentNotifier({required this.sdkClient}) : super(const AgentState());

  final SdkApiClient sdkClient;

  /// Fetch all agents from the daemon configuration
  Future<void> loadAgents() async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      // SdkApiClient.listAgents returns the raw `agents` array — callers
      // are responsible for deserializing each entry via Agent.fromJson
      // because the OpenAPI spec leaves the Session entity untyped.
      final rawAgents = await sdkClient.listAgents();
      final agents =
          rawAgents.map((a) => Agent.fromJson(a)).toList(growable: false);
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
  final client = ref.watch(sdkClientProvider);
  return AgentNotifier(sdkClient: client);
});
