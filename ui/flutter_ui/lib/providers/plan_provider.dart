import 'dart:async';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/sdk_client.dart';
import '../services/websocket_service.dart';
import 'providers.dart';

const _unset = Object();

class PlanState {
  final List<Plan> plans;
  final bool isLoading;
  final String? error;

  const PlanState({
    this.plans = const [],
    this.isLoading = false,
    this.error,
  });

  PlanState copyWith({
    List<Plan>? plans,
    bool? isLoading,
    Object? error = _unset,
  }) {
    return PlanState(
      plans: plans ?? this.plans,
      isLoading: isLoading ?? this.isLoading,
      error: identical(error, _unset) ? this.error : error as String?,
    );
  }
}

class PlanNotifier extends StateNotifier<PlanState> {
  PlanNotifier({
    required this.sdkClient,
    this.webSocketService,
  }) : super(const PlanState()) {
    if (webSocketService != null) {
      try {
        _plansSubscription =
            webSocketService!.subscribeToPlans().listen((_) {
          // Refresh plans using the same filter as the last loadPlans call.
          loadPlans(sessionID: _lastSessionID, projectID: _lastProjectID);
        });
      } catch (e) {
        debugPrint('[warn] PlanNotifier WebSocket subscribe failed: $e');
      }
    }
  }

  final SdkApiClient sdkClient;
  final WebSocketService? webSocketService;

  StreamSubscription<Map<String, dynamic>>? _plansSubscription;

  // Store the params from the last loadPlans() call so that a
  // WebSocket-triggered refresh uses the same filter.
  String? _lastSessionID;
  String? _lastProjectID;

  Future<void> loadPlans({String? sessionID, String? projectID}) async {
    _lastSessionID = sessionID;
    _lastProjectID = projectID;
    state = state.copyWith(isLoading: true, error: null);
    try {
      // SdkApiClient.listPlansBySession/listPlans return the raw `plans`
      // arrays — callers deserialize each entry via Plan.fromJson because
      // the OpenAPI spec leaves the Plan entity untyped.
      final rawPlans = sessionID != null
          ? await sdkClient.listPlansBySession(sessionID)
          : await sdkClient.listPlans(projectId: projectID);
      final plans = rawPlans.map((p) => Plan.fromJson(p)).toList(growable: false);
      state = state.copyWith(plans: plans, isLoading: false);
    } catch (e) {
      state = state.copyWith(isLoading: false, error: e.toString());
    }
  }

  Future<void> approvePlan(String planID, {String? sessionID}) async {
    try {
      await sdkClient.approvePlan(planID, sessionID: sessionID, by: 'flutter_ui');
      await loadPlans(sessionID: sessionID);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> rejectPlan(String planID, {String? sessionID, String? reason}) async {
    try {
      await sdkClient.rejectPlan(planID, sessionID: sessionID, by: 'flutter_ui', reason: reason);
      await loadPlans(sessionID: sessionID);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> confirmPlan(String planID, {String? sessionID}) async {
    try {
      await sdkClient.confirmPlan(planID, sessionID: sessionID, by: 'flutter_ui');
      await loadPlans(sessionID: sessionID);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> revisePlan(String planID, {String? sessionID, String? feedback}) async {
    try {
      await sdkClient.revisePlan(planID, sessionID: sessionID, feedback: feedback);
      await loadPlans(sessionID: sessionID);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  void clearError() {
    state = state.copyWith(error: null);
  }

  @override
  void dispose() {
    _plansSubscription?.cancel();
    _plansSubscription = null;
    super.dispose();
  }
}

final planProvider = StateNotifierProvider<PlanNotifier, PlanState>((ref) {
  final client = ref.watch(sdkClientProvider);
  final ws = ref.watch(websocketProvider);
  return PlanNotifier(sdkClient: client, webSocketService: ws);
});
