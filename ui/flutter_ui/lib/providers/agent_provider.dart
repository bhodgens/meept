import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import 'providers.dart';

const _unset = Object();

/// Tracks quota episode state for one agent.
class AgentQuotaState {
  /// Whether the agent is currently blocked (tier 2 / 24h).
  final bool quotaBlocked;
  /// Epoch milliseconds when the quota unblocks (null means no wait).
  final int? quotaWaitUntilEpoch;
  /// Fallback model carrying work while the primary waits out its reset
  /// (event fallback_model; null when the backend sent none).
  final String? fallbackModel;
  /// Notify escalation tier from the latest quota event
  /// ("warn" | "action_recommended" | "blocked"). The backend sends ""
  /// on initial entry (to == quota_wait/blocked); tier firings arrive
  /// later as to == "" events. Null when never specified.
  final String? escalation;
  /// Parked-turn class from the tree 03 leaf 04 park event payload
  /// ("quota" | "throttle"; null on legacy events and tier refreshes).
  /// Selects the wait label: quota (or absent) → "quota_wait · reset
  /// HH:MM", throttle → "quota_wait · throttle retry HH:MM".
  final String? waitClass;

  const AgentQuotaState({
    required this.quotaBlocked,
    this.quotaWaitUntilEpoch,
    this.fallbackModel,
    this.escalation,
    this.waitClass,
  });

  AgentQuotaState copyWith({
    bool? quotaBlocked,
    int? quotaWaitUntilEpoch,
    String? fallbackModel,
    String? escalation,
    String? waitClass,
  }) {
    return AgentQuotaState(
      quotaBlocked: quotaBlocked ?? this.quotaBlocked,
      quotaWaitUntilEpoch:
          quotaWaitUntilEpoch ?? this.quotaWaitUntilEpoch,
      fallbackModel: fallbackModel ?? this.fallbackModel,
      escalation: escalation ?? this.escalation,
      waitClass: waitClass ?? this.waitClass,
    );
  }
}

/// State tracked by AgentNotifier
class AgentState {
  final List<Agent> agents;
  final bool isLoading;
  final String? error;
  /// Per-agent quota episode data keyed by agent id.
  final Map<String, AgentQuotaState> quotaEpisodes;

  const AgentState({
    this.agents = const [],
    this.isLoading = false,
    this.error,
    this.quotaEpisodes = const {},
  });

  AgentState copyWith({
    List<Agent>? agents,
    bool? isLoading,
    Object? error = _unset,
    Map<String, AgentQuotaState>? quotaEpisodes,
  }) {
    return AgentState(
      agents: agents ?? this.agents,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
      quotaEpisodes: quotaEpisodes ?? this.quotaEpisodes,
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
      final agents = rawAgents
          .map((a) => Agent.fromJson(a))
          .toList(growable: false);
      state = state.copyWith(agents: agents, isLoading: false);
    } catch (e) {
      state = state.copyWith(isLoading: false, error: e.toString());
    }
  }

  /// Apply a quota-status agent_progress event to [state].
  ///
  /// Valid transitions:
  ///  - to == 'quota_wait'  -> set quotaWaitUntilEpoch, quotaBlocked=false
  ///  - to == 'blocked'     -> set quotaBlocked=true, keep unblock time
  ///  - to == 'running'     -> clear the episode for this agent
  ///  - to == ''            -> tier escalation refresh (12h warn /
  ///     20h action_recommended): update unblock time + escalation tier on
  ///     the existing episode; ignored when no episode exists.
  void handleQuotaEvent({
    required String agentId,
    required String to,
    String? unblockAt,
    String? fallbackModel,
    String? escalation,
    String? waitClass,
  }) {
    // Normalize: backend sends "" for absent values; treat as null.
    final esc = (escalation == null || escalation.isEmpty) ? null : escalation;
    final cls = (waitClass == null || waitClass.isEmpty) ? null : waitClass;
    final episodes = Map<String, AgentQuotaState>.from(
      state.quotaEpisodes,
    );
    switch (to) {
      case 'quota_wait':
      case 'blocked':
        final blocked = to == 'blocked';
        final epoch = _parseQuotaEpoch(unblockAt);
        episodes[agentId] = AgentQuotaState(
          quotaBlocked: blocked,
          quotaWaitUntilEpoch: epoch,
          fallbackModel:
              (fallbackModel == null || fallbackModel.isEmpty) ? null : fallbackModel,
          escalation: esc,
          waitClass: cls,
        );
        break;
      case 'running':
        // Clear episode — quota was resolved.
        episodes.remove(agentId);
        break;
      case '':
        // Tier escalation refresh (12h warn / 20h action_recommended fire
        // with to == ""). The episode persists; only the unblock time and
        // escalation tier refresh. Ignored when no episode exists — there
        // is nothing to escalate. waitClass is not carried by tier events;
        // the parked class stays as entered.
        final existing = episodes[agentId];
        if (existing == null) return;
        episodes[agentId] = existing.copyWith(
          quotaWaitUntilEpoch: _parseQuotaEpoch(unblockAt),
          escalation: esc,
        );
        break;
      default:
        // Unknown transition — ignore silently.
        return;
    }
    state = state.copyWith(quotaEpisodes: episodes);
  }

  static int? _parseQuotaEpoch(String? iso) {
    if (iso == null || iso.isEmpty) return null;
    try {
      return DateTime.tryParse(iso)?.millisecondsSinceEpoch;
    } catch (_) {
      return null;
    }
  }
}

/// Agent state provider
final agentProvider = StateNotifierProvider<AgentNotifier, AgentState>((ref) {
  final client = ref.watch(sdkClientProvider);
  return AgentNotifier(sdkClient: client);
});
