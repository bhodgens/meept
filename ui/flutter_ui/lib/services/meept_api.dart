import 'package:dio/dio.dart';
import '../models/api_models.dart';

/// Typed HTTP client for the Meept REST API.
///
/// Provides a fully-typed interface for every endpoint consumed by the
/// Flutter UI.  Wraps a pre-configured [Dio] instance that carries the
/// base URL, timeouts, TLS config, and auth header.
///
/// This replaces the generic `ApiClient.get<T>/post<T>` calls with
/// named, typed methods.  Construct via [MeeptApi] passing a [Dio]
/// instance configured by [ApiClient].
///
/// Example:
/// ```dart
/// final dio = ...; // configured with baseUrl, TLS, auth
/// final api = MeeptApi(dio);
/// final status = await api.getDaemonStatus();
/// final sessions = await api.listSessions();
/// ```
class MeeptApi {
  final Dio _dio;

  MeeptApi(this._dio);

  // ===== Health =====

  /// GET /health (outside /api/v1 prefix -- uses root URL).
  Future<Map<String, dynamic>> healthCheck() async {
    // Health is at root /health, not under the API prefix.
    // The caller should construct a separate Dio for this, or we strip the
    // base path.  For simplicity, use the full URL relative to root.
    final rootUrl = _dio.options.baseUrl;
    final idx = rootUrl.indexOf('/api/');
    final rootBase = idx >= 0 ? rootUrl.substring(0, idx) : rootUrl;
    final response = await _dio.get('$rootBase/health');
    return response.data as Map<String, dynamic>;
  }

  // ===== Daemon =====

  Future<Map<String, dynamic>> getDaemonStatus() async {
    final response = await _dio.get('/api/v1/daemon/status');
    return response.data as Map<String, dynamic>;
  }

