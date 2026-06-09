import 'package:freezed_annotation/freezed_annotation.dart';

part 'api_models.freezed.dart';
part 'api_models.g.dart';

// ===== Request/Response Models =====

/// Generic API response wrapper.
///
/// Not freezed because it is a generic class; freezed does not support
/// generating code for classes with unconstrained type parameters that are
/// not themselves serialised.
class ApiResponse<T> {
  final T? data;
  final String? error;
  final int statusCode;

  const ApiResponse({
    this.data,
    this.error,
    required this.statusCode,
  });

  bool get isSuccess => statusCode >= 200 && statusCode < 300;
  bool get isError => statusCode >= 400;
}

// ===== Chat Models =====

@freezed
class ChatMessage with _$ChatMessage {
  const ChatMessage._();

  const factory ChatMessage({
    required String id,
    required String role,
    required String content,
    required DateTime timestamp,
    @JsonKey(name: 'session_id') String? sessionId,
    @JsonKey(name: 'tool_calls') List<String>? toolCalls,
  }) = _ChatMessage;

  factory ChatMessage.fromJson(Map<String, dynamic> json) =>
      _$$ChatMessageImplFromJson(json);

  /// Parse from a backend session.Message (int64 ID, RFC3339 timestamp).
  factory ChatMessage.fromBackendMessage(Map<String, dynamic> json) {
    // Normalise the timestamp field so the generated fromJson can handle it.
    final ts = json['timestamp'];
    String isoTimestamp;
    if (ts is String) {
      isoTimestamp = ts;
    } else if (ts is num) {
      isoTimestamp = DateTime.fromMillisecondsSinceEpoch(ts.toInt())
          .toIso8601String();
    } else {
      isoTimestamp = DateTime.now().toIso8601String();
    }

    // Build a normalised map that matches the freezed JSON schema.
    return ChatMessage.fromJson({
      'id': _toStringId(json['id']),
      'role': json['role'] as String? ?? 'assistant',
      'content': json['content'] as String? ?? '',
      'timestamp': isoTimestamp,
      'session_id': json['session_id'] as String?,
      'tool_calls': (json['tool_calls'] as List?)?.cast<String>(),
    });
  }

  static String _toStringId(dynamic id) {
    if (id is String) return id;
    if (id is int) return id.toString();
    if (id is num) return id.toInt().toString();
    return id?.toString() ?? '';
  }
}

@freezed
class ChatRequest with _$ChatRequest {
  const ChatRequest._();

  const factory ChatRequest({
    required String message,
    @JsonKey(name: 'conversation_id') String? conversationId,
    @JsonKey(name: 'agent_id') String? agentId,
    List<ChatMessage>? history,
  }) = _ChatRequest;

  /// Custom toJson to omit null / history fields when not needed.
  factory ChatRequest.fromJson(Map<String, dynamic> json) =>
      _$$ChatRequestImplFromJson(json);

  @override
  Map<String, dynamic> toJson() => {
        'message': message,
        if (conversationId != null) 'conversation_id': conversationId,
        if (agentId != null) 'agent_id': agentId,
        if (history != null) 'history': history!.map((m) => m.toJson()).toList(),
      };
}

// ===== Session Models =====

@freezed
class Session with _$Session {
  const Session._();

  const factory Session({
    required String id,
    /// Backend field name is 'name'; stored as 'title' in the Dart model.
    @JsonKey(name: 'name') required String title,
    String? description,
    @JsonKey(name: 'conversation_id') String? conversationId,
    @JsonKey(name: 'created_at') required DateTime createdAt,
    @JsonKey(name: 'last_activity') DateTime? lastActivity,
    @JsonKey(name: 'attached_clients') List<String>? attachedClients,
  }) = _Session;

  factory Session.fromJson(Map<String, dynamic> json) =>
      _$$SessionImplFromJson(_normaliseSessionJson(json));

  /// Normalise backend JSON before freezed parsing.
  /// Prefer description as display title when name is generic ("default") or
  /// shorter.
  static Map<String, dynamic> _normaliseSessionJson(Map<String, dynamic> json) {
    final name =
        json['name'] as String? ?? json['title'] as String? ?? 'Untitled';
    final description = json['description'] as String?;
    final displayTitle =
        (description != null &&
                description.isNotEmpty &&
                (name == 'default' || name.length < description.length))
            ? description
            : name;
    return {...json, 'name': displayTitle};
  }
}

// ===== Task Models =====

@freezed
class Task with _$Task {
  const Task._();

