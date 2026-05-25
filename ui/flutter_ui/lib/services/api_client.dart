import 'package:dio/dio.dart';
import '../core/constants.dart';
import '../models/api_models.dart';

/// API client for Meept HTTP backend
class ApiClient {
  final Dio _dio;
  final String baseUrl;

  ApiClient({
    String? host,
    int? port,
    String? apiKey,
  })  : baseUrl =
            'http://${host ?? AppConstants.defaultApiHost}:${port ?? AppConstants.defaultApiPort}/api/${AppConstants.apiVersion}',
        _dio = Dio(
          BaseOptions(
            baseUrl:
                'http://${host ?? AppConstants.defaultApiHost}:${port ?? AppConstants.defaultApiPort}/api/${AppConstants.apiVersion}',
            connectTimeout: AppConstants.connectionTimeout,
            receiveTimeout: AppConstants.receiveTimeout,
            headers: {
              'Content-Type': 'application/json',
              if (apiKey != null) 'Authorization': 'Bearer $apiKey',
            },
          ),
        );

  /// Generic GET request
  Future<T> get<T>(
    String path, {
    Map<String, dynamic>? queryParameters,
  }) async {
    try {
      final response = await _dio.get(
        path,
        queryParameters: queryParameters,
      );
      return response.data as T;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Generic POST request
  Future<T> post<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    try {
      final response = await _dio.post(
        path,
        data: data,
        queryParameters: queryParameters,
      );
      return response.data as T;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Generic PUT request
  Future<T> put<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
  }) async {
    try {
      final response = await _dio.put(
        path,
        data: data,
        queryParameters: queryParameters,
      );
      return response.data as T;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Generic DELETE request
  Future<T> delete<T>(String path) async {
    try {
      final response = await _dio.delete(path);
      return response.data as T;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  ApiClientException _handleError(DioException e) {
    String message;
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
        message = 'Connection timeout - is the daemon running?';
        break;
      case DioExceptionType.connectionError:
        message = 'Cannot connect to daemon at $baseUrl';
        break;
      case DioExceptionType.badResponse:
        message = 'Server error: ${e.response?.statusCode}';
        break;
      case DioExceptionType.cancel:
        message = 'Request cancelled';
        break;
      case DioExceptionType.unknown:
        message = 'Network error - check your connection';
        break;
      default:
        message = e.message ?? 'Unknown error';
    }
    return ApiClientException(
      message: message,
      statusCode: e.response?.statusCode ?? 0,
      response: e.response?.data,
    );
  }

  // ===== Chat Endpoints =====

  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    return post<Map<String, dynamic>>(
      '/chat',
      data: {
        'message': message,
        if (conversationId != null) 'conversation_id': conversationId,
        if (agentId != null) 'agent_id': agentId,
      },
    );
  }

  // ===== Session Endpoints =====

  Future<List<Session>> listSessions() async {
    final data = await get<Map<String, dynamic>>('/sessions');
    final sessions = (data['sessions'] as List)
        .map((s) => Session.fromJson(s as Map<String, dynamic>))
        .toList();
    return sessions;
  }

  Future<Session> getSession(String id) async {
    final data = await get<Map<String, dynamic>>('/sessions/$id');
    return Session.fromJson(data);
  }

  Future<Session> createSession({
    required String title,
    String? agentId,
  }) async {
    final data = await post<Map<String, dynamic>>(
      '/sessions',
      data: {
        'title': title,
        if (agentId != null) 'agent_id': agentId,
      },
    );
    return Session.fromJson(data);
  }

  Future<void> deleteSession(String id) async {
    await delete('/sessions/$id');
  }

  // ===== Agent Endpoints =====

  Future<List<Agent>> listAgents() async {
    final data = await get<Map<String, dynamic>>('/config/agents');
    final agents = (data['agents'] as List)
        .map((a) => Agent.fromJson(a as Map<String, dynamic>))
        .toList();
    return agents;
  }

  // ===== Task Endpoints =====

  Future<List<Task>> listTasks({String? sessionId}) async {
    final data = await get<Map<String, dynamic>>(
      '/tasks',
      queryParameters: sessionId != null ? {'session_id': sessionId} : null,
    );
    final tasks = (data['tasks'] as List)
        .map((t) => Task.fromJson(t as Map<String, dynamic>))
        .toList();
    return tasks;
  }

  Future<Task> getTask(String id) async {
    final data = await get<Map<String, dynamic>>('/tasks/$id');
    return Task.fromJson(data);
  }

  // ===== Task Endpoints (write) =====

  Future<Task> createTask({
    required String title,
    String? sessionId,
  }) async {
    final data = await post<Map<String, dynamic>>(
      '/tasks',
      data: {
        'name': title,
        if (sessionId != null) 'session_id': sessionId,
      },
    );
    return Task.fromJson(data);
  }

  // ===== Queue Endpoints =====

  Future<List<Job>> listJobs({String? agentId}) async {
    final data = await get<Map<String, dynamic>>(
      '/queue/jobs',
      queryParameters: agentId != null ? {'agent_id': agentId} : null,
    );
    final jobs = (data['jobs'] as List)
        .map((j) => Job.fromJson(j as Map<String, dynamic>))
        .toList();
    return jobs;
  }

  Future<Map<String, dynamic>> getQueueStats() async {
    return get<Map<String, dynamic>>('/queue/stats');
  }

  // ===== Metrics Endpoints =====

  Future<Map<String, dynamic>> getLiveMetrics() async {
    return get<Map<String, dynamic>>('/metrics/live');
  }

  // ===== Memory Endpoints =====

  Future<List<Map<String, dynamic>>> queryMemory({
    required String query,
    int limit = 10,
    String? category,
  }) async {
    final data = await post<Map<String, dynamic>>(
      '/memory/query',
      data: {
        'query': query,
        'limit': limit,
        if (category != null) 'category': category,
      },
    );
    return (data['memories'] as List).cast<Map<String, dynamic>>();
  }

  Future<List<Map<String, dynamic>>> getRecentMemories({
    int limit = 10,
  }) async {
    final data = await get<Map<String, dynamic>>(
      '/memory/recent',
      queryParameters: {'limit': limit},
    );
    return (data['memories'] as List).cast<Map<String, dynamic>>();
  }

  // ===== Skills/Tools Endpoints =====

  Future<List<Skill>> getSkills({String? category}) async {
    final data = await get<Map<String, dynamic>>(
      '/skills',
      queryParameters:
          category != null ? {'category': category} : null,
    );
    final skills = (data['skills'] as List)
        .map((s) => Skill.fromJson(s as Map<String, dynamic>))
        .toList();
    return skills;
  }

  // ===== Health Endpoint =====

  Future<Map<String, dynamic>> healthCheck() async {
    return get<Map<String, dynamic>>('/health');
  }

  // ===== Task Endpoints (write) =====

  Future<void> deleteTask(String id) async {
    await delete('/tasks/$id');
  }

  /// Update a task's state (and optionally name/title) using PUT.
  Future<Task> updateTask(
    String id, {
    String? name,
    String? state,
  }) async {
    final data = <String, dynamic>{};
    if (name != null) data['name'] = name;
    if (state != null) data['state'] = state;
    final resp = await put<Map<String, dynamic>>('/tasks/$id', data: data);
    return Task.fromJson(resp);
  }

  /// Cancel a task via POST /tasks/{id}/cancel.
  /// The backend returns {"status": "cancelled"}, so we return the raw map.
  Future<Map<String, dynamic>> cancelTask(String id) async {
    return post<Map<String, dynamic>>('/tasks/$id/cancel');
  }

  // ===== Daemon Endpoints =====

  Future<Map<String, dynamic>> getDaemonStatus() async {
    return get<Map<String, dynamic>>('/daemon/status');
  }

  // ===== Config/Agent Endpoints =====

  Future<Agent> updateAgent(
    String id,
    Map<String, dynamic> config,
  ) async {
    final data = await post<Map<String, dynamic>>('/config/agents/$id', data: config);
    return Agent.fromJson(data);
  }

  // ===== Config File Endpoints =====

  Future<String> getClientConfig() async {
    final data = await get<Map<String, dynamic>>('/config/client');
    return data['content'] as String? ?? '';
  }

  Future<void> saveClientConfig(String content) async {
    await post('/config/client', data: {'content': content});
  }

  Future<String> getModelsConfig() async {
    final data = await get<Map<String, dynamic>>('/config/models');
    return data['content'] as String? ?? '';
  }

  Future<void> saveModelsConfig(String content) async {
    await post('/config/models', data: {'content': content});
  }

  Future<String> getMenubarConfig() async {
    final data = await get<Map<String, dynamic>>('/config/menubar');
    return data['content'] as String? ?? '';
  }

  Future<void> saveMenubarConfig(String content) async {
    await post('/config/menubar', data: {'content': content});
  }
}

/// API client exception
class ApiClientException implements Exception {
  final String message;
  final int statusCode;
  final dynamic response;

  ApiClientException({
    required this.message,
    required this.statusCode,
    this.response,
  });

  @override
  String toString() => 'ApiClientException: $message (HTTP $statusCode)';
}
