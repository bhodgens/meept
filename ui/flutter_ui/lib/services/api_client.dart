import 'dart:io' show HttpClient, X509Certificate;
import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import 'package:flutter/foundation.dart';
import '../core/constants.dart';
import '../models/api_models.dart';
import 'storage_service.dart';
import 'meept_api.dart';
import 'daemon_cert_pinner.dart';

/// Build the base URL from host and port. HTTPS is mandatory.
///
/// Does NOT include the /api/v1 prefix — endpoint paths in MeeptApi
/// already include it, so adding it here would cause double-prefixing.
String _buildBaseUrl(String? host, int? port) {
  return 'https://${host ?? AppConstants.defaultApiHost}:${port ?? AppConstants.defaultApiPort}';
}

/// API client for Meept HTTP backend (always uses HTTPS).
///
/// Wraps a [Dio] instance and a [MeeptApi] typed client.  All endpoint
/// methods delegate to [MeeptApi] so that the typed interface is the
/// single source of truth.  The legacy generic `get`/`post`/`put`/`delete`
/// methods are retained for backward compatibility with code that bypasses
/// the typed methods.
class ApiClient {
  final Dio _dio;
  final String baseUrl;

  /// Typed API client -- delegates all endpoint calls.
  late final MeeptApi _api;

  /// Create an API client with explicit [host], [port], and [apiKey].
  ///
  /// This constructor exists to allow test subclasses to redirect API
  /// calls without needing a live [StorageService].  Use the
  /// [ApiClient.storage] factory for production code.
  ApiClient({
    String? host,
    int? port,
    String? apiKey,
  })  : baseUrl = _buildBaseUrl(host, port),
        _dio = Dio(
          BaseOptions(
            baseUrl: _buildBaseUrl(host, port),
            connectTimeout: AppConstants.connectionTimeout,
            receiveTimeout: AppConstants.receiveTimeout,
            headers: {
              'Content-Type': 'application/json',
              if (apiKey != null) 'Authorization': 'Bearer $apiKey',
            },
          ),
        ) {
    // Configure Dio with certificate pinning for the daemon's self-signed cert.
    _dio.httpClientAdapter = IOHttpClientAdapter(
      createHttpClient: () {
        final client = HttpClient();
        client.badCertificateCallback =
            (X509Certificate cert, String host, int port) =>
                DaemonCertPinner.validateCert(cert, host);
        return client;
      },
    );

    // Log all requests and errors for debugging.
    _dio.interceptors.add(LogInterceptor(
      requestHeader: false,
      responseHeader: false,
      requestBody: false,
      responseBody: false,
      error: true,
      logPrint: (obj) => debugPrint('[http] $obj'),
    ));

    _api = MeeptApi(_dio);
  }

  /// Create an API client, optionally loading persisted host/port/API key
  /// from [StorageService].  If [storage] is null the client uses the
  /// defaults for host/port and skips API key persistence.
  ///
  /// **Note:** The underlying storage must have been initialized (via
  /// `StorageService.init()` in [main]) before constructing the client.
  factory ApiClient.storage({StorageService? storage}) {
    String? host = AppConstants.defaultApiHost;
    int? port = AppConstants.defaultApiPort;
    String? apiKey;

    if (storage != null) {
      host = storage.getApiHost() ?? host;
      port = storage.getApiPort() ?? port;
      // getApiKey() reads from SharedPreferences (sync) after init()
      // Key writes go to keychain + prefs, so prefs always have the latest
      apiKey = storage.getApiKey();
    }

    return ApiClient(
      host: host,
      port: port,
      apiKey: apiKey,
    );
  }

  /// Expose the underlying typed API client for direct use.
  ///
  /// Providers and widgets that want the fully-typed interface can use
  /// this directly instead of going through the legacy wrapper methods.
  MeeptApi get typed => _api;

  /// Expose the underlying Dio instance for advanced usage (e.g. interceptors).
  Dio get dio => _dio;

