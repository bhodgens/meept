import 'package:equatable/equatable.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

part 'api_models.freezed.dart';
part 'api_models.g.dart';

// ===== Request/Response Models =====

/// Generic API response wrapper
class ApiResponse<T> extends Equatable {
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

  @override
  List<Object?> get props => [data, error, statusCode];
}

// ===== Chat Models =====

@freezed
class ChatMessage with _$ChatMessage {
  const factory ChatMessage({
    required String id,
    @Default('user') String role,
    @Default('') String content,
    required DateTime timestamp,
    String? sessionId,
    List<String>? toolCalls,
  }) = _ChatMessage;

  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
        id: _toStringId(json['id']),
        role: json['role'] as String? ?? 'user',
        content: json['content'] as String? ?? '',
        timestamp: json['timestamp'] is String
            ? DateTime.parse(json['timestamp'] as String)
            : DateTime.fromMillisecondsSinceEpoch(
                (json['timestamp'] as num?)?.toInt() ?? 0),
        sessionId: json['session_id'] as String?,
        toolCalls: (json['tool_calls'] as List?)?.cast<String>(),
      );

  /// Parse from a backend session.Message (int64 ID, RFC3339 timestamp).
  factory ChatMessage.fromBackendMessage(Map<String, dynamic> json) {
    final ts = json['timestamp'];
    return ChatMessage(
      id: json['id'].toString(),
      role: json['role'] as String? ?? 'assistant',
      content: json['content'] as String? ?? '',
      timestamp: ts is String
          ? DateTime.parse(ts)
          : DateTime.fromMillisecondsSinceEpoch(
              (ts as num?)?.toInt() ?? DateTime.now().millisecondsSinceEpoch),
      sessionId: json['session_id'] as String?,
    );
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
  const factory ChatRequest({
    required String message,
    String? conversationId,
    String? agentId,
    List<ChatMessage>? history,
  }) = _ChatRequest;
}

// ===== Session Models =====

@freezed
class Session with _$Session {
  const factory Session({
    required String id,
    required String title,
    String? description,
    String? conversationId,
    required DateTime createdAt,
    DateTime? lastActivity,
    List<String>? attachedClients,
  }) = _Session;

  factory Session.fromJson(Map<String, dynamic> json) {
    final name = json['name'] as String? ?? json['title'] as String? ?? 'Untitled';
    final description = json['description'] as String?;
    final displayTitle = (description != null &&
            description.isNotEmpty &&
            (name == 'default' || name.length < description.length))
        ? description
        : name;
    return Session(
      id: json['id'] as String,
      title: displayTitle,
      description: description,
      conversationId: json['conversation_id'] as String?,
      createdAt: DateTime.parse(json['created_at'] as String),
      lastActivity: json['last_activity'] != null
          ? DateTime.parse(json['last_activity'] as String)
          : null,
      attachedClients: (json['attached_clients'] as List?)?.cast<String>(),
    );
  }
}

// ===== Task Models =====

@freezed
class Task with _$Task {
  const factory Task({
    required String id,
    @Default('') String title,
    @Default('') String description,
    @Default('pending') String status,
    String? agentId,
    String? sessionId,
    required DateTime createdAt,
    DateTime? updatedAt,
    DateTime? completedAt,
    Map<String, dynamic>? metadata,
    int? totalJobs,
    int? completedJobs,
    int? failedJobs,
    List<TaskStep>? steps,
  }) = _Task;

  factory Task.fromJson(Map<String, dynamic> json) => Task(
        id: json['id'] as String,
        title: json['name'] as String? ?? json['title'] as String? ?? '',
        description: json['description'] as String? ?? '',
        status: json['state'] as String? ?? json['status'] as String? ?? 'pending',
        agentId: json['agent_id'] as String? ?? json['assigned_agent'] as String?,
        sessionId: json['session_id'] as String?,
        createdAt: DateTime.parse(json['created_at'] as String),
        updatedAt: json['updated_at'] != null
            ? DateTime.parse(json['updated_at'] as String)
            : null,
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
        metadata: json['metadata'] as Map<String, dynamic>?,
        totalJobs: json['total_jobs'] as int?,
        completedJobs: json['completed_jobs'] as int?,
        failedJobs: json['failed_jobs'] as int?,
        steps: (json['steps'] as List?)?.map((s) => TaskStep.fromJson(s as Map<String, dynamic>)).toList(),
      );
}

@freezed
class TaskStep with _$TaskStep {
  const factory TaskStep({
    required String id,
    required String taskId,
    required String description,
    @Default('pending') String status,
    String? output,
    DateTime? completedAt,
  }) = _TaskStep;

  factory TaskStep.fromJson(Map<String, dynamic> json) => TaskStep(
        id: json['id'] as String,
        taskId: json['task_id'] as String,
        description: json['description'] as String,
        status: json['status'] as String? ?? 'pending',
        output: json['output'] as String?,
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
      );
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

  factory Agent.fromJson(Map<String, dynamic> json) => Agent(
        id: json['id'] as String,
        name: json['name'] as String,
        description: json['description'] as String? ?? '',
        enabled: json['enabled'] as bool? ?? true,
        prompt: json['prompt'] as String?,
        frontmatter: json['frontmatter'] as Map<String, dynamic>?,
      );
}

// ===== Queue/Job Models =====

