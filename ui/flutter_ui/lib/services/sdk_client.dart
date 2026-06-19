import 'dart:io' show HttpClient, X509Certificate;
import 'package:built_value/serializer.dart';
import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import 'package:flutter/foundation.dart';

import 'package:meept_client/meept_client.dart' as sdk;

import '../core/constants.dart';
import '../models/api_models.dart' show SearchResults, SemanticSearchResults, SearchScope, SearchScopeX;
import 'daemon_cert_pinner.dart';
import 'storage_service.dart';

/// HTTP client that uses the generated OpenAPI SDK models for request
/// serialization and response deserialization.
///
/// Wraps a pre-configured [Dio] instance that carries the base URL,
/// timeouts, TLS config (via [DaemonCertPinner]), and auth header.
/// Request bodies are produced via the built_value serializers exposed by
/// the generated `meept_client` package (dart-dio generator), and response
/// bodies are consumed via the same serializers where the SDK provides a
/// type.
///
/// For endpoints whose response schema is not modeled in the OpenAPI spec
/// (Session, Task, Plan, Agent, Job, Project, BranchInfo, ChatMessage),
/// the method returns the raw `Map<String, dynamic>` and the caller is
/// expected to deserialize via the appropriate local `fromJson`.
///
/// Import convention:
/// ```dart
/// import 'package:meept_client/meept_client.dart' as sdk;
/// ```
class SdkApiClient {
  final Dio _dio;
  final String baseUrl;

  /// Built_value serializers exposed by the generated SDK (with the
  /// StandardJsonPlugin already applied via [sdk.standardSerializers]).
  /// Used to serialize request bodies and deserialize responses for
  /// endpoints whose payload has a generated model.
  static Serializers get _serializers => sdk.standardSerializers;

  /// Serialize a built_value model to a JSON-compatible [Map].
  static Map<String, dynamic> _toJson<T>(T model) {
    final serialized = _serializers.serialize(model,
        specifiedType: FullType(T));
    return Map<String, dynamic>.from(serialized as Map);
  }

  /// Deserialize a JSON [Map] into a built_value model of type [T].
  static T? _fromJson<T>(Map<String, dynamic> raw) {
    final result = _serializers.deserialize(raw,
        specifiedType: FullType(T));
    return result as T?;
  }

  // ------------------------------------------------------------------
  // Construction
  // ------------------------------------------------------------------

