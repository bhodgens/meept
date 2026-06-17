import 'dart:io' show HttpClient, X509Certificate;
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import 'package:meept_client/api.dart' as sdk;

import '../core/constants.dart';
import 'daemon_cert_pinner.dart';

/// HTTP client that uses the generated OpenAPI SDK models for request
/// serialization and response deserialization.
///
/// Wraps a pre-configured [Dio] instance that carries the base URL,
/// timeouts, TLS config (via [DaemonCertPinner]), and auth header.
/// All request bodies are produced via SDK model [toJson] and all
/// response bodies are consumed via SDK model [fromJson].
///
/// Import convention:
/// ```dart
/// import 'package:meept_client/api.dart' as sdk;
/// ```
class SdkApiClient {
  final Dio _dio;
  final String baseUrl;

  // ------------------------------------------------------------------
  // Construction
  // ------------------------------------------------------------------

  SdkApiClient({
    required String host,
    int? port,
    String? apiKey,
  })  : baseUrl = 'https://${host}:${port ?? AppConstants.defaultApiPort}',
        _dio = Dio(
          BaseOptions(
            baseUrl: 'https://${host}:${port ?? AppConstants.defaultApiPort}',
            connectTimeout: AppConstants.connectionTimeout,
            receiveTimeout: AppConstants.receiveTimeout,
            headers: {
              'Content-Type': 'application/json',
              if (apiKey != null) 'Authorization': 'Bearer $apiKey',
            },
          ),
        ) {
    // Configure TLS with certificate pinning (reuse existing DaemonCertPinner).
    _dio.httpClientAdapter = IOHttpClientAdapter(
      createHttpClient: () {
        final client = HttpClient();
        client.badCertificateCallback =
            (X509Certificate cert, String host, int port) =>
                DaemonCertPinner.validateCert(cert, host);
        return client;
      },
    );

    // Log all errors for debugging.
    _dio.interceptors.add(LogInterceptor(
      requestHeader: false,
      responseHeader: false,
      requestBody: false,
      responseBody: false,
      error: true,
      logPrint: (obj) => debugPrint('[sdk-http] $obj'),
    ));
  }

  /// Returns the underlying Dio instance for advanced usage (e.g. interceptors).
  Dio get dio => _dio;

  /// Returns the fully-qualified base URL (without trailing slash or /api/v1 prefix).
  String get buildBaseUrl => baseUrl;

  /// Dispose the underlying Dio/HttpClient resources.
  void dispose() {
    _dio.close(force: true);
  }

  // ------------------------------------------------------------------
  // Error handling
  // ------------------------------------------------------------------

  SdkApiException _handleError(DioException e) {
    final statusCode = e.response?.statusCode;
    final rawResponse = e.response?.data;
    String? serverMessage;
    if (rawResponse is Map) {
      serverMessage =
          rawResponse['message'] as String? ?? rawResponse['error'] as String?;
    }

    String message;
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
        message = 'Connection timeout -- is the daemon running?';
        break;
      case DioExceptionType.connectionError:
        message = 'Cannot connect to daemon at $baseUrl';
        break;
      case DioExceptionType.badResponse:
        switch (statusCode) {
          case 401:
            message = serverMessage ?? 'missing API token -- configure in settings';
            break;
          case 418:
            message = serverMessage ?? 'invalid API token (HTTP 418)';
            break;
          case 426:
            message = serverMessage ?? 'Use HTTPS for this endpoint (HTTP 426)';
            break;
          default:
            message = serverMessage ?? 'Server error: $statusCode';
        }
        break;
      case DioExceptionType.cancel:
        message = 'Request cancelled';
        break;
      case DioExceptionType.unknown:
        message = 'Network error -- check your connection';
        break;
      default:
        message = e.message ?? 'Unknown error';
    }

