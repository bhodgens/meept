import 'package:freezed_annotation/freezed_annotation.dart';

part 'api_models.freezed.dart';
part 'api_models.g.dart';

// ===== UI-Only Panel Models =====

/// Simple file entry for the files panel.
class FileEntry {
  final String path;
  FileEntry({required this.path});
}

/// Memory search result model matching backend MemoryResult structure.
class MemoryResultModel {
  final String id;
  final String content;
  final String type;
  final String category;
  final double relevanceScore;
  final String source;
  final DateTime createdAt;
  final String? sessionId;
  final String? taskId;

  MemoryResultModel({
    required this.id,
    required this.content,
    required this.type,
    required this.category,
    required this.relevanceScore,
    required this.source,
    required this.createdAt,
    this.sessionId,
    this.taskId,
  });

  factory MemoryResultModel.fromJson(Map<String, dynamic> json) {
    final memory = json['memory'] as Map<String, dynamic>? ?? {};
    return MemoryResultModel(
      id: memory['id'] as String? ?? '',
      content: memory['content'] as String? ?? '',
      type: memory['type'] as String? ?? '',
      category: memory['category'] as String? ?? '',
      relevanceScore: (json['relevance_score'] as num?)?.toDouble() ?? 0.0,
      source: json['source'] as String? ?? '',
      createdAt: memory['created_at'] != null
          ? DateTime.parse(memory['created_at'] as String)
          : DateTime.now(),
      sessionId: memory['session_id'] as String?,
      taskId: memory['task_id'] as String?,
    );
  }
}

/// Terminal command history entry.
class CommandEntry {
  final String id;
  final String command;
  final String output;
  final String stderr;
  final int exitCode;
  final DateTime timestamp;
  final String workingDir;
  final bool success;

  CommandEntry({
    required this.id,
    required this.command,
    required this.output,
    required this.stderr,
    required this.exitCode,
    required this.timestamp,
    required this.workingDir,
    required this.success,
  });

  factory CommandEntry.fromJson(Map<String, dynamic> json) {
    return CommandEntry(
      id: json['id'] as String? ?? '',
      command: json['command'] as String? ?? '',
      output: json['output'] as String? ?? '',
      stderr: json['stderr'] as String? ?? '',
      exitCode: json['exit_code'] as int? ?? 0,
      timestamp: DateTime.parse(json['timestamp'] as String? ?? DateTime.now().toIso8601String()),
      workingDir: json['working_dir'] as String? ?? '',
      success: json['success'] as bool? ?? true,
    );
  }
}

/// Calendar event model (Google Calendar format).
class CalendarEvent {
  final String id;
  final String summary;
  final String? description;
  final String? location;
  final DateTime start;
  final DateTime end;

  CalendarEvent({
    required this.id,
    required this.summary,
    this.description,
    this.location,
    required this.start,
    required this.end,
  });

  factory CalendarEvent.fromJson(Map<String, dynamic> json) {
    final startVal = json['start'] ?? {};
    final endVal = json['end'] ?? {};
    return CalendarEvent(
      id: json['id'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      description: json['description'] as String?,
      location: json['location'] as String?,
      start: DateTime.tryParse((startVal['dateTime'] as String?) ?? (startVal['date'] as String?) ?? '') ?? DateTime.now(),
      end: DateTime.tryParse((endVal['dateTime'] as String?) ?? (endVal['date'] as String?) ?? '') ?? DateTime.now(),
    );
  }
}

// ===== Agent Progress Model =====

/// Represents a real-time agent progress event from the WebSocket stream.
///
/// Populated from the `{type: "agent_progress", data: {...}}` messages
/// sent by the backend's progress synthesizer.
class AgentProgress {
  final String agentId;
  final String message;
  final int tier;         // VerbosityLevel: 0=Quiet, 1=Normal, 2=Verbose
  final String? sourceEvent;
  final DateTime timestamp;

  AgentProgress({
    required this.agentId,
    required this.message,
    required this.tier,
    this.sourceEvent,
    required this.timestamp,
  });

  factory AgentProgress.fromJson(Map<String, dynamic> json) {
    // The server sends a flat {type, session_id, agent_id, message, tier, ...}
    // message directly (not wrapped in a "data" envelope).
    final data = json['data'] as Map<String, dynamic>?;

    // Coerce tier defensively: some backends send it as a string ("1") which
    // would throw on `as num`. Fall back to 1 (Normal) on parse failure.
    int coerceTier(dynamic v) {
      if (v is num) return v.toInt();
      if (v is String) return int.tryParse(v) ?? 1;
      return 1;
    }

    return AgentProgress(
      agentId: (data?['agent_id'] ?? json['agent_id'] ?? '') as String,
      message: (data?['message'] ?? json['message'] ?? '') as String,
      tier: coerceTier(data?['tier'] ?? json['tier'] ?? 1),
      sourceEvent: (data?['source_event'] ?? json['source_event']) as String?,
      timestamp: DateTime.tryParse(
              data?['timestamp'] as String? ??
                  json['timestamp'] as String? ??
                  DateTime.now().toIso8601String()) ??
          DateTime.now(),
    );
  }
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
      'tool_calls': (json['tool_calls'] as List?)?.map((e) => e.toString()).toList(),
    });
  }

  static String _toStringId(dynamic id) {
    if (id is String) return id;
    if (id is int) return id.toString();
    if (id is num) return id.toInt().toString();
    return id?.toString() ?? '';
  }
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
    @JsonKey(name: 'state') required String status,
    @JsonKey(name: 'result') String? output,
    @JsonKey(name: 'created_at') DateTime? createdAt,
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
    @Default('') String description,
    @Default(true) bool enabled,
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

extension SearchScopeX on SearchScope {
  String get displayName => switch (this) {
        SearchScope.all => 'all',
        SearchScope.sessions => 'sessions',
        SearchScope.tasks => 'tasks',
        SearchScope.memories => 'memories',
        SearchScope.plans => 'plans',
      };

  /// API parameter value for this scope.  Note: this must NOT be named
  /// `name` because Dart 2.15+ enums have a built-in `Enum.name` getter
  /// that shadows any extension getter with the same name.
  String get apiValue => switch (this) {
        SearchScope.all => '',
        SearchScope.sessions => 'sessions',
        SearchScope.tasks => 'tasks',
        SearchScope.memories => 'memories',
        SearchScope.plans => 'plans',
      };
}

enum SearchResultType { session, task, memory, plan }

extension SearchResultTypeX on SearchResultType {
  String get displayName => switch (this) {
        SearchResultType.session => 'sessions',
        SearchResultType.task => 'tasks',
        SearchResultType.memory => 'memories',
        SearchResultType.plan => 'plans',
      };
}

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

// ===== Project Models =====

/// Project model matching the daemon's `GET /api/v1/projects` response.
///
/// Mirrors `internal/project/types.go:Project`. Hand-rolled (not freezed)
/// to avoid forcing a build_runner run for a single panel fix, matching the
/// pattern used by [MemoryResultModel] above.
class Project {
  final String id;
  final String name;
  final String mode; // "git" or "local"
  final String branch;
  final String localPath;
  final String status; // "active", "archived", "error"

  const Project({
    required this.id,
    required this.name,
    this.mode = 'git',
    this.branch = '',
    this.localPath = '',
    this.status = 'active',
  });

  factory Project.fromJson(Map<String, dynamic> json) {
    return Project(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? json['id'] as String? ?? '',
      mode: json['mode'] as String? ?? 'git',
      branch: json['branch'] as String? ?? '',
      localPath: json['local_path'] as String? ?? '',
      status: json['status'] as String? ?? 'active',
    );
  }
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
