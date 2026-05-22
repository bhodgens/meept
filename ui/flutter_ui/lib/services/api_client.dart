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
    return ApiClientException(
      message: e.message ?? 'Unknown error',
      statusCode: e.response?.statusCode ?? 0,
      response: e.response?.data,
    );
  }

  // ===== Chat Endpoints =====

  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
  }) async {
    return post<Map<String, dynamic>>(
      '/chat',
      data: {
        'message': message,
        if (conversationId != null) 'conversation_id': conversationId,
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

  // ===== Health Endpoint =====

  Future<Map<String, dynamic>> healthCheck() async {
    return get<Map<String, dynamic>>('/health');
  }

  // ===== Task Endpoints (write) =====

  Future<void> deleteTask(String id) async {
    await delete('/tasks/$id');
  }

  Future<Task> updateTask(
    String id, {
    String? title,
    String? status,
  }) async {
    final data = <String, dynamic>{};
    if (title != null) data['name'] = title;
    if (status != null) data['state'] = status;
    return post<Task>('/tasks/$id', data: data);
  }

  Future<Task> cancelTask(String id) async {
    final data = await post<Map<String, dynamic>>('/tasks/$id/cancel');
    return Task.fromJson(data);
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