    return SdkApiException(
      message: message,
      statusCode: statusCode ?? 0,
      response: rawResponse,
    );
  }

  // ------------------------------------------------------------------
  // Raw HTTP helpers (used by typed methods below)
  // ------------------------------------------------------------------

  Future<Map<String, dynamic>> _get(String path,
      {Map<String, dynamic>? query}) async {
    try {
      final response = await _dio.get(
        path,
        queryParameters: query,
      );
      return response.data as Map<String, dynamic>;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> _post(String path,
      {Map<String, dynamic>? body, Map<String, dynamic>? query}) async {
    try {
      final response = await _dio.post(
        path,
        data: body,
        queryParameters: query,
      );
      return response.data as Map<String, dynamic>;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<void> _put(String path,
      {Map<String, dynamic>? body, Map<String, dynamic>? query}) async {
    try {
      await _dio.put(
        path,
        data: body,
        queryParameters: query,
      );
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<void> _delete(String path) async {
    try {
      await _dio.delete(path);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }
}

/// Exception thrown by [SdkApiClient].
class SdkApiException implements Exception {
  final String message;
  final int statusCode;
  final dynamic response;

  SdkApiException({
    required this.message,
    required this.statusCode,
    this.response,
  });

  @override
  String toString() => 'SdkApiException: $message (HTTP $statusCode)';
}

// ====================================================================
// TYPED ENDPOINT LAYER -- uses SDK models for serialization
// ====================================================================

/// Extension on [SdkApiClient] that provides fully-typed methods using
/// the generated SDK models.  Each method converts between the UI-facing
/// freezed/Dart types and the SDK models.
extension SdkApiEndpoints on SdkApiClient {
  // ===== Health =====

  Future<sdk.DaemonStatus?> healthCheck() async {
    try {
      final response = await _dio.get('${_dio.options.baseUrl}/health');
      final raw = response.data as Map<String, dynamic>?;
      if (raw == null) return null;
      return sdk.DaemonStatus.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Daemon =====

  Future<sdk.DaemonStatus?> getDaemonStatus() async {
    try {
      final raw = await _get('/api/v1/daemon/status');
      // Status lives inside the response map.
      return sdk.DaemonStatus.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Chat =====

  Future<sdk.ChatResponse> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    final req = sdk.ChatRequest(
      message: message,
      conversationId: conversationId ?? '',
      agentIdCommaOmitempty: agentId,
    );

    final raw = await _post('/api/v1/chat', body: req.toJson());
    return sdk.ChatResponse.fromJson(raw);
  }

  Future<sdk.ChatResponse> sendSteerMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final req = sdk.SteerRequest(
      message: message,
      conversationId: conversationId,
      sourceCommaOmitempty: source,
    );

    final raw = await _post('/api/v1/chat/steer', body: req.toJson());
    return sdk.ChatResponse.fromJson(raw);
  }

  Future<sdk.ChatResponse> sendFollowUpMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final req = sdk.FollowUpRequest(
      message: message,
      conversationId: conversationId,
      sourceCommaOmitempty: source,
    );

    final raw = await _post('/api/v1/chat/followup', body: req.toJson());
    return sdk.ChatResponse.fromJson(raw);
  }

  // ===== Sessions =====

  Future<List<sdk.ListSessionsRequest>> listSessions({int? limit}) async {
    final query = <String, dynamic>{};
    if (limit != null) query['limit'] = limit;

    final raw = await _get('/api/v1/sessions', query: query);
    final sessionsRaw = raw['sessions'] as List?;
    if (sessionsRaw == null) return [];
    return sessionsRaw
        .whereType<Map>()
        .map((s) => s.cast<String, dynamic>())
        .map(sdk.Session.fromJson)
        .toList();
  }

  Future<sdk.Session?> getSession(String id) async {
    try {
      final raw = await _get('/api/v1/sessions/$id');
      return sdk.Session.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<List<sdk.ChatMessage>> getMessages(String id,
      {int offset = 0, int limit = 1000}) async {
    try {
      final raw = await _get('/api/v1/sessions/$id/messages', query: {
        'offset': offset,
        'limit': limit,
      });
      final messagesRaw = raw['messages'] as List?;
      if (messagesRaw == null) return [];
      return messagesRaw
          .whereType<Map>()
          .map((m) => m.cast<String, dynamic>())
          .map((m) => sdk.ChatMessage.fromBackendMessage(m))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Session> createSession({
    required String title,
    String? agentId,
  }) async {
    final req = sdk.CreateSessionRequest(
      nameCommaOmitempty: title,
    );

    final raw = await _post('/api/v1/sessions', body: req.toJson());
    return sdk.Session.fromJson(raw);
  }

  Future<void> deleteSession(String id) async {
    await _delete('/api/v1/sessions/$id');
  }

  Future<List<sdk.Plan>> listPlansBySession(String sessionId) async {
    try {
      final raw = await _get('/api/v1/sessions/$sessionId/plans');
      final plansRaw = raw['plans'] as List?;
      if (plansRaw == null) return [];
      return plansRaw
          .whereType<Map>()
          .map((p) => p.cast<String, dynamic>())
          .map((p) => sdk.Plan.fromJson(p))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Agents =====

  Future<List<sdk.Agent>> listAgents() async {
    try {
      final raw = await _get('/api/v1/config/agents');
      final agentsRaw = raw['agents'] as List?;
      if (agentsRaw == null) return [];
      return agentsRaw
          .whereType<Map>()
          .map((a) => a.cast<String, dynamic>())
          .map(sdk.Agent.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Agent> updateAgent(String id, Map<String, dynamic> config) async {
    final raw = await _post('/api/v1/config/agents/$id', body: config);
    return sdk.Agent.fromJson(raw);
  }

  // ===== Tasks =====

  Future<List<sdk.Task>> listTasks({String? sessionId}) async {
    try {
      final query = <String, dynamic>{};
      if (sessionId != null) query['session_id'] = sessionId;
      final raw = await _get('/api/v1/tasks', query: query);
      final tasksRaw = raw['tasks'] as List?;
      if (tasksRaw == null) return [];
      return tasksRaw
          .whereType<Map>()
          .map((t) => t.cast<String, dynamic>())
          .map((t) => sdk.Task.fromJson(t))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Task?> getTask(String id) async {
    try {
      final raw = await _get('/api/v1/tasks/$id');
      return sdk.Task.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Task> createTask({
    required String title,
    String? sessionId,
  }) async {
    final req = sdk.CreateTaskRequest(
      name: title,
      sessionIdCommaOmitempty: sessionId,
    );

    final raw = await _post('/api/v1/tasks', body: req.toJson());
    return sdk.Task.fromJson(raw);
  }

  Future<sdk.Task> updateTask(String id, {String? name, String? state}) async {
    final req = sdk.UpdateTaskRequest(
      id: id,
      nameCommaOmitempty: name,
      stateCommaOmitempty: state,
    );

    final raw = await _putRaw('/api/v1/tasks/$id', body: req.toJson());
    return sdk.Task.fromJson(raw);
  }

  Future<void> deleteTask(String id) async {
    await _delete('/api/v1/tasks/$id');
  }

  Future<void> cancelTask(String id) async {
    try {
      final req = sdk.CancelTaskRequest(id: id);
      final raw = await _post('/api/v1/tasks/$id/cancel', body: req.toJson());
      return raw;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Queue / Jobs =====

  Future<List<sdk.Job>> listJobs({String? agentId}) async {
    try {
      final query = <String, dynamic>{};
      if (agentId != null) query['agent_id'] = agentId;
      final raw = await _get('/api/v1/queue/jobs', query: query);
      final jobsRaw = raw['jobs'] as List?;
      if (jobsRaw == null) return [];
      return jobsRaw
          .whereType<Map>()
          .map((j) => j.cast<String, dynamic>())
          .map(sdk.Job.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> getQueueStats() async {
    final raw = await _get('/api/v1/queue/stats');
    return raw;
  }

  // ===== Metrics =====

  Future<Map<String, dynamic>> getLiveMetrics() async {
    final raw = await _get('/api/v1/metrics/live');
    return raw;
  }

  // ===== Memory =====

  Future<List<sdk.MemoryResult>> queryMemory({
    required String query,
    int limit = 10,
    String? category,
  }) async {
    final req = sdk.MemoryQueryRequest(
      query: query,
      limitCommaOmitempty: limit,
      categoryCommaOmitempty: category,
    );

    final raw = await _post('/api/v1/memory/query', body: req.toJson());
    final memoriesRaw = raw['memories'] as List?;
    if (memoriesRaw == null) return [];
    return memoriesRaw
        .whereType<Map>()
        .map((m) => m.cast<String, dynamic>())
        .map(sdk.MemoryResult.fromJson)
        .toList();
  }

  Future<List<sdk.MemoryResult>> getRecentMemories({int limit = 10}) async {
    try {
      final raw = await _get('/api/v1/memory/recent', query: {'limit': limit});
      final memoriesRaw = raw['memories'] as List?;
      if (memoriesRaw == null) return [];
      return memoriesRaw
          .whereType<Map>()
          .map((m) => m.cast<String, dynamic>())
          .map(sdk.MemoryResult.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Skills =====

  Future<List<sdk.SkillInfo>> getSkills({String? category}) async {
    try {
      final query = <String, dynamic>{};
      if (category != null) query['category'] = category;
      final raw = await _get('/api/v1/skills', query: query);
      final skillsRaw = raw['skills'] as List?;
      if (skillsRaw == null) return [];
      return skillsRaw
          .whereType<Map>()
          .map((s) => s.cast<String, dynamic>())
          .map(sdk.SkillInfo.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.SkillUIDescriptor> getSkillUi(String slug) async {
    try {
      final raw =
          await _get('/api/v1/skills/$slug/ui');
      return sdk.SkillUIDescriptor.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.ExecuteResult> executeSkill({
    required String slug,
    required String prompt,
  }) async {
    final body = <String, dynamic>{'prompt': prompt};
    final raw = await _post('/api/v1/skills/$slug/execute', body: body);
    return sdk.ExecuteResult.fromJson(raw);
  }

  Future<sdk.ExecuteResult> executeSkillWithParams({
    required String slug,
    required Map<String, dynamic> params,
  }) async {
    final raw = await _post('/api/v1/skills/$slug/execute', body: params);
    return sdk.ExecuteResult.fromJson(raw);
  }

  // ===== Search =====

  Future<sdk.SearchResults> search({required String query, String? scope}) async {
    final req = sdk.SearchRequest(
      query: query,
      scopeCommaOmitempty: scope,
    );

    final raw = await _post('/api/v1/search', body: req.toJson());
    return sdk.SearchResults.fromJson(raw);
  }

  // ===== Projects / Branches =====

  Future<List<sdk.Project>> listProjects() async {
    try {
      final raw = await _get('/api/v1/projects');
      final projectsRaw = raw['projects'] as List? ?? [];
      return projectsRaw
          .whereType<Map>()
          .map((p) => p.cast<String, dynamic>())
          .map(sdk.Project.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<List<sdk.BranchInfo>> listBranches(String projectId) async {
    try {
      final raw = await _get('/api/v1/projects/$projectId/branches');
      final branchesRaw = raw['branches'] as List? ?? [];
      return branchesRaw
          .whereType<Map>()
          .map((b) => b.cast<String, dynamic>())
          .map(sdk.BranchInfo.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<void> checkoutBranch(String projectId, String branch) async {
    final body = <String, dynamic>{'branch': branch};
    await _post('/api/v1/projects/$projectId/checkout', body: body);
  }

  // ===== Plans =====

  Future<List<sdk.Plan>> listPlans({String? projectId, int limit = 50}) async {
    try {
      final query = <String, dynamic>{'limit': limit};
      if (projectId != null) query['project_id'] = projectId;
      final raw = await _get('/api/v1/plans', query: query);
      final plansRaw = raw['plans'] as List?;
      if (plansRaw == null) return [];
      return plansRaw
          .whereType<Map>()
          .map((p) => p.cast<String, dynamic>())
          .map(sdk.Plan.fromJson)
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Plan?> getPlan(String id) async {
    try {
      final raw = await _get('/api/v1/plans/$id');
      return sdk.Plan.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Plan?> approvePlan(String id,
      {String? sessionID, String? by}) async {
    final req = sdk.ApprovePlanRequest(
      planId: id,
      sessionId: sessionID ?? '',
      by: by ?? '',
    );
    try {
      final raw = await _post('/api/v1/plans/$id/approve', body: req.toJson());
      return sdk.Plan.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Plan?> rejectPlan(String id,
      {String? sessionID, String? by, String? reason}) async {
    final req = sdk.RejectPlanRequest(
      planId: id,
      sessionId: sessionID ?? '',
      by: by ?? '',
      reasonCommaOmitempty: reason,
    );
    try {
      final raw =
          await _post('/api/v1/plans/$id/reject', body: req.toJson());
      return sdk.Plan.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Plan?> confirmPlan(String id, {String? sessionID, String? by}) async {
    final req = sdk.ConfirmPlanRequest(
      planId: id,
      sessionId: sessionID ?? '',
      by: by ?? '',
    );
    try {
      final raw = await _post('/api/v1/plans/$id/confirm', body: req.toJson());
      return sdk.Plan.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.Plan?> revisePlan(String id,
      {String? sessionID, String? feedback}) async {
    final req = sdk.RevisePlanRequest(
      planId: id,
      sessionId: sessionID ?? '',
      feedback: feedback ?? '',
    );
    try {
      final raw = await _post('/api/v1/plans/$id/revise', body: req.toJson());
      return sdk.Plan.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Config Files =====

  Future<String> getClientConfig() async {
    final raw = await _get('/api/v1/config/client');
    return raw['content'] as String? ?? '';
  }

  Future<void> saveClientConfig(String content) async {
    await _post('/api/v1/config/client', body: {'content': content});
  }

  Future<String> getModelsConfig() async {
    final raw = await _get('/api/v1/config/models');
    return raw['content'] as String? ?? '';
  }

  Future<void> saveModelsConfig(String content) async {
    await _post('/api/v1/config/models', body: {'content': content});
  }

  Future<String> getMenubarConfig() async {
    final raw = await _get('/api/v1/config/menubar');
    return raw['content'] as String? ?? '';
  }

  Future<void> saveMenubarConfig(String content) async {
    await _post('/api/v1/config/menubar', body: {'content': content});
  }

  // ===== Terminal =====

  Future<sdk.CommandHistory> getTerminalHistory() async {
    final raw = await _get('/api/v1/terminal/history');
    // The endpoint returns a wrapped response; check for history key.
    final history = raw['history'] as Map<String, dynamic>? ?? raw;
    return sdk.CommandHistory.fromJson(history);
  }

  Future<sdk.ExecuteResult> executeCommand(String command) async {
    final body = <String, dynamic>{'command': command};
    final raw = await _post('/api/v1/terminal/exec', body: body);
    return sdk.ExecuteResult.fromJson(raw);
  }

  Future<void> clearTerminalHistory() async {
    await _post('/api/v1/terminal/clear');
  }

  // ===== Calendar =====

  Future<sdk.CalendarEvent?> getCalendarToday() async {
    try {
      final raw = await _get('/api/v1/calendar/today');
      // Calendar endpoint may return a list or a single event.
      if (raw is List) {
        if (raw.isNotEmpty) {
          final items = raw.whereType<Map>().toList();
          if (items.isNotEmpty) {
            return sdk.CalendarEvent.fromJson(items[0].cast<String, dynamic>());
          }
        }
        return null;
      }
      return sdk.CalendarEvent.fromJson(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<void> createCalendarEvent({
    required String summary,
    required DateTime start,
    required DateTime end,
    String? description,
  }) async {
    final body = <String, dynamic>{
      'summary': summary,
      'start': start.toIso8601String(),
      'end': end.toIso8601String(),
      if (description != null) 'description': description,
    };
    await _post('/api/v1/calendar/events', body: body);
  }

  // ------------------------------------------------------------------
  // Internal PUT helper (extension doesn't have direct access to _put
  // since it's on SdkApiClient -- we duplicate where needed).
  // ------------------------------------------------------------------

  Future<Map<String, dynamic>> _putRaw(String path,
      {Map<String, dynamic>? body}) async {
    try {
      final response = await _dio.put(path, data: body);
      return response.data as Map<String, dynamic>;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }
}