  // ===== Chat =====

  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    final response = await _dio.post('/api/v1/chat', data: {
      'message': message,
      if (conversationId != null) 'conversation_id': conversationId,
      if (agentId != null) 'agent_id': agentId,
    });
    return response.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> sendSteerMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final response = await _dio.post('/api/v1/chat/steer', data: {
      'message': message,
      'conversation_id': conversationId,
      if (source != null) 'source': source,
    });
    return response.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> sendFollowUpMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final response = await _dio.post('/api/v1/chat/followup', data: {
      'message': message,
      'conversation_id': conversationId,
      if (source != null) 'source': source,
    });
    return response.data as Map<String, dynamic>;
  }

  // ===== Sessions =====

  Future<List<Session>> listSessions() async {
    final response = await _dio.get('/api/v1/sessions');
    final data = response.data as Map<String, dynamic>;
    final raw = data['sessions'] as List?;
    if (raw == null) return [];
    return raw.map((s) => Session.fromJson(s as Map<String, dynamic>)).toList();
  }

  Future<Session> getSession(String id) async {
    final response = await _dio.get('/api/v1/sessions/$id');
    return Session.fromJson(response.data as Map<String, dynamic>);
  }

  Future<List<ChatMessage>> getMessages(
    String id, {
    int offset = 0,
    int limit = 1000,
  }) async {
    final response = await _dio.get('/api/v1/sessions/$id/messages', queryParameters: {
      'offset': offset,
      'limit': limit,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['messages'] as List?;
    if (raw == null) return [];
    return raw
        .map((m) => ChatMessage.fromBackendMessage(m as Map<String, dynamic>))
        .toList();
  }

  Future<Session> createSession({
    required String title,
    String? agentId,
  }) async {
    final response = await _dio.post('/api/v1/sessions', data: {
      'name': title,
      if (agentId != null) 'agent_id': agentId,
    });
    return Session.fromJson(response.data as Map<String, dynamic>);
  }

  Future<void> deleteSession(String id) async {
    await _dio.delete('/api/v1/sessions/$id');
  }

  Future<List<Plan>> listPlansBySession(String sessionId) async {
    final response = await _dio.get('/api/v1/sessions/$sessionId/plans');
    final data = response.data as Map<String, dynamic>;
    final raw = data['plans'] as List?;
    if (raw == null) return [];
    return raw.map((p) => Plan.fromJson(p as Map<String, dynamic>)).toList();
  }

  // ===== Agents =====

  Future<List<Agent>> listAgents() async {
    final response = await _dio.get('/api/v1/config/agents');
    final data = response.data as Map<String, dynamic>;
    final raw = data['agents'] as List?;
    if (raw == null) return [];
    return raw.map((a) => Agent.fromJson(a as Map<String, dynamic>)).toList();
  }

  Future<Agent> updateAgent(String id, Map<String, dynamic> config) async {
    final response = await _dio.post('/api/v1/config/agents/$id', data: config);
    return Agent.fromJson(response.data as Map<String, dynamic>);
  }

  // ===== Tasks =====

  Future<List<Task>> listTasks({String? sessionId}) async {
    final response = await _dio.get('/api/v1/tasks', queryParameters: {
      if (sessionId != null) 'session_id': sessionId,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['tasks'] as List?;
    if (raw == null) return [];
    return raw.map((t) => Task.fromJson(t as Map<String, dynamic>)).toList();
  }

  Future<Task> getTask(String id) async {
    final response = await _dio.get('/api/v1/tasks/$id');
    return Task.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Task> createTask({
    required String title,
    String? sessionId,
  }) async {
    final response = await _dio.post('/api/v1/tasks', data: {
      'name': title,
      if (sessionId != null) 'session_id': sessionId,
    });
    return Task.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Task> updateTask(String id, {String? name, String? state}) async {
    final body = <String, dynamic>{};
    if (name != null) body['name'] = name;
    if (state != null) body['state'] = state;
    final response = await _dio.put('/api/v1/tasks/$id', data: body);
    return Task.fromJson(response.data as Map<String, dynamic>);
  }

  Future<void> deleteTask(String id) async {
    await _dio.delete('/api/v1/tasks/$id');
  }

  Future<Map<String, dynamic>> cancelTask(String id) async {
    final response = await _dio.post('/api/v1/tasks/$id/cancel');
    return response.data as Map<String, dynamic>;
  }

  // ===== Queue / Jobs =====

  Future<List<Job>> listJobs({String? agentId}) async {
    final response = await _dio.get('/api/v1/queue/jobs', queryParameters: {
      if (agentId != null) 'agent_id': agentId,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['jobs'] as List?;
    if (raw == null) return [];
    return raw.map((j) => Job.fromJson(j as Map<String, dynamic>)).toList();
  }

  Future<Map<String, dynamic>> getQueueStats() async {
    final response = await _dio.get('/api/v1/queue/stats');
    return response.data as Map<String, dynamic>;
  }

  // ===== Metrics =====

  Future<Map<String, dynamic>> getLiveMetrics() async {
    final response = await _dio.get('/api/v1/metrics/live');
    return response.data as Map<String, dynamic>;
  }

  // ===== Memory =====

  Future<List<Map<String, dynamic>>> queryMemory({
    required String query,
    int limit = 10,
    String? category,
  }) async {
    final response = await _dio.post('/api/v1/memory/query', data: {
      'query': query,
      'limit': limit,
      if (category != null) 'category': category,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['memories'] as List?;
    if (raw == null) return [];
    return raw.map((e) => Map<String, dynamic>.from(e as Map)).toList();
  }

  Future<List<Map<String, dynamic>>> getRecentMemories({int limit = 10}) async {
    final response = await _dio.get('/api/v1/memory/recent', queryParameters: {
      'limit': limit,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['memories'] as List?;
    if (raw == null) return [];
    return raw.map((e) => Map<String, dynamic>.from(e as Map)).toList();
  }

  // ===== Skills =====

  Future<List<Skill>> getSkills({String? category}) async {
    final response = await _dio.get('/api/v1/skills', queryParameters: {
      if (category != null) 'category': category,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['skills'] as List?;
    if (raw == null) return [];
    return raw.map((s) => Skill.fromJson(s as Map<String, dynamic>)).toList();
  }

  Future<SkillUiDescriptor> getSkillUi(String slug) async {
    final response = await _dio.get('/api/v1/skills/$slug/ui');
    return SkillUiDescriptor.fromJson(response.data as Map<String, dynamic>);
  }

  Future<SkillExecuteResult> executeSkill({
    required String slug,
    required String prompt,
  }) async {
    final response = await _dio.post('/api/v1/skills/$slug/execute', data: {
      'prompt': prompt,
    });
    return SkillExecuteResult.fromJson(response.data as Map<String, dynamic>);
  }

  Future<SkillExecuteResult> executeSkillWithParams({
    required String slug,
    required Map<String, dynamic> params,
  }) async {
    final response = await _dio.post('/api/v1/skills/$slug/execute', data: params);
    return SkillExecuteResult.fromJson(response.data as Map<String, dynamic>);
  }

  // ===== Search =====

  Future<SearchResults> search({
    required String query,
    SearchScope scope = SearchScope.all,
  }) async {
    final data = <String, dynamic>{'query': query};
    if (scope != SearchScope.all) {
      data['scope'] = scope.name;
    }
    final response = await _dio.post('/api/v1/search', data: data);
    return SearchResults.fromJson(response.data as Map<String, dynamic>);
  }

  // ===== Projects / Branches =====

  Future<List<BranchInfo>> listBranches(String projectId) async {
    final response = await _dio.get('/api/v1/projects/$projectId/branches');
    final data = response.data as List<dynamic>;
    return data.map((b) => BranchInfo.fromJson(b as Map<String, dynamic>)).toList();
  }

  Future<void> checkoutBranch(String projectId, String branch) async {
    await _dio.post('/api/v1/projects/$projectId/checkout', data: {'branch': branch});
  }

  // ===== Plans =====

  Future<List<Plan>> listPlans({String? projectId, int limit = 50}) async {
    final response = await _dio.get('/api/v1/plans', queryParameters: {
      if (projectId != null) 'project_id': projectId,
      'limit': limit,
    });
    final data = response.data as Map<String, dynamic>;
    final raw = data['plans'] as List?;
    if (raw == null) return [];
    return raw.map((p) => Plan.fromJson(p as Map<String, dynamic>)).toList();
  }

  Future<Plan> getPlan(String id) async {
    final response = await _dio.get('/api/v1/plans/$id');
    return Plan.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Plan> approvePlan(String id, {String? sessionID, String? by}) async {
    final response = await _dio.post('/api/v1/plans/$id/approve', data: {
      if (sessionID != null) 'session_id': sessionID,
      if (by != null) 'by': by,
    });
    return Plan.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Plan> rejectPlan(String id, {String? sessionID, String? by, String? reason}) async {
    final response = await _dio.post('/api/v1/plans/$id/reject', data: {
      if (sessionID != null) 'session_id': sessionID,
      if (by != null) 'by': by,
      if (reason != null) 'reason': reason,
    });
    return Plan.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Plan> confirmPlan(String id, {String? sessionID, String? by}) async {
    final response = await _dio.post('/api/v1/plans/$id/confirm', data: {
      if (sessionID != null) 'session_id': sessionID,
      if (by != null) 'by': by,
    });
    return Plan.fromJson(response.data as Map<String, dynamic>);
  }

  Future<Plan> revisePlan(String id, {String? sessionID, String? feedback}) async {
    final response = await _dio.post('/api/v1/plans/$id/revise', data: {
      if (sessionID != null) 'session_id': sessionID,
      if (feedback != null) 'feedback': feedback,
    });
    return Plan.fromJson(response.data as Map<String, dynamic>);
  }

  // ===== Config Files =====

  Future<String> getClientConfig() async {
    final response = await _dio.get('/api/v1/config/client');
    final data = response.data as Map<String, dynamic>;
    return data['content'] as String? ?? '';
  }

  Future<void> saveClientConfig(String content) async {
    await _dio.post('/api/v1/config/client', data: {'content': content});
  }

  Future<String> getModelsConfig() async {
    final response = await _dio.get('/api/v1/config/models');
    final data = response.data as Map<String, dynamic>;
    return data['content'] as String? ?? '';
  }

  Future<void> saveModelsConfig(String content) async {
    await _dio.post('/api/v1/config/models', data: {'content': content});
  }

  Future<String> getMenubarConfig() async {
    final response = await _dio.get('/api/v1/config/menubar');
    final data = response.data as Map<String, dynamic>;
    return data['content'] as String? ?? '';
  }

  Future<void> saveMenubarConfig(String content) async {
    await _dio.post('/api/v1/config/menubar', data: {'content': content});
  }

  // ===== Terminal =====

  Future<Map<String, dynamic>> getTerminalHistory() async {
    final response = await _dio.get('/api/v1/terminal/history');
    return response.data as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> executeCommand(String command) async {
    final response = await _dio.post('/api/v1/terminal/exec', data: {'command': command});
    return response.data as Map<String, dynamic>;
  }

  Future<void> clearTerminalHistory() async {
    await _dio.post('/api/v1/terminal/clear');
  }

  // ===== Calendar =====

  Future<Map<String, dynamic>> getCalendarToday() async {
    final response = await _dio.get('/api/v1/calendar/today');
    return response.data as Map<String, dynamic>;
  }

  Future<void> createCalendarEvent({
    required String summary,
    required DateTime start,
    required DateTime end,
    String? description,
  }) async {
    await _dio.post('/api/v1/calendar/events', data: {
      'summary': summary,
      'start': start.toIso8601String(),
      'end': end.toIso8601String(),
      if (description != null) 'description': description,
    });
  }
}