  SdkApiClient({
    required String host,
    int? port,
    String? apiKey,
  })  : baseUrl = 'https://$host:${port ?? AppConstants.defaultApiPort}',
        _dio = Dio(
          BaseOptions(
            baseUrl: 'https://$host:${port ?? AppConstants.defaultApiPort}',
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

  /// Create an SDK-backed client, optionally loading persisted host/port/API
  /// key from [StorageService].
  ///
  /// **Note:** The underlying storage must have been initialized (via
  /// `StorageService.init()` in `main`) before constructing the client.
  factory SdkApiClient.storage({StorageService? storage}) {
    String? host = AppConstants.defaultApiHost;
    int? port = AppConstants.defaultApiPort;
    String? apiKey;

    if (storage != null) {
      host = storage.getApiHost() ?? host;
      port = storage.getApiPort() ?? port;
      apiKey = storage.getApiKey();
    }

    // Fallback to default dev API key if not configured.
    if ((apiKey == null || apiKey.isEmpty) &&
        AppConstants.defaultApiKey.isNotEmpty) {
      apiKey = AppConstants.defaultApiKey;
    }

    return SdkApiClient(host: host, port: port, apiKey: apiKey);
  }

  /// Initialize cert pinning by loading the daemon's certificate fingerprint.
  /// Must be called before constructing any SdkApiClient instances.
  ///
  /// Delegates to [DaemonCertPinner] which loads the fingerprint exactly once.
  static Future<void> initCertPinning() async {
    await DaemonCertPinner.loadFingerprint();
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

  Future<Map<String, dynamic>> _put(String path,
      {Map<String, dynamic>? body, Map<String, dynamic>? query}) async {
    try {
      final response = await _dio.put(
        path,
        data: body,
        queryParameters: query,
      );
      return response.data as Map<String, dynamic>;
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

  // ==================================================================
  // Typed endpoint layer -- uses SDK models for serialization.
  //
  // These methods live on the class (rather than in an extension) so
  // that test suites can subclass [SdkApiClient] and override the
  // endpoint methods with stub implementations.  They call into the
  // private `_get`/`_post`/`_put`/`_delete` helpers above, which means
  // overrides don't need to touch the Dio transport.
  // ==================================================================

  // ===== Health =====

  Future<sdk.DaemonStatus?> healthCheck() async {
    try {
      final response = await _dio.get('/health');
      final raw = response.data as Map<String, dynamic>?;
      if (raw == null) return null;
      return _fromJson<sdk.DaemonStatus>(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Daemon =====

  Future<sdk.DaemonStatus?> getDaemonStatus() async {
    try {
      final raw = await _get('/api/v1/daemon/status');
      return _fromJson<sdk.DaemonStatus>(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw `/api/v1/daemon/status` JSON for callers that need
  /// to inspect fields (e.g. `state`, `pid`, `uptime`) as dynamic values
  /// without going through the typed [sdk.DaemonStatus] model.
  Future<Map<String, dynamic>> getDaemonStatusRaw() async {
    return _get('/api/v1/daemon/status');
  }

  // ===== Chat =====

  /// Sends a chat message and returns the raw JSON response map.
  ///
  /// Returns the raw map rather than a typed [sdk.ChatResponse] because the
  /// chat provider needs to inspect the `error` field which is not modeled
  /// in the OpenAPI spec's ChatResponse schema (the field only appears when
  /// the agent fails before producing a reply).
  Future<Map<String, dynamic>> sendChatMessage({
    required String message,
    String? conversationId,
    String? agentId,
  }) async {
    final req = sdk.ChatRequest((b) => b
      ..message = message
      ..conversationId = conversationId ?? ''
      ..agentIdCommaOmitempty = agentId);

    return _post('/api/v1/chat', body: _toJson(req));
  }

  /// Sends a steering message and returns the raw JSON response map.
  Future<Map<String, dynamic>> sendSteerMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final req = sdk.SteerRequest((b) => b
      ..message = message
      ..conversationId = conversationId
      ..sourceCommaOmitempty = source);

    return _post('/api/v1/chat/steer', body: _toJson(req));
  }

  /// Sends a follow-up message and returns the raw JSON response map.
  Future<Map<String, dynamic>> sendFollowUpMessage({
    required String message,
    required String conversationId,
    String? source,
  }) async {
    final req = sdk.FollowUpRequest((b) => b
      ..message = message
      ..conversationId = conversationId
      ..sourceCommaOmitempty = source);

    return _post('/api/v1/chat/followup', body: _toJson(req));
  }

  // ===== Sessions =====

  /// Returns the raw `sessions` array from `/api/v1/sessions`.
  ///
  /// The SDK does not currently model the Session entity because the
  /// OpenAPI spec leaves the response shape untyped; callers must
  /// deserialize each entry via the local `Session.fromJson`.
  Future<List<Map<String, dynamic>>> listSessions({int? limit}) async {
    final query = <String, dynamic>{};
    if (limit != null) query['limit'] = limit;

    final raw = await _get('/api/v1/sessions', query: query);
    final sessionsRaw = raw['sessions'] as List?;
    if (sessionsRaw == null) return [];
    return sessionsRaw
        .whereType<Map>()
        .map((s) => Map<String, dynamic>.from(s))
        .toList();
  }

  /// Returns the raw session JSON for `/api/v1/sessions/{id}`.
  Future<Map<String, dynamic>> getSession(String id) async {
    try {
      return await _get('/api/v1/sessions/$id');
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw `messages` array for `/api/v1/sessions/{id}/messages`.
  ///
  /// Each entry should be passed to the local `ChatMessage.fromBackendMessage`.
  Future<List<Map<String, dynamic>>> getMessages(String id,
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
          .map((m) => Map<String, dynamic>.from(m))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Creates a session and returns the raw JSON.
  Future<Map<String, dynamic>> createSession({
    required String title,
    String? agentId,
  }) async {
    final req = sdk.CreateSessionRequest((b) => b
      ..nameCommaOmitempty = title);

    final raw = await _post('/api/v1/sessions', body: _toJson(req));
    return raw;
  }

  Future<void> deleteSession(String id) async {
    await _delete('/api/v1/sessions/$id');
  }

  /// Returns the raw `plans` array for `/api/v1/sessions/{sessionId}/plans`.
  Future<List<Map<String, dynamic>>> listPlansBySession(String sessionId) async {
    try {
      final raw = await _get('/api/v1/sessions/$sessionId/plans');
      final plansRaw = raw['plans'] as List?;
      if (plansRaw == null) return [];
      return plansRaw
          .whereType<Map>()
          .map((p) => Map<String, dynamic>.from(p))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Agents =====

  /// Returns the raw `agents` array.  Callers deserialize via `Agent.fromJson`.
  Future<List<Map<String, dynamic>>> listAgents() async {
    try {
      final raw = await _get('/api/v1/config/agents');
      final agentsRaw = raw['agents'] as List?;
      if (agentsRaw == null) return [];
      return agentsRaw
          .whereType<Map>()
          .map((a) => Map<String, dynamic>.from(a))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Updates an agent and returns the raw JSON.
  Future<Map<String, dynamic>> updateAgent(
      String id, Map<String, dynamic> config) async {
    final raw = await _post('/api/v1/config/agents/$id', body: config);
    return raw;
  }

  // ===== Tasks =====

  /// Returns the raw `tasks` array.  Callers deserialize via `Task.fromJson`.
  Future<List<Map<String, dynamic>>> listTasks({String? sessionId}) async {
    try {
      final query = <String, dynamic>{};
      if (sessionId != null) query['session_id'] = sessionId;
      final raw = await _get('/api/v1/tasks', query: query);
      final tasksRaw = raw['tasks'] as List?;
      if (tasksRaw == null) return [];
      return tasksRaw
          .whereType<Map>()
          .map((t) => Map<String, dynamic>.from(t))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw task JSON.  Callers deserialize via `Task.fromJson`.
  Future<Map<String, dynamic>> getTask(String id) async {
    try {
      return await _get('/api/v1/tasks/$id');
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Creates a task and returns the raw JSON.
  Future<Map<String, dynamic>> createTask({
    required String title,
    String? sessionId,
  }) async {
    final req = sdk.CreateTaskRequest((b) => b
      ..name = title
      ..sessionIdCommaOmitempty = sessionId);

    final raw = await _post('/api/v1/tasks', body: _toJson(req));
    return raw;
  }

  /// Updates a task and returns the raw JSON.
  Future<Map<String, dynamic>> updateTask(String id,
      {String? name, String? state}) async {
    final req = sdk.UpdateTaskRequest((b) => b
      ..id = id
      ..nameCommaOmitempty = name
      ..stateCommaOmitempty = state);

    final raw = await _put('/api/v1/tasks/$id', body: _toJson(req));
    return raw;
  }

  Future<void> deleteTask(String id) async {
    await _delete('/api/v1/tasks/$id');
  }

  Future<void> cancelTask(String id) async {
    try {
      final req = sdk.CancelTaskRequest((b) => b..id = id);
      await _post('/api/v1/tasks/$id/cancel', body: _toJson(req));
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  // ===== Queue / Jobs =====

  /// Returns the raw `jobs` array.  Callers deserialize via `Job.fromJson`.
  Future<List<Map<String, dynamic>>> listJobs({String? agentId}) async {
    try {
      final query = <String, dynamic>{};
      if (agentId != null) query['agent_id'] = agentId;
      final raw = await _get('/api/v1/queue/jobs', query: query);
      final jobsRaw = raw['jobs'] as List?;
      if (jobsRaw == null) return [];
      return jobsRaw
          .whereType<Map>()
          .map((j) => Map<String, dynamic>.from(j))
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
    final req = sdk.MemoryQueryRequest((b) => b
      ..query = query
      ..limitCommaOmitempty = limit
      ..categoryCommaOmitempty = category);

    final raw = await _post('/api/v1/memory/query', body: _toJson(req));
    final memoriesRaw = raw['memories'] as List?;
    if (memoriesRaw == null) return [];
    final results = <sdk.MemoryResult>[];
    for (final m in memoriesRaw) {
      if (m is Map) {
        final parsed =
            _fromJson<sdk.MemoryResult>(Map<String, dynamic>.from(m));
        if (parsed != null) results.add(parsed);
      }
    }
    return results;
  }

  Future<List<sdk.MemoryResult>> getRecentMemories({int limit = 10}) async {
    try {
      final raw =
          await _get('/api/v1/memory/recent', query: {'limit': limit});
      final memoriesRaw = raw['memories'] as List?;
      if (memoriesRaw == null) return [];
      final results = <sdk.MemoryResult>[];
      for (final m in memoriesRaw) {
        if (m is Map) {
          final parsed =
              _fromJson<sdk.MemoryResult>(Map<String, dynamic>.from(m));
          if (parsed != null) results.add(parsed);
        }
      }
      return results;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw `memories` array for callers that need to deserialize
  /// via a local model whose shape differs from [sdk.MemoryResult]
  /// (e.g. the local `MemoryResultModel` with its nested `memory` Map).
  Future<List<Map<String, dynamic>>> queryMemoryRaw({
    required String query,
    int limit = 10,
    String? category,
  }) async {
    final body = <String, dynamic>{
      'query': query,
      'limit': limit,
      if (category != null) 'category': category,
    };
    final raw = await _post('/api/v1/memory/query', body: body);
    final memoriesRaw = raw['memories'] as List? ?? [];
    return memoriesRaw
        .whereType<Map>()
        .map((m) => Map<String, dynamic>.from(m))
        .toList();
  }

  /// Returns the raw `memories` array for callers that need to deserialize
  /// via a local model whose shape differs from [sdk.MemoryResult].
  Future<List<Map<String, dynamic>>> getRecentMemoriesRaw(
      {int limit = 10}) async {
    final raw = await _get('/api/v1/memory/recent', query: {'limit': limit});
    final memoriesRaw = raw['memories'] as List? ?? [];
    return memoriesRaw
        .whereType<Map>()
        .map((m) => Map<String, dynamic>.from(m))
        .toList();
  }

  // ===== Skills =====

  Future<List<sdk.SkillInfo>> getSkills({String? category}) async {
    try {
      final query = <String, dynamic>{};
      if (category != null) query['category'] = category;
      final raw = await _get('/api/v1/skills', query: query);
      final skillsRaw = raw['skills'] as List?;
      if (skillsRaw == null) return [];
      final results = <sdk.SkillInfo>[];
      for (final s in skillsRaw) {
        if (s is Map) {
          final parsed =
              _fromJson<sdk.SkillInfo>(Map<String, dynamic>.from(s));
          if (parsed != null) results.add(parsed);
        }
      }
      return results;
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.SkillUIDescriptor?> getSkillUi(String slug) async {
    try {
      final raw = await _get('/api/v1/skills/$slug/ui');
      return _fromJson<sdk.SkillUIDescriptor>(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<sdk.ExecuteResult?> executeSkill({
    required String slug,
    required String prompt,
  }) async {
    final body = <String, dynamic>{'prompt': prompt};
    final raw = await _post('/api/v1/skills/$slug/execute', body: body);
    return _fromJson<sdk.ExecuteResult>(raw);
  }

  Future<sdk.ExecuteResult?> executeSkillWithParams({
    required String slug,
    required Map<String, dynamic> params,
  }) async {
    final raw = await _post('/api/v1/skills/$slug/execute', body: params);
    return _fromJson<sdk.ExecuteResult>(raw);
  }

  /// Returns the raw `skills` array for callers that need to deserialize
  /// via a local model whose shape differs from [sdk.SkillInfo]
  /// (e.g. the local `Skill` class with `tags` and `capabilities` arrays).
  Future<List<Map<String, dynamic>>> getSkillsRaw({String? category}) async {
    final query = <String, dynamic>{};
    if (category != null) query['category'] = category;
    final raw = await _get('/api/v1/skills', query: query);
    final skillsRaw = raw['skills'] as List? ?? [];
    return skillsRaw
        .whereType<Map>()
        .map((s) => Map<String, dynamic>.from(s))
        .toList();
  }

  /// Returns the raw `/api/v1/skills/{slug}/ui` JSON for callers that
  /// need to deserialize via a local model whose shape differs from
  /// [sdk.SkillUIDescriptor].
  Future<Map<String, dynamic>> getSkillUiRaw(String slug) async {
    return _get('/api/v1/skills/$slug/ui');
  }

  /// Returns the raw `/api/v1/skills/{slug}/execute` JSON for callers
  /// that need to deserialize via the local `SkillExecuteResult` model.
  Future<Map<String, dynamic>> executeSkillWithParamsRaw({
    required String slug,
    required Map<String, dynamic> params,
  }) async {
    return _post('/api/v1/skills/$slug/execute', body: params);
  }

  // ===== Search =====

  /// Returns the raw search-response JSON.  The SDK only models the
  /// individual [sdk.SearchResult] item, not the top-level response,
  /// so callers deserialize the `results` array themselves.
  Future<Map<String, dynamic>> search(
      {required String query, String? scope}) async {
    final req = sdk.SearchRequest((b) => b
      ..query = query
      ..scopeCommaOmitempty = scope);

    final raw = await _post('/api/v1/search', body: _toJson(req));
    return raw;
  }

  /// Convenience overload accepting the local [SearchScope] enum and
  /// returning a typed [SearchResults] for panels that prefer the local
  /// model.
  Future<SearchResults> searchWithScope({
    required String query,
    SearchScope scope = SearchScope.all,
  }) async {
    final scopeValue = scope == SearchScope.all ? null : scope.apiValue;
    final raw = await search(query: query, scope: scopeValue);
    return SearchResults.fromJson(raw);
  }

  /// Semantic search via `POST /api/v1/search/semantic`.
  ///
  /// The server may fall back to keyword mode and signals this via
  /// `SemanticSearchResults.mode`.  Check [SemanticSearchResults.err] for
  /// server-reported errors.
  Future<SemanticSearchResults> searchSemantic({
    required String query,
    SearchScope scope = SearchScope.all,
    int limit = 20,
  }) async {
    final res = await _post('/api/v1/search/semantic', body: {
      'query': query,
      'scope': scope == SearchScope.all ? '' : scope.apiValue,
      'limit': limit,
    });
    return SemanticSearchResults.fromJson(res);
  }

  // ===== Projects / Branches =====

  /// Returns the raw `projects` array.  Callers deserialize via `Project.fromJson`.
  Future<List<Map<String, dynamic>>> listProjects() async {
    try {
      final raw = await _get('/api/v1/projects');
      final projectsRaw = raw['projects'] as List? ?? [];
      return projectsRaw
          .whereType<Map>()
          .map((p) => Map<String, dynamic>.from(p))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw `branches` array.  Callers deserialize via `BranchInfo.fromJson`.
  Future<List<Map<String, dynamic>>> listBranches(String projectId) async {
    try {
      final raw = await _get('/api/v1/projects/$projectId/branches');
      final branchesRaw = raw['branches'] as List? ?? [];
      return branchesRaw
          .whereType<Map>()
          .map((b) => Map<String, dynamic>.from(b))
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

  /// Returns the raw `plans` array.  Callers deserialize via `Plan.fromJson`.
  Future<List<Map<String, dynamic>>> listPlans(
      {String? projectId, int limit = 50}) async {
    try {
      final query = <String, dynamic>{'limit': limit};
      if (projectId != null) query['project_id'] = projectId;
      final raw = await _get('/api/v1/plans', query: query);
      final plansRaw = raw['plans'] as List?;
      if (plansRaw == null) return [];
      return plansRaw
          .whereType<Map>()
          .map((p) => Map<String, dynamic>.from(p))
          .toList();
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw plan JSON.  Callers deserialize via `Plan.fromJson`.
  Future<Map<String, dynamic>> getPlan(String id) async {
    try {
      return await _get('/api/v1/plans/$id');
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> approvePlan(String id,
      {String? sessionID, String? by}) async {
    final req = sdk.ApprovePlanRequest((b) => b
      ..planId = id
      ..sessionId = sessionID ?? ''
      ..by = by ?? '');
    try {
      return await _post('/api/v1/plans/$id/approve', body: _toJson(req));
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> rejectPlan(String id,
      {String? sessionID, String? by, String? reason}) async {
    final req = sdk.RejectPlanRequest((b) => b
      ..planId = id
      ..sessionId = sessionID ?? ''
      ..by = by ?? ''
      ..reasonCommaOmitempty = reason);
    try {
      return await _post('/api/v1/plans/$id/reject', body: _toJson(req));
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> confirmPlan(String id,
      {String? sessionID, String? by}) async {
    final req = sdk.ConfirmPlanRequest((b) => b
      ..planId = id
      ..sessionId = sessionID ?? ''
      ..by = by ?? '');
    try {
      return await _post('/api/v1/plans/$id/confirm', body: _toJson(req));
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  Future<Map<String, dynamic>> revisePlan(String id,
      {String? sessionID, String? feedback}) async {
    final req = sdk.RevisePlanRequest((b) => b
      ..planId = id
      ..sessionId = sessionID ?? ''
      ..feedback = feedback ?? '');
    try {
      return await _post('/api/v1/plans/$id/revise', body: _toJson(req));
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

  Future<sdk.CommandHistory?> getTerminalHistory() async {
    final raw = await _get('/api/v1/terminal/history');
    // The endpoint returns a wrapped response; check for history key.
    final history = raw['history'] as Map<String, dynamic>? ?? raw;
    return _fromJson<sdk.CommandHistory>(history);
  }

  /// Returns the raw `/api/v1/terminal/history` JSON (with the
  /// `history` array) for panels that deserialize entries via the
  /// local `CommandEntry.fromJson`.  Use this when you need the
  /// array shape rather than the typed [sdk.CommandHistory] model.
  Future<Map<String, dynamic>> getTerminalHistoryRaw() async {
    return _get('/api/v1/terminal/history');
  }

  Future<sdk.ExecuteResult?> executeCommand(String command) async {
    final body = <String, dynamic>{'command': command};
    final raw = await _post('/api/v1/terminal/exec', body: body);
    return _fromJson<sdk.ExecuteResult>(raw);
  }

  /// Returns the raw `/api/v1/terminal/exec` JSON for panels that
  /// inspect fields (e.g. `success`) not modeled on [sdk.ExecuteResult].
  Future<Map<String, dynamic>> executeCommandRaw(String command) async {
    final body = <String, dynamic>{'command': command};
    return _post('/api/v1/terminal/exec', body: body);
  }

  Future<void> clearTerminalHistory() async {
    await _post('/api/v1/terminal/clear');
  }

  // ===== Calendar =====

  Future<sdk.CalendarEvent?> getCalendarToday() async {
    try {
      final raw = await _get('/api/v1/calendar/today');
      // `_get` returns Map<String, dynamic>. The calendar endpoint may
      // also return a JSON array, in which case the caller would need a
      // different code path; for now we treat the response as a single
      // event payload and let the SDK's fromJson return null if the
      // shape doesn't match.
      return _fromJson<sdk.CalendarEvent>(raw);
    } on DioException catch (e) {
      throw _handleError(e);
    }
  }

  /// Returns the raw `/api/v1/calendar/today` JSON (with the `events`
  /// array) for panels that deserialize entries via the local
  /// `CalendarEvent.fromJson`.  Use this when the panel needs the
  /// wrapped list shape rather than a single typed event.
  Future<Map<String, dynamic>> getCalendarTodayRaw() async {
    return _get('/api/v1/calendar/today');
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