  const factory Task({
    required String id,
    /// Backend field name is 'name'; stored as 'title' in the Dart model.
    @JsonKey(name: 'name') required String title,
    required String description,
    /// Backend field name is 'state'; stored as 'status' in the Dart model.
    @JsonKey(name: 'state') required String status,
    @JsonKey(name: 'agent_id') String? agentId,
    @JsonKey(name: 'session_id') String? sessionId,
    @JsonKey(name: 'created_at') required DateTime createdAt,
    @JsonKey(name: 'updated_at') DateTime? updatedAt,
    @JsonKey(name: 'completed_at') DateTime? completedAt,
    Map<String, dynamic>? metadata,
    @JsonKey(name: 'total_jobs') int? totalJobs,
    @JsonKey(name: 'completed_jobs') int? completedJobs,
    @JsonKey(name: 'failed_jobs') int? failedJobs,
    List<TaskStep>? steps,
  }) = _Task;

  factory Task.fromJson(Map<String, dynamic> json) =>
      _$$TaskImplFromJson(_normaliseTaskJson(json));

  /// Normalise backend JSON before freezed parsing.
  static Map<String, dynamic> _normaliseTaskJson(Map<String, dynamic> json) {
    return {
      ...json,
      'name': json['name'] as String? ?? json['title'] as String? ?? '',
      'state': json['state'] as String? ?? json['status'] as String? ?? 'pending',
      'agent_id': json['agent_id'] as String? ?? json['assigned_agent'] as String?,
    };
  }
}

@freezed
class TaskStep with _$TaskStep {
  const factory TaskStep({
    required String id,
    @JsonKey(name: 'task_id') required String taskId,
    required String description,
    required String status,
    String? output,
    @JsonKey(name: 'completed_at') DateTime? completedAt,
  }) = _TaskStep;

  factory TaskStep.fromJson(Map<String, dynamic> json) =>
      _$$TaskStepImplFromJson(json);
}

// ===== Agent Models =====

@freezed
class Agent with _$Agent {
  const factory Agent({
    required String id,
    required String name,
    required String description,
    required bool enabled,
    String? prompt,
    Map<String, dynamic>? frontmatter,
  }) = _Agent;

  factory Agent.fromJson(Map<String, dynamic> json) =>
      _$$AgentImplFromJson(json);
}

// ===== Queue/Job Models =====

@freezed
class Job with _$Job {
  const factory Job({
    required String id,
    required String type,
    /// Backend field name is 'state'; stored as 'status' in the Dart model.
    @JsonKey(name: 'state') required String status,
    @JsonKey(name: 'agent_id') String? agentId,
    Map<String, dynamic>? payload,
    @JsonKey(name: 'created_at') required DateTime createdAt,
    @JsonKey(name: 'completed_at') DateTime? completedAt,
    @JsonKey(name: 'retry_count') @Default(0) int retryCount,
    String? error,
  }) = _Job;

  factory Job.fromJson(Map<String, dynamic> json) =>
      _$$JobImplFromJson(_normaliseJobJson(json));

  /// Normalise backend JSON before freezed parsing.
  static Map<String, dynamic> _normaliseJobJson(Map<String, dynamic> json) {
    return {
      ...json,
      'state': json['state'] as String? ?? json['status'] as String? ?? 'pending',
    };
  }
}

// ===== Skill/Tool Models =====

@freezed
class Skill with _$Skill {
  const factory Skill({
    required String slug,
    required String name,
    required String description,
    @Default('') String category,
    @Default([]) List<String> capabilities,
    @Default([]) List<String> tags,
    @Default(true) bool enabled,
  }) = _Skill;

  factory Skill.fromJson(Map<String, dynamic> json) =>
      _$$SkillImplFromJson(json);
}

// ===== Metrics Models =====

@freezed
class MetricsSnapshot with _$MetricsSnapshot {
  const factory MetricsSnapshot({
    required DateTime timestamp,
    @JsonKey(name: 'active_agents') @Default(0) int activeAgents,
    @JsonKey(name: 'requests_per_sec') @Default(0.0) double requestsPerSec,
    @JsonKey(name: 'token_usage_rate') @Default(0.0) double tokenUsageRate,
    @JsonKey(name: 'queue_depth') @Default(0) int queueDepth,
    @JsonKey(name: 'total_sessions') @Default(0) int totalSessions,
    @JsonKey(name: 'total_jobs') @Default(0) int totalJobs,
    @JsonKey(name: 'running_jobs') @Default(0) int runningJobs,
    @JsonKey(name: 'pending_jobs') @Default(0) int pendingJobs,
    @Default('') String version,
    Map<String, dynamic>? metadata,
  }) = _MetricsSnapshot;

  factory MetricsSnapshot.fromJson(Map<String, dynamic> json) =>
      _$$MetricsSnapshotImplFromJson(_normaliseMetricsJson(json));

