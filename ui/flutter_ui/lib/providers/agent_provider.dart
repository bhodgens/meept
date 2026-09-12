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

  /// The daemon's UTC offset (in minutes) embedded in the event's RFC3339
  /// unblock time (M9 timezone convention: producers Format(time.RFC3339),
  /// so the offset rides on the wire). Surfaces render the DAEMON's
  /// wall-clock by default — the offset is what makes that possible after
  /// the time has been normalized to epoch millis. Null only when no
  /// unblock time was parsed alongside the epoch (defensive; the parser
  /// fills both from the same string).
  final int? quotaWaitUntilOffsetMinutes;

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

  /// Park lifecycle reason (I-M8): "quota_wait" | "throttle_wait" |
  /// "throttle_resumed" | "throttle_give_up"; null on legacy events. A
  /// give-up renders the give-up badge instead of a wait label.
  final String? reason;

  const AgentQuotaState({
    required this.quotaBlocked,
    this.quotaWaitUntilEpoch,
    this.quotaWaitUntilOffsetMinutes,
    this.fallbackModel,
    this.escalation,
    this.waitClass,
    this.reason,
  });

  /// Sentinel-aware copyWith (I-M8 stale-badge fix).
  ///
  /// Every nullable field defaults to the [_unset] marker, giving three
  /// distinct outcomes that the previous null-preserving signature could
  /// not express:
  ///
  ///   omitted            → field preserved (same as before)
  ///   explicitly `null`  → field CLEARED
  ///   a value            → field set
  ///
  /// The old `String? x` parameters made `copyWith(x: null)` identical to
  /// omitting x, so a clear/park event that carried an empty reason or
  /// resume time could never clear stale badge state. Callers decide
  /// absent-vs-explicit from the wire payload (see [AgentNotifier]).
  AgentQuotaState copyWith({
    bool? quotaBlocked,
    Object? quotaWaitUntilEpoch = _unset,
    Object? quotaWaitUntilOffsetMinutes = _unset,
    Object? fallbackModel = _unset,
    Object? escalation = _unset,
    Object? waitClass = _unset,
    Object? reason = _unset,
  }) {
    return AgentQuotaState(
      quotaBlocked: quotaBlocked ?? this.quotaBlocked,
      quotaWaitUntilEpoch: identical(quotaWaitUntilEpoch, _unset)
          ? this.quotaWaitUntilEpoch
          : quotaWaitUntilEpoch as int?,
      quotaWaitUntilOffsetMinutes:
          identical(quotaWaitUntilOffsetMinutes, _unset)
          ? this.quotaWaitUntilOffsetMinutes
          : quotaWaitUntilOffsetMinutes as int?,
      fallbackModel: identical(fallbackModel, _unset)
          ? this.fallbackModel
          : fallbackModel as String?,
      escalation: identical(escalation, _unset)
          ? this.escalation
          : escalation as String?,
      waitClass: identical(waitClass, _unset)
          ? this.waitClass
          : waitClass as String?,
      reason: identical(reason, _unset) ? this.reason : reason as String?,
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
  ///     the existing episode; ignored when no episode exists. A
  ///     throttle_give_up reason (I-M8) stores the reason instead of
  ///     refreshing (the badge switches to the give-up label) and never
  ///     wipes the episode.
  ///
  /// Field semantics within an event (I-M8): a field ABSENT from the wire
  /// (null) preserves the stored value; a field explicitly carrying an
  /// empty string clears it. This is what lets genuinely-clear events
  /// reset stale badge state while refresh events leave untouched fields
  /// alone.
  void handleQuotaEvent({
    required String agentId,
    required String to,
    String? unblockAt,
    String? fallbackModel,
    String? escalation,
    String? waitClass,
    String? reason,
  }) {
    // Normalize: backend sends "" for absent values; treat as null.
    final esc = (escalation == null || escalation.isEmpty) ? null : escalation;
    final cls = (waitClass == null || waitClass.isEmpty) ? null : waitClass;
    final rsn = (reason == null || reason.isEmpty) ? null : reason;
    final episodes = Map<String, AgentQuotaState>.from(state.quotaEpisodes);
    switch (to) {
      case 'quota_wait':
      case 'blocked':
        final blocked = to == 'blocked';
        final epoch = _parseQuotaEpoch(unblockAt);
        episodes[agentId] = AgentQuotaState(
          quotaBlocked: blocked,
          quotaWaitUntilEpoch: epoch,
          quotaWaitUntilOffsetMinutes: _parseQuotaOffsetMinutes(unblockAt),
          fallbackModel: (fallbackModel == null || fallbackModel.isEmpty)
              ? null
              : fallbackModel,
          escalation: esc,
          waitClass: cls,
          reason: rsn,
        );
        break;
      case 'running':
        // Clear episode — quota was resolved.
        episodes.remove(agentId);
        break;
      case '':
        // Tier escalation refresh (12h warn / 20h action_recommended fire
        // with to == "") or a throttle give-up. The episode persists;
        // absent wire fields preserve, explicitly empty ones clear
        // (sentinel copyWith). Ignored when no episode exists — there is
        // nothing to escalate or give up on.
        final existing = episodes[agentId];
        if (existing == null) return;
        episodes[agentId] = existing.copyWith(
          quotaWaitUntilEpoch: unblockAt == null
              ? _unset
              : _parseQuotaEpoch(unblockAt),
          quotaWaitUntilOffsetMinutes: unblockAt == null
              ? _unset
              : _parseQuotaOffsetMinutes(unblockAt),
          fallbackModel: _optionalOrClear(fallbackModel),
          escalation: _optionalOrClear(escalation),
          waitClass: _optionalOrClear(waitClass),
          reason: _optionalOrClear(reason),
        );
        break;
      default:
        // Unknown transition — ignore silently.
        return;
    }
    state = state.copyWith(quotaEpisodes: episodes);
  }

  /// Wire-optional string → copyWith sentinel: absent (null) preserves the
  /// stored value, an explicitly empty string clears it, a value sets it.
  static Object? _optionalOrClear(String? raw) {
    if (raw == null) return _unset;
    if (raw.isEmpty) return null;
    return raw;
  }

  static int? _parseQuotaEpoch(String? iso) {
    if (iso == null || iso.isEmpty) return null;
    try {
      return DateTime.tryParse(iso)?.millisecondsSinceEpoch;
    } catch (_) {
      return null;
    }
  }

  /// The daemon's UTC offset embedded in the RFC3339 string, in minutes.
  ///
  /// This is a STRING-level parse, deliberately: Dart's DateTime.tryParse
  /// normalizes any explicit offset to UTC (isUtc == true), so
  /// parsed.timeZoneOffset would always be 0 and could never recover the
  /// daemon zone. Go producers Format(time.RFC3339) which always ends in
  /// "Z" or "+hh:mm"/"-hh:mm" — match those directly. Null when the string
  /// carries neither form.
  static int? _parseQuotaOffsetMinutes(String? iso) {
    if (iso == null || iso.isEmpty) return null;
    if (iso.endsWith('Z') || iso.endsWith('z')) return 0;
    final m = RegExp(r'([+-])(\d{2}):?(\d{2})$').firstMatch(iso);
    if (m == null) return null;
    final sign = m.group(1) == '-' ? -1 : 1;
    final hours = int.parse(m.group(2)!);
    final minutes = int.parse(m.group(3)!);
    return sign * (hours * 60 + minutes);
  }
}

/// Agent state provider
final agentProvider = StateNotifierProvider<AgentNotifier, AgentState>((ref) {
  final client = ref.watch(sdkClientProvider);
  return AgentNotifier(sdkClient: client);
});