  /// Initialize cert pinning by loading the daemon's certificate fingerprint.
  /// Must be called before constructing any ApiClient instances.
  static Future<void> initCertPinning() async {
    await DaemonCertPinner.loadFingerprint();
  }

  /// Dispose the underlying Dio/HttpClient resources.
  ///
  /// Must be called when the client is no longer needed to prevent
  /// resource leaks (open HTTP connections, connection pools, etc.).
  void dispose() {
    _dio.close(force: true);
  }

  // ===== Generic CRUD (retained for backward compat) =====

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
    final statusCode = e.response?.statusCode;
    final responseData = e.response?.data;
    String? serverMessage;
    if (responseData is Map) {
      serverMessage = responseData['message'] as String? ?? responseData['error'] as String?;
    }

    String message;
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
        message = 'Connection timeout - is the daemon running?';
        break;
      case DioExceptionType.connectionError:
        message = 'Cannot connect to daemon at $baseUrl';
        break;
      case DioExceptionType.badResponse:
        switch (statusCode) {
          case 401:
            message = serverMessage ?? 'missing API token — configure in settings';
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
        message = 'Network error - check your connection';
        break;
      default:
        message = e.message ?? 'Unknown error';
    }
    return ApiClientException(
      message: message,
      statusCode: statusCode ?? 0,
      response: responseData,
    );
  }

  // ===== Chat Endpoints =====

  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    required String conversationId,
    String? agentId,
  }) => _api.sendChatMessage(
    message: message,
    conversationId: conversationId,
    agentId: agentId,
  );

  Future<Map<String, dynamic>> sendSteerMessage({
    required String message,
    required String conversationId,
    String? source,
  }) => _api.sendSteerMessage(
    message: message,
    conversationId: conversationId,
    source: source,
  );

  Future<Map<String, dynamic>> sendFollowUpMessage({
    required String message,
    required String conversationId,
    String? source,
  }) => _api.sendFollowUpMessage(
    message: message,
    conversationId: conversationId,
    source: source,
  );

  // ===== Session Endpoints =====

  Future<List<Session>> listSessions() => _api.listSessions();

  Future<Session> getSession(String id) => _api.getSession(id);

  Future<List<ChatMessage>> getMessages(String id,
          {int offset = 0, int limit = 1000}) =>
      _api.getMessages(id, offset: offset, limit: limit);

  Future<Session> createSession({
    required String title,
    String? agentId,
  }) => _api.createSession(title: title, agentId: agentId);

  Future<void> deleteSession(String id) => _api.deleteSession(id);

  // ===== Agent Endpoints =====

  Future<List<Agent>> listAgents() => _api.listAgents();

  Future<Agent> updateAgent(String id, Map<String, dynamic> config) =>
      _api.updateAgent(id, config);

  // ===== Task Endpoints =====

  Future<List<Task>> listTasks({String? sessionId}) =>
      _api.listTasks(sessionId: sessionId);

  Future<Task> getTask(String id) => _api.getTask(id);

  Future<Task> createTask({
    required String title,
    String? sessionId,
  }) => _api.createTask(title: title, sessionId: sessionId);

  Future<Task> updateTask(String id, {String? name, String? state}) =>
      _api.updateTask(id, name: name, state: state);

  Future<void> deleteTask(String id) => _api.deleteTask(id);

  Future<Map<String, dynamic>> cancelTask(String id) => _api.cancelTask(id);

  // ===== Queue Endpoints =====

  Future<List<Job>> listJobs({String? agentId}) =>
      _api.listJobs(agentId: agentId);

  Future<Map<String, dynamic>> getQueueStats() => _api.getQueueStats();

  // ===== Metrics Endpoints =====

  Future<Map<String, dynamic>> getLiveMetrics() => _api.getLiveMetrics();

  // ===== Memory Endpoints =====

  Future<List<Map<String, dynamic>>> queryMemory({
    required String query,
    int limit = 10,
    String? category,
  }) => _api.queryMemory(query: query, limit: limit, category: category);

  Future<List<Map<String, dynamic>>> getRecentMemories({int limit = 10}) =>
      _api.getRecentMemories(limit: limit);

  // ===== Skills/Tools Endpoints =====

  Future<List<Skill>> getSkills({String? category}) =>
      _api.getSkills(category: category);

  Future<SkillUiDescriptor> getSkillUi(String slug) => _api.getSkillUi(slug);

  Future<SkillExecuteResult> executeSkill({
    required String slug,
    required String prompt,
  }) => _api.executeSkill(slug: slug, prompt: prompt);

  Future<SkillExecuteResult> executeSkillWithParams({
    required String slug,
    required Map<String, dynamic> params,
  }) => _api.executeSkillWithParams(slug: slug, params: params);

  // ===== Health Endpoint =====

  Future<Map<String, dynamic>> healthCheck() => _api.healthCheck();

  // ===== Plan Endpoints =====

  Future<List<Plan>> listPlans({String? projectID, int limit = 50}) =>
      _api.listPlans(projectId: projectID, limit: limit);

  Future<Plan> getPlan(String id) => _api.getPlan(id);

  Future<List<Plan>> listPlansBySession(String sessionID) =>
      _api.listPlansBySession(sessionID);

  Future<Plan> approvePlan(String id, {String? sessionID, String? by}) =>
      _api.approvePlan(id, sessionID: sessionID, by: by);

  Future<Plan> rejectPlan(String id,
          {String? sessionID, String? by, String? reason}) =>
      _api.rejectPlan(id, sessionID: sessionID, by: by, reason: reason);

  Future<Plan> confirmPlan(String id, {String? sessionID, String? by}) =>
      _api.confirmPlan(id, sessionID: sessionID, by: by);

  Future<Plan> revisePlan(String id,
          {String? sessionID, String? feedback}) =>
      _api.revisePlan(id, sessionID: sessionID, feedback: feedback);

  // ===== Daemon Endpoints =====

  Future<Map<String, dynamic>> getDaemonStatus() => _api.getDaemonStatus();

  // ===== Config File Endpoints =====

  Future<String> getClientConfig() => _api.getClientConfig();

  Future<void> saveClientConfig(String content) =>
      _api.saveClientConfig(content);

  Future<String> getModelsConfig() => _api.getModelsConfig();

  Future<void> saveModelsConfig(String content) =>
      _api.saveModelsConfig(content);

  Future<String> getMenubarConfig() => _api.getMenubarConfig();

  Future<void> saveMenubarConfig(String content) =>
      _api.saveMenubarConfig(content);

  // ===== Search Endpoints =====

  Future<SearchResults> search({
    required String query,
    SearchScope scope = SearchScope.all,
  }) => _api.search(query: query, scope: scope);

  // ===== Project/Branch Endpoints =====

  Future<List<Project>> listProjects() => _api.listProjects();

  Future<List<BranchInfo>> listBranches(String projectId) =>
      _api.listBranches(projectId);

  Future<void> checkoutBranch(String projectId, String branch) =>
      _api.checkoutBranch(projectId, branch);

  // ===== Terminal Endpoints =====

  Future<Map<String, dynamic>> getTerminalHistory() =>
      _api.getTerminalHistory();

  Future<Map<String, dynamic>> executeCommand(String command) =>
      _api.executeCommand(command);

  Future<void> clearTerminalHistory() => _api.clearTerminalHistory();

  // ===== Calendar Endpoints =====

  Future<Map<String, dynamic>> getCalendarToday() => _api.getCalendarToday();

  Future<void> createCalendarEvent({
    required String summary,
    required DateTime start,
    required DateTime end,
    String? description,
  }) => _api.createCalendarEvent(
    summary: summary,
    start: start,
    end: end,
    description: description,
  );
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
