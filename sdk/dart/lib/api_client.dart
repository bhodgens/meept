//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ApiClient {
  ApiClient({this.basePath = 'http://localhost:8081', this.authentication,});

  final String basePath;
  final Authentication? authentication;

  var _client = Client();
  final _defaultHeaderMap = <String, String>{};

  /// Returns the current HTTP [Client] instance to use in this class.
  ///
  /// The return value is guaranteed to never be null.
  Client get client => _client;

  /// Requests to use a new HTTP [Client] in this class.
  set client(Client newClient) {
    _client = newClient;
  }

  Map<String, String> get defaultHeaderMap => _defaultHeaderMap;

  void addDefaultHeader(String key, String value) {
     _defaultHeaderMap[key] = value;
  }

  // We don't use a Map<String, String> for queryParams.
  // If collectionFormat is 'multi', a key might appear multiple times.
  Future<Response> invokeAPI(
    String path,
    String method,
    List<QueryParam> queryParams,
    Object? body,
    Map<String, String> headerParams,
    Map<String, String> formParams,
    String? contentType, {
    Future<void>? abortTrigger,
  }) async {
    await authentication?.applyToParams(queryParams, headerParams);

    headerParams.addAll(_defaultHeaderMap);
    if (contentType != null) {
      headerParams['Content-Type'] = contentType;
    }

    final urlEncodedQueryParams = queryParams.map((param) => '$param');
    final queryString = urlEncodedQueryParams.isNotEmpty ? '?${urlEncodedQueryParams.join('&')}' : '';
    final uri = Uri.parse('$basePath$path$queryString');

    try {
      // Special case for uploading a single file which isn't a 'multipart/form-data'.
      if (
        body is MultipartFile && (contentType == null ||
        !contentType.toLowerCase().startsWith('multipart/form-data'))
      ) {
        final request = AbortableStreamedRequest(method, uri, abortTrigger: abortTrigger);
        request.headers.addAll(headerParams);
        request.contentLength = body.length;
        body.finalize().listen(
          request.sink.add,
          onDone: request.sink.close,
          // ignore: avoid_types_on_closure_parameters
          onError: (Object error, StackTrace trace) => request.sink.close(),
          cancelOnError: true,
        );
        final response = await _client.send(request);
        return Response.fromStream(response);
      }

      if (body is MultipartRequest) {
        final request = AbortableMultipartRequest(method, uri, abortTrigger: abortTrigger);
        request.fields.addAll(body.fields);
        request.files.addAll(body.files);
        request.headers.addAll(body.headers);
        request.headers.addAll(headerParams);
        final response = await _client.send(request);
        return Response.fromStream(response);
      }

      final msgBody = contentType == 'application/x-www-form-urlencoded'
        ? formParams
        : await serializeAsync(body);
      final nullableHeaderParams = headerParams.isEmpty ? null : headerParams;

      final request = AbortableRequest(method, uri, abortTrigger: abortTrigger);
      if (nullableHeaderParams != null) {
        request.headers.addAll(nullableHeaderParams);
      }
      if (msgBody is String && msgBody.isNotEmpty) {
        request.body = msgBody;
      } else if (msgBody is List<int> && msgBody.isNotEmpty) {
        request.bodyBytes = msgBody;
      } else if (msgBody is Map<String, String>) {
        request.bodyFields = msgBody;
      }
      final response = await _client.send(request);
      return Response.fromStream(response);
    } on SocketException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'Socket operation failed: $method $path',
        error,
        trace,
      );
    } on TlsException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'TLS/SSL communication failed: $method $path',
        error,
        trace,
      );
    } on IOException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'I/O operation failed: $method $path',
        error,
        trace,
      );
    } on ClientException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'HTTP connection failed: $method $path',
        error,
        trace,
      );
    } on Exception catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'Exception occurred: $method $path',
        error,
        trace,
      );
    }
  }

  Future<dynamic> deserializeAsync(String value, String targetType, {bool growable = false,}) async =>
    // ignore: deprecated_member_use_from_same_package
    deserialize(value, targetType, growable: growable);

  @Deprecated('Scheduled for removal in OpenAPI Generator 6.x. Use deserializeAsync() instead.')
  dynamic deserialize(String value, String targetType, {bool growable = false,}) {
    // Remove all spaces. Necessary for regular expressions as well.
    targetType = targetType.replaceAll(' ', ''); // ignore: parameter_assignments

    // If the expected target type is String, nothing to do...
    return targetType == 'String'
      ? value
      : fromJson(json.decode(value), targetType, growable: growable);
  }

  // ignore: deprecated_member_use_from_same_package
  Future<String> serializeAsync(Object? value) async => serialize(value);

  @Deprecated('Scheduled for removal in OpenAPI Generator 6.x. Use serializeAsync() instead.')
  String serialize(Object? value) => value == null ? '' : json.encode(value);

  /// Returns a native instance of an OpenAPI class matching the [specified type][targetType].
  static dynamic fromJson(dynamic value, String targetType, {bool growable = false,}) {
    try {
      switch (targetType) {
        case 'String':
          return value is String ? value : value.toString();
        case 'int':
          return value is int ? value : int.parse('$value');
        case 'double':
          return value is double ? value : double.parse('$value');
        case 'bool':
          if (value is bool) {
            return value;
          }
          final valueString = '$value'.toLowerCase();
          return valueString == 'true' || valueString == '1';
        case 'DateTime':
          return value is DateTime ? value : DateTime.tryParse(value);
        case 'AddJobRequest':
          return AddJobRequest.fromJson(value);
        case 'AddJobResponse':
          return AddJobResponse.fromJson(value);
        case 'AddWorkerRequest':
          return AddWorkerRequest.fromJson(value);
        case 'AgentJobConfig':
          return AgentJobConfig.fromJson(value);
        case 'AgentProgressEvent':
          return AgentProgressEvent.fromJson(value);
        case 'ApplyImprovementRequest':
          return ApplyImprovementRequest.fromJson(value);
        case 'ApprovePlanRequest':
          return ApprovePlanRequest.fromJson(value);
        case 'AttachSessionRequest':
          return AttachSessionRequest.fromJson(value);
        case 'AttendeeInfo':
          return AttendeeInfo.fromJson(value);
        case 'AuditEntry':
          return AuditEntry.fromJson(value);
        case 'AuditRequest':
          return AuditRequest.fromJson(value);
        case 'BranchSessionRequest':
          return BranchSessionRequest.fromJson(value);
        case 'BusService':
          return BusService.fromJson(value);
        case 'BusStatsResponse':
          return BusStatsResponse.fromJson(value);
        case 'CacheInspectResult':
          return CacheInspectResult.fromJson(value);
        case 'CacheService':
          return CacheService.fromJson(value);
        case 'CacheStatsResponse':
          return CacheStatsResponse.fromJson(value);
        case 'CalendarEvent':
          return CalendarEvent.fromJson(value);
        case 'CalendarService':
          return CalendarService.fromJson(value);
        case 'CancelRequest':
          return CancelRequest.fromJson(value);
        case 'CancelTaskRequest':
          return CancelTaskRequest.fromJson(value);
        case 'ChatRequest':
          return ChatRequest.fromJson(value);
        case 'ChatResponse':
          return ChatResponse.fromJson(value);
        case 'ChatService':
          return ChatService.fromJson(value);
        case 'CheckRequest':
          return CheckRequest.fromJson(value);
        case 'CheckResponse':
          return CheckResponse.fromJson(value);
        case 'ClaimRequest':
          return ClaimRequest.fromJson(value);
        case 'ClearCacheRequest':
          return ClearCacheRequest.fromJson(value);
        case 'CommandHistory':
          return CommandHistory.fromJson(value);
        case 'CompactSessionRequest':
          return CompactSessionRequest.fromJson(value);
        case 'CompleteRequest':
          return CompleteRequest.fromJson(value);
        case 'Config':
          return Config.fromJson(value);
        case 'ConfirmPlanRequest':
          return ConfirmPlanRequest.fromJson(value);
        case 'CreateEventRequest':
          return CreateEventRequest.fromJson(value);
        case 'CreatePipelineRequest':
          return CreatePipelineRequest.fromJson(value);
        case 'CreatePlanRequest':
          return CreatePlanRequest.fromJson(value);
        case 'CreateSessionRequest':
          return CreateSessionRequest.fromJson(value);
        case 'CreateTaskRequest':
          return CreateTaskRequest.fromJson(value);
        case 'DaemonService':
          return DaemonService.fromJson(value);
        case 'DaemonStatus':
          return DaemonStatus.fromJson(value);
        case 'DeletePipelineRequest':
          return DeletePipelineRequest.fromJson(value);
        case 'DeleteSessionRequest':
          return DeleteSessionRequest.fromJson(value);
        case 'DeleteTaskRequest':
          return DeleteTaskRequest.fromJson(value);
        case 'DetachSessionRequest':
          return DetachSessionRequest.fromJson(value);
        case 'EnableJobRequest':
          return EnableJobRequest.fromJson(value);
        case 'EnqueueRequest':
          return EnqueueRequest.fromJson(value);
        case 'ExecuteRequest':
          return ExecuteRequest.fromJson(value);
        case 'ExecuteResult':
          return ExecuteResult.fromJson(value);
        case 'FailRequest':
          return FailRequest.fromJson(value);
        case 'FollowUpRequest':
          return FollowUpRequest.fromJson(value);
        case 'ForkSessionRequest':
          return ForkSessionRequest.fromJson(value);
        case 'GenerateImprovementRequest':
          return GenerateImprovementRequest.fromJson(value);
        case 'GetMessagesRequest':
          return GetMessagesRequest.fromJson(value);
        case 'GetRequest':
          return GetRequest.fromJson(value);
        case 'GetSessionRequest':
          return GetSessionRequest.fromJson(value);
        case 'GetTaskRequest':
          return GetTaskRequest.fromJson(value);
        case 'GetTaskStepsRequest':
          return GetTaskStepsRequest.fromJson(value);
        case 'GetTreeRequest':
          return GetTreeRequest.fromJson(value);
        case 'InvalidateRequest':
          return InvalidateRequest.fromJson(value);
        case 'ListBranchesRequest':
          return ListBranchesRequest.fromJson(value);
        case 'ListEventsRequest':
          return ListEventsRequest.fromJson(value);
        case 'ListEventsResponse':
          return ListEventsResponse.fromJson(value);
        case 'ListJobsResponse':
          return ListJobsResponse.fromJson(value);
        case 'ListOptions':
          return ListOptions.fromJson(value);
        case 'ListRequest':
          return ListRequest.fromJson(value);
        case 'ListSessionsRequest':
          return ListSessionsRequest.fromJson(value);
        case 'MemoryQueryRequest':
          return MemoryQueryRequest.fromJson(value);
        case 'MemoryResult':
          return MemoryResult.fromJson(value);
        case 'MemoryService':
          return MemoryService.fromJson(value);
        case 'ModelInfo':
          return ModelInfo.fromJson(value);
        case 'ModelService':
          return ModelService.fromJson(value);
        case 'PaginatedResponse':
          return PaginatedResponse.fromJson(value);
        case 'PauseJobRequest':
          return PauseJobRequest.fromJson(value);
        case 'Pipeline':
          return Pipeline.fromJson(value);
        case 'PipelineInfo':
          return PipelineInfo.fromJson(value);
        case 'PipelineListRequest':
          return PipelineListRequest.fromJson(value);
        case 'PipelineService':
          return PipelineService.fromJson(value);
        case 'PipelineStatusResponse':
          return PipelineStatusResponse.fromJson(value);
        case 'PipelineStep':
          return PipelineStep.fromJson(value);
        case 'PipelineStepStatus':
          return PipelineStepStatus.fromJson(value);
        case 'PlanService':
          return PlanService.fromJson(value);
        case 'ProjectService':
          return ProjectService.fromJson(value);
        case 'ProviderInfo':
          return ProviderInfo.fromJson(value);
        case 'PublishRequest':
          return PublishRequest.fromJson(value);
        case 'QueueService':
          return QueueService.fromJson(value);
        case 'QueueStatusRequest':
          return QueueStatusRequest.fromJson(value);
        case 'QueueStatusResponse':
          return QueueStatusResponse.fromJson(value);
        case 'RegisterProjectRequest':
          return RegisterProjectRequest.fromJson(value);
        case 'RejectImprovementRequest':
          return RejectImprovementRequest.fromJson(value);
        case 'RejectPlanRequest':
          return RejectPlanRequest.fromJson(value);
        case 'RemoveJobRequest':
          return RemoveJobRequest.fromJson(value);
        case 'RemoveWorkerRequest':
          return RemoveWorkerRequest.fromJson(value);
        case 'ResumeJobRequest':
          return ResumeJobRequest.fromJson(value);
        case 'ResumeSessionRequest':
          return ResumeSessionRequest.fromJson(value);
        case 'RetryRequest':
          return RetryRequest.fromJson(value);
        case 'RevisePlanRequest':
          return RevisePlanRequest.fromJson(value);
        case 'RuntimeService':
          return RuntimeService.fromJson(value);
        case 'RuntimeStatusResponse':
          return RuntimeStatusResponse.fromJson(value);
        case 'ScaleWorkersRequest':
          return ScaleWorkersRequest.fromJson(value);
        case 'SchedulerService':
          return SchedulerService.fromJson(value);
        case 'SearchRequest':
          return SearchRequest.fromJson(value);
        case 'SearchResult':
          return SearchResult.fromJson(value);
        case 'SearchService':
          return SearchService.fromJson(value);
        case 'SecurityService':
          return SecurityService.fromJson(value);
        case 'SelfImproveService':
          return SelfImproveService.fromJson(value);
        case 'ServiceError':
          return ServiceError.fromJson(value);
        case 'ServiceRegistry':
          return ServiceRegistry.fromJson(value);
        case 'SessionService':
          return SessionService.fromJson(value);
        case 'SetProjectRequest':
          return SetProjectRequest.fromJson(value);
        case 'ShardDetail':
          return ShardDetail.fromJson(value);
        case 'ShellJobConfig':
          return ShellJobConfig.fromJson(value);
        case 'SkillInfo':
          return SkillInfo.fromJson(value);
        case 'SkillUIDescriptor':
          return SkillUIDescriptor.fromJson(value);
        case 'SkillsGetRequest':
          return SkillsGetRequest.fromJson(value);
        case 'SkillsListRequest':
          return SkillsListRequest.fromJson(value);
        case 'SkillsService':
          return SkillsService.fromJson(value);
        case 'StatusRequest':
          return StatusRequest.fromJson(value);
        case 'StatusResponse':
          return StatusResponse.fromJson(value);
        case 'SteerRequest':
          return SteerRequest.fromJson(value);
        case 'TaskListRequest':
          return TaskListRequest.fromJson(value);
        case 'TaskService':
          return TaskService.fromJson(value);
        case 'TemplateInfo':
          return TemplateInfo.fromJson(value);
        case 'TemplatesClearRequest':
          return TemplatesClearRequest.fromJson(value);
        case 'TemplatesClearResult':
          return TemplatesClearResult.fromJson(value);
        case 'TemplatesGetRequest':
          return TemplatesGetRequest.fromJson(value);
        case 'TemplatesInvokeRequest':
          return TemplatesInvokeRequest.fromJson(value);
        case 'TemplatesInvokeResult':
          return TemplatesInvokeResult.fromJson(value);
        case 'TemplatesListRequest':
          return TemplatesListRequest.fromJson(value);
        case 'TemplatesService':
          return TemplatesService.fromJson(value);
        case 'TerminalService':
          return TerminalService.fromJson(value);
        case 'TerminalSession':
          return TerminalSession.fromJson(value);
        case 'TriggerRequest':
          return TriggerRequest.fromJson(value);
        case 'UIActionDef':
          return UIActionDef.fromJson(value);
        case 'UIFieldDef':
          return UIFieldDef.fromJson(value);
        case 'UpdateEventRequest':
          return UpdateEventRequest.fromJson(value);
        case 'UpdateStatusRequest':
          return UpdateStatusRequest.fromJson(value);
        case 'UpdateTaskRequest':
          return UpdateTaskRequest.fromJson(value);
        case 'ValidateImprovementRequest':
          return ValidateImprovementRequest.fromJson(value);
        case 'VectorSearchRequest':
          return VectorSearchRequest.fromJson(value);
        case 'VectorSearchResult':
          return VectorSearchResult.fromJson(value);
        case 'VectorStats':
          return VectorStats.fromJson(value);
        case 'VectorStoreRequest':
          return VectorStoreRequest.fromJson(value);
        case 'WSSubscribeMessage':
          return WSSubscribeMessage.fromJson(value);
        case 'WSUnsubscribeMessage':
          return WSUnsubscribeMessage.fromJson(value);
        case 'WorkerService':
          return WorkerService.fromJson(value);
        case 'WorkerStatsResponse':
          return WorkerStatsResponse.fromJson(value);
        default:
          dynamic match;
          if (value is List && (match = _regList.firstMatch(targetType)?.group(1)) != null) {
            return value
              .map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,))
              .toList(growable: growable);
          }
          if (value is Set && (match = _regSet.firstMatch(targetType)?.group(1)) != null) {
            return value
              .map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,))
              .toSet();
          }
          if (value is Map && (match = _regMap.firstMatch(targetType)?.group(1)) != null) {
            return Map<String, dynamic>.fromIterables(
              value.keys.cast<String>(),
              value.values.map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,)),
            );
          }
      }
    } on Exception catch (error, trace) {
      throw ApiException.withInner(HttpStatus.internalServerError, 'Exception during deserialization.', error, trace,);
    }
    throw ApiException(HttpStatus.internalServerError, 'Could not find a suitable class for deserialization',);
  }
}

