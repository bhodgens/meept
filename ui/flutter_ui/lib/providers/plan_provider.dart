import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/api_models.dart';
import '../services/api_client.dart';
import 'async_state.dart';
import 'providers.dart';

class PlanNotifier extends StateNotifier<AsyncState<List<Plan>>> {
  PlanNotifier({required this.apiClient}) : super(const AsyncState.initial());

  final ApiClient apiClient;

  Future<void> loadPlans({String? sessionID, String? projectID}) async {
    state = const AsyncState.loading();
    try {
      final plans = sessionID != null
          ? await apiClient.listPlansBySession(sessionID)
          : await apiClient.listPlans(projectID: projectID);
      state = AsyncState.data(plans);
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  Future<void> approvePlan(String planID, {String? sessionID}) async {
    final currentPlans = state.whenOrNull(data: (p) => p) ?? [];
    try {
      final updated = await apiClient.approvePlan(planID, sessionID: sessionID, by: 'flutter_ui');
      state = AsyncState.data(_updatePlanInList(currentPlans, updated));
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  Future<void> rejectPlan(String planID, {String? sessionID, String? reason}) async {
    final currentPlans = state.whenOrNull(data: (p) => p) ?? [];
    try {
      final updated = await apiClient.rejectPlan(planID, sessionID: sessionID, by: 'flutter_ui', reason: reason);
      state = AsyncState.data(_updatePlanInList(currentPlans, updated));
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  Future<void> confirmPlan(String planID, {String? sessionID}) async {
    final currentPlans = state.whenOrNull(data: (p) => p) ?? [];
    try {
      final updated = await apiClient.confirmPlan(planID, sessionID: sessionID, by: 'flutter_ui');
      state = AsyncState.data(_updatePlanInList(currentPlans, updated));
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  Future<void> revisePlan(String planID, {String? sessionID, String? feedback}) async {
    final currentPlans = state.whenOrNull(data: (p) => p) ?? [];
    try {
      final updated = await apiClient.revisePlan(planID, sessionID: sessionID, feedback: feedback);
      state = AsyncState.data(_updatePlanInList(currentPlans, updated));
    } catch (e, st) {
      state = AsyncState.error(e, st);
    }
  }

  List<Plan> _updatePlanInList(List<Plan> plans, Plan updated) {
    return plans.map((p) => p.id == updated.id ? updated : p).toList();
  }

  void clearError() {
    state = const AsyncState.initial();
  }
}

final planProvider = StateNotifierProvider<PlanNotifier, AsyncState<List<Plan>>>((ref) {
  final client = ref.watch(apiClientProvider);
  return PlanNotifier(apiClient: client);
});