@freezed
class Job with _$Job {
  const factory Job({
    required String id,
    required String type,
    @Default('pending') String status,
    String? agentId,
    @Default({}) Map<String, dynamic> payload,
    required DateTime createdAt,
    DateTime? completedAt,
    @Default(0) int retryCount,
    String? error,
  }) = _Job;

  factory Job.fromJson(Map<String, dynamic> json) => Job(
        id: json['id'] as String,
        type: json['type'] as String,
        status: json['state'] as String? ?? json['status'] as String? ?? 'pending',
        agentId: json['agent_id'] as String?,
        payload: (json['payload'] as Map<String, dynamic>?) ?? {},
        createdAt: DateTime.parse(json['created_at'] as String),
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
        retryCount: json['retry_count'] as int? ?? 0,
        error: json['error'] as String?,
      );
}

// ===== Skill/Tool Models =====

@freezed
class Skill with _$Skill {
  const factory Skill({
    @Default('') String slug,
    @Default('') String name,
    @Default('') String description,
    @Default('') String category,
    @Default([]) List<String> capabilities,
    @Default(true) bool enabled,
  }) = _Skill;

  factory Skill.fromJson(Map<String, dynamic> json) => Skill(
        slug: json['slug'] as String? ?? '',
        name: json['name'] as String? ?? '',
        description: json['description'] as String? ?? '',
        category: json['category'] as String? ?? '',
        capabilities: (json['capabilities'] as List?)?.cast<String>() ?? [],
        enabled: json['enabled'] as bool? ?? true,
      );
}

// ===== Metrics Models =====

@freezed
class MetricsSnapshot with _$MetricsSnapshot {
  const factory MetricsSnapshot({
    required DateTime timestamp,
    @Default(0) int activeAgents,
    @Default(0.0) double requestsPerSec,
    @Default(0.0) double tokenUsageRate,
    @Default(0) int queueDepth,
    @Default(0) int totalSessions,
    @Default(0) int totalJobs,
    @Default(0) int runningJobs,
    @Default(0) int pendingJobs,
    @Default('') String version,
    Map<String, dynamic>? metadata,
  }) = _MetricsSnapshot;

  factory MetricsSnapshot.fromJson(Map<String, dynamic> json) =>
      MetricsSnapshot(
        timestamp: json['timestamp'] != null
            ? DateTime.parse(json['timestamp'] as String)
            : DateTime.now(),
        activeAgents: json['active_agents'] as int? ?? 0,
        requestsPerSec: (json['requests_per_sec'] as num?)?.toDouble() ?? 0.0,
        tokenUsageRate: (json['token_usage_rate'] as num?)?.toDouble() ?? 0.0,
        queueDepth: json['queue_depth'] as int? ?? 0,
        totalSessions: json['total_sessions'] as int? ?? 0,
        totalJobs: json['total_jobs'] as int? ?? 0,
        runningJobs: json['running_jobs'] as int? ?? 0,
        pendingJobs: json['pending_jobs'] as int? ?? 0,
        version: json['version'] as String? ?? '',
        metadata: json['metadata'] as Map<String, dynamic>?,
      );
}

// ===== Plan Models =====

@freezed
class Plan with _$Plan {
  const factory Plan({
    required String id,
    required String title,
    @Default('') String description,
    @Default('') String filePath,
    String? projectID,
    required String state,
    required DateTime createdAt,
    required DateTime updatedAt,
    DateTime? approvedAt,
    DateTime? confirmedAt,
    String? approvedBy,
    String? confirmedBy,
    String? taskID,
    String? sourceSession,
    @Default(0) int revisionCount,
    @Default([]) List<PlanPhase> phases,
  }) = _Plan;

  factory Plan.fromJson(Map<String, dynamic> json) => Plan(
        id: json['id'] as String,
        title: json['title'] as String,
        description: json['description'] as String? ?? '',
        filePath: json['file_path'] as String? ?? '',
        projectID: json['project_id'] as String?,
        state: json['state'] as String,
        createdAt: DateTime.parse(json['created_at'] as String),
        updatedAt: DateTime.parse(json['updated_at'] as String),
        approvedAt: json['approved_at'] != null
            ? DateTime.parse(json['approved_at'] as String)
            : null,
        confirmedAt: json['confirmed_at'] != null
            ? DateTime.parse(json['confirmed_at'] as String)
            : null,
        approvedBy: json['approved_by'] as String?,
        confirmedBy: json['confirmed_by'] as String?,
        taskID: json['task_id'] as String?,
        sourceSession: json['source_session'] as String?,
        revisionCount: json['revision_count'] as int? ?? 0,
        phases: (json['phases'] as List?)
                ?.map((p) => PlanPhase.fromJson(p as Map<String, dynamic>))
                .toList() ??
            [],
      );
}

@freezed
class PlanPhase with _$PlanPhase {
  const factory PlanPhase({
    required String id,
    required String planID,
    required String name,
    @Default(0) int sequence,
    @Default(0) int totalSteps,
    @Default(0) int completedSteps,
    @Default(0) int failedSteps,
    required String state,
  }) = _PlanPhase;

  factory PlanPhase.fromJson(Map<String, dynamic> json) => PlanPhase(
        id: json['id'] as String,
        planID: json['plan_id'] as String,
        name: json['name'] as String,
        sequence: json['sequence'] as int? ?? 0,
        totalSteps: json['total_steps'] as int? ?? 0,
        completedSteps: json['completed_steps'] as int? ?? 0,
        failedSteps: json['failed_steps'] as int? ?? 0,
        state: json['state'] as String,
      );
}