/// Primarily intended for use in an isolate.
class DeserializationMessage {
  const DeserializationMessage({
    required this.json,
    required this.targetType,
    this.growable = false,
  });

  /// The JSON value to deserialize.
  final String json;

  /// Target type to deserialize to.
  final String targetType;

  /// Whether to make deserialized lists or maps growable.
  final bool growable;
}

/// Primarily intended for use in an isolate.
Future<dynamic> decodeAsync(DeserializationMessage message) async {
  // Remove all spaces. Necessary for regular expressions as well.
  final targetType = message.targetType.replaceAll(' ', '');

  // If the expected target type is String, nothing to do...
  return targetType == 'String'
    ? message.json
    : json.decode(message.json);
}

/// Primarily intended for use in an isolate.
Future<dynamic> deserializeAsync(DeserializationMessage message) async {
  // Remove all spaces. Necessary for regular expressions as well.
  final targetType = message.targetType.replaceAll(' ', '');

  // If the expected target type is String, nothing to do...
  return targetType == 'String'
    ? message.json
    : ApiClient.fromJson(
        json.decode(message.json),
        targetType,
        growable: message.growable,
      );
}

/// Primarily intended for use in an isolate.
Future<String> serializeAsync(Object? value) async => value == null ? '' : json.encode(value);