  /// Normalise backend JSON before freezed parsing.
  static Map<String, dynamic> _normaliseMetricsJson(Map<String, dynamic> json) {
    if (json['timestamp'] == null) {
      return {...json, 'timestamp': DateTime.now().toIso8601String()};
    }
    return json;
  }
}

// ===== Plan Models =====

@freezed
class Plan with _$Plan {
  const factory Plan({
    required String id,
    required String title,
    @Default('') String description,
    @JsonKey(name: 'file_path') @Default('') String filePath,
    @JsonKey(name: 'project_id') String? projectID,
    required String state,
    @JsonKey(name: 'created_at') required DateTime createdAt,
    @JsonKey(name: 'updated_at') required DateTime updatedAt,
    @JsonKey(name: 'approved_at') DateTime? approvedAt,
    @JsonKey(name: 'confirmed_at') DateTime? confirmedAt,
    @JsonKey(name: 'approved_by') String? approvedBy,
    @JsonKey(name: 'confirmed_by') String? confirmedBy,
    @JsonKey(name: 'task_id') String? taskID,
    @JsonKey(name: 'source_session') String? sourceSession,
    @JsonKey(name: 'revision_count') @Default(0) int revisionCount,
    @Default([]) List<PlanPhase> phases,
  }) = _Plan;

  factory Plan.fromJson(Map<String, dynamic> json) =>
      _$$PlanImplFromJson(json);
}

@freezed
class PlanPhase with _$PlanPhase {
  const factory PlanPhase({
    required String id,
    @JsonKey(name: 'plan_id') required String planID,
    required String name,
    required int sequence,
    @JsonKey(name: 'total_steps') @Default(0) int totalSteps,
    @JsonKey(name: 'completed_steps') @Default(0) int completedSteps,
    @JsonKey(name: 'failed_steps') @Default(0) int failedSteps,
    required String state,
  }) = _PlanPhase;

  factory PlanPhase.fromJson(Map<String, dynamic> json) =>
      _$$PlanPhaseImplFromJson(json);
}

// ===== Search Models =====

enum SearchScope { all, sessions, tasks, memories, plans }

enum SearchResultType { session, task, memory, plan }

@freezed
class SearchResults with _$SearchResults {
  const factory SearchResults({
    @Default([]) List<SearchResultItem> results,
  }) = _SearchResults;

  factory SearchResults.fromJson(Map<String, dynamic> json) =>
      _$$SearchResultsImplFromJson(json);
}

@freezed
class SearchResultItem with _$SearchResultItem {
  const factory SearchResultItem({
    required SearchResultType type,
    required String id,
    required String title,
    @Default('') String snippet,
  }) = _SearchResultItem;

  factory SearchResultItem.fromJson(Map<String, dynamic> json) =>
      _$$SearchResultItemImplFromJson(json);
}

// ===== Branch Models =====

@freezed
class BranchInfo with _$BranchInfo {
  const factory BranchInfo({
    required String name,
    @JsonKey(name: 'is_current') @Default(false) bool isCurrent,
    @JsonKey(name: 'is_head') @Default(false) bool isHead,
  }) = _BranchInfo;

  factory BranchInfo.fromJson(Map<String, dynamic> json) =>
      _$$BranchInfoImplFromJson(json);
}

// ===== Skill UI Descriptor Models =====

@freezed
class SkillFormField with _$SkillFormField {
  const factory SkillFormField({
    required String name,
    required String label,
    @Default('text') String type,
    @Default(false) bool required,
    @JsonKey(name: 'default_value') String? defaultValue,
    @Default([]) List<String> options,
  }) = _SkillFormField;

  factory SkillFormField.fromJson(Map<String, dynamic> json) =>
      _$$SkillFormFieldImplFromJson(json);
}

@freezed
class SkillUiDescriptor with _$SkillUiDescriptor {
  const factory SkillUiDescriptor({
    @JsonKey(name: 'ui_type') @Default('form') String uiType,
    @JsonKey(name: 'form_fields') @Default([])
    List<SkillFormField> formFields,
    List<String>? actions,
  }) = _SkillUiDescriptor;

  factory SkillUiDescriptor.fromJson(Map<String, dynamic> json) =>
      _$$SkillUiDescriptorImplFromJson(json);
}

// ===== Skill Execution Models =====

@freezed
class SkillExecuteResult with _$SkillExecuteResult {
  const factory SkillExecuteResult({
    required String output,
    required bool success,
    String? error,
  }) = _SkillExecuteResult;

  factory SkillExecuteResult.fromJson(Map<String, dynamic> json) =>
      _$$SkillExecuteResultImplFromJson(json);
}
