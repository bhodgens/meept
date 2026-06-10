import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
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
  PlanNotifier({required this.apiClient}) : super(const PlanState());

  final ApiClient apiClient;

  Future<void> loadPlans({String? sessionID, String? projectID}) async {
    state = state.copyWith(isLoading: true, error: null);
    try {
      final plans = sessionID != null
          ? await apiClient.listPlansBySession(sessionID)
          : await apiClient.listPlans(projectID: projectID);
      state = state.copyWith(plans: plans, isLoading: false);
    } catch (e) {
      state = state.copyWith(isLoading: false, error: e.toString());
    }
  }

  Future<void> approvePlan(String planID, {String? sessionID}) async {
    try {
      final updated = await apiClient.approvePlan(planID, sessionID: sessionID, by: 'flutter_ui');
      _updatePlanInList(updated);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> rejectPlan(String planID, {String? sessionID, String? reason}) async {
    try {
      final updated = await apiClient.rejectPlan(planID, sessionID: sessionID, by: 'flutter_ui', reason: reason);
      _updatePlanInList(updated);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> confirmPlan(String planID, {String? sessionID}) async {
    try {
      final updated = await apiClient.confirmPlan(planID, sessionID: sessionID, by: 'flutter_ui');
      _updatePlanInList(updated);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  Future<void> revisePlan(String planID, {String? sessionID, String? feedback}) async {
    try {
      final updated = await apiClient.revisePlan(planID, sessionID: sessionID, feedback: feedback);
      _updatePlanInList(updated);
    } catch (e) {
      state = state.copyWith(error: e.toString());
    }
  }

  void _updatePlanInList(Plan updated) {
    final newPlans = state.plans.map((p) => p.id == updated.id ? updated : p).toList();
    state = state.copyWith(plans: newPlans);
  }

  void clearError() {
    state = state.copyWith(error: null);
  }
}

final planProvider = StateNotifierProvider<PlanNotifier, PlanState>((ref) {
  final client = ref.watch(apiClientProvider);
  return PlanNotifier(apiClient: client);
});
