import 'package:equatable/equatable.dart';

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

class ChatMessage extends Equatable {
  final String id;
  final String role; // 'user', 'assistant', 'system'
  final String content;
  final DateTime timestamp;
  final String? sessionId;
  final List<String>? toolCalls;

  const ChatMessage({
    required this.id,
    required this.role,
    required this.content,
    required this.timestamp,
    this.sessionId,
    this.toolCalls,
  });

  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
        id: json['id'] as String,
        role: json['role'] as String,
        content: json['content'] as String,
        timestamp: DateTime.parse(json['timestamp'] as String),
        sessionId: json['session_id'] as String?,
        toolCalls: (json['tool_calls'] as List?)?.cast<String>(),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'role': role,
        'content': content,
        'timestamp': timestamp.toIso8601String(),
        'session_id': sessionId,
        'tool_calls': toolCalls,
      };

  @override
  List<Object?> get props => [id, role, content, sessionId, toolCalls];
}

class ChatRequest extends Equatable {
  final String message;
  final String? sessionId;
  final String? agentId;
  final List<ChatMessage>? history;

  const ChatRequest({
    required this.message,
    this.sessionId,
    this.agentId,
    this.history,
  });

  Map<String, dynamic> toJson() => {
        'message': message,
        if (sessionId != null) 'session_id': sessionId,
        if (agentId != null) 'agent_id': agentId,
        if (history != null) 'history': history!.map((m) => m.toJson()).toList(),
      };

  @override
  List<Object?> get props => [message, sessionId, agentId, history];
}

// ===== Session Models =====

class Session extends Equatable {
  final String id;
  final String title; // Backend returns 'name', we map it to 'title'
  final String? description;
  final String? conversationId;
  final DateTime createdAt;
  final DateTime? lastActivity;
  final List<String>? attachedClients;

  const Session({
    required this.id,
    required this.title,
    this.description,
    this.conversationId,
    required this.createdAt,
    this.lastActivity,
    this.attachedClients,
  });

  factory Session.fromJson(Map<String, dynamic> json) => Session(
        id: json['id'] as String,
        title: json['name'] as String? ?? json['title'] as String? ?? 'Untitled',
        description: json['description'] as String?,
        conversationId: json['conversation_id'] as String?,
        createdAt: DateTime.parse(json['created_at'] as String),
        lastActivity: json['last_activity'] != null
            ? DateTime.parse(json['last_activity'] as String)
            : null,
        attachedClients: (json['attached_clients'] as List?)
            ?.cast<String>(),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': title,
        if (description != null) 'description': description,
        if (conversationId != null) 'conversation_id': conversationId,
        'created_at': createdAt.toIso8601String(),
        if (lastActivity != null) 'last_activity': lastActivity!.toIso8601String(),
        if (attachedClients != null) 'attached_clients': attachedClients,
      };

  @override
  List<Object?> get props => [id, title, description, conversationId, createdAt, lastActivity];
}

// ===== Task Models =====

class Task extends Equatable {
  final String id;
  final String title; // Backend returns 'name'
  final String description;
  final String status; // Backend returns 'state': 'pending', 'in_progress', 'completed', 'failed'
  final String? agentId;
  final String? sessionId;
  final DateTime createdAt;
  final DateTime? updatedAt;
  final DateTime? completedAt;
  final Map<String, dynamic>? metadata;
  final int? totalJobs;
  final int? completedJobs;
  final int? failedJobs;
  final List<TaskStep>? steps;

  const Task({
    required this.id,
    required this.title,
    required this.description,
    required this.status,
    this.agentId,
    this.sessionId,
    required this.createdAt,
    this.updatedAt,
    this.completedAt,
    this.metadata,
    this.totalJobs,
    this.completedJobs,
    this.failedJobs,
    this.steps,
  });

  factory Task.fromJson(Map<String, dynamic> json) => Task(
        id: json['id'] as String,
        title: json['name'] as String? ?? json['title'] as String? ?? '',
        description: json['description'] as String? ?? '',
        status: json['state'] as String? ?? json['status'] as String? ?? 'pending',
        agentId: json['agent_id'] as String?,
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
        steps: (json['steps'] as List?)
            ?.map((s) => TaskStep.fromJson(s as Map<String, dynamic>))
            .toList(),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': title,
        'description': description,
        'state': status,
        if (agentId != null) 'agent_id': agentId,
        if (sessionId != null) 'session_id': sessionId,
        'created_at': createdAt.toIso8601String(),
        if (updatedAt != null) 'updated_at': updatedAt!.toIso8601String(),
        if (completedAt != null) 'completed_at': completedAt!.toIso8601String(),
        if (metadata != null) 'metadata': metadata,
        if (totalJobs != null) 'total_jobs': totalJobs,
        if (completedJobs != null) 'completed_jobs': completedJobs,
        if (failedJobs != null) 'failed_jobs': failedJobs,
        if (steps != null) 'steps': steps!.map((s) => s.toJson()).toList(),
      };

  @override
  List<Object?> get props =>
      [id, title, description, status, agentId, sessionId];
}

class TaskStep extends Equatable {
  final String id;
  final String taskId;
  final String description;
  final String status;
  final String? output;
  final DateTime? completedAt;

  const TaskStep({
    required this.id,
    required this.taskId,
    required this.description,
    required this.status,
    this.output,
    this.completedAt,
  });

  factory TaskStep.fromJson(Map<String, dynamic> json) => TaskStep(
        id: json['id'] as String,
        taskId: json['task_id'] as String,
        description: json['description'] as String,
        status: json['status'] as String,
        output: json['output'] as String?,
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'task_id': taskId,
        'description': description,
        'status': status,
        if (output != null) 'output': output,
        if (completedAt != null)
          'completed_at': completedAt!.toIso8601String(),
      };

  @override
  List<Object?> get props => [id, taskId, description, status, output];
}

// ===== Agent Models =====

class Agent extends Equatable {
  final String id;
  final String name;
  final String description;
  final bool enabled;
  final String? prompt; // Optional - backend may not return this
  final Map<String, dynamic>? frontmatter;

  const Agent({
    required this.id,
    required this.name,
    required this.description,
    required this.enabled,
    this.prompt,
    this.frontmatter,
  });

  factory Agent.fromJson(Map<String, dynamic> json) => Agent(
        id: json['id'] as String,
        name: json['name'] as String,
        description: json['description'] as String? ?? '',
        enabled: json['enabled'] as bool? ?? true,
        prompt: json['prompt'] as String?,
        frontmatter: json['frontmatter'] as Map<String, dynamic>?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'description': description,
        'enabled': enabled,
        if (prompt != null) 'prompt': prompt,
        if (frontmatter != null) 'frontmatter': frontmatter,
      };

  @override
  List<Object?> get props => [id, name, description, enabled, prompt];
}

// ===== Queue/Job Models =====

class Job extends Equatable {
  final String id;
  final String type;
  final String status; // 'pending', 'running', 'completed', 'failed'
  final String? agentId;
  final Map<String, dynamic>? payload;
  final DateTime createdAt;
  final DateTime? completedAt;
  final int retryCount;
  final String? error;

  const Job({
    required this.id,
    required this.type,
    required this.status,
    this.agentId,
    this.payload,
    required this.createdAt,
    this.completedAt,
    this.retryCount = 0,
    this.error,
  });

  factory Job.fromJson(Map<String, dynamic> json) => Job(
        id: json['id'] as String,
        type: json['type'] as String,
        status: json['status'] as String,
        agentId: json['agent_id'] as String?,
        payload: json['payload'] as Map<String, dynamic>?,
        createdAt: DateTime.parse(json['created_at'] as String),
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
        retryCount: json['retry_count'] as int? ?? 0,
        error: json['error'] as String?,
      );

  @override
  List<Object?> get props => [id, type, status, agentId];
}

// ===== Skill/Tool Models =====

class Skill extends Equatable {
  final String slug;
  final String name;
  final String description;
  final String category;
  final List<String> capabilities;
  final bool enabled;

  const Skill({
    required this.slug,
    required this.name,
    required this.description,
    this.category = '',
    this.capabilities = const [],
    this.enabled = true,
  });

  factory Skill.fromJson(Map<String, dynamic> json) => Skill(
        slug: json['slug'] as String? ?? '',
        name: json['name'] as String? ?? '',
        description: json['description'] as String? ?? '',
        category: json['category'] as String? ?? '',
        capabilities:
            (json['capabilities'] as List?)?.cast<String>() ?? [],
        enabled: json['enabled'] as bool? ?? true,
      );

  @override
  List<Object?> get props => [slug, name, description, category, enabled];
}

// ===== Metrics Models =====

class MetricsSnapshot extends Equatable {
  final DateTime timestamp;
  final int activeAgents;
  final double requestsPerSec;
  final double tokenUsageRate;
  final int queueDepth;
  final int totalSessions;
  final int totalJobs;
  final int runningJobs;
  final int pendingJobs;
  final String version;
  final Map<String, dynamic>? metadata;

  const MetricsSnapshot({
    required this.timestamp,
    this.activeAgents = 0,
    this.requestsPerSec = 0.0,
    this.tokenUsageRate = 0.0,
    this.queueDepth = 0,
    this.totalSessions = 0,
    this.totalJobs = 0,
    this.runningJobs = 0,
    this.pendingJobs = 0,
    this.version = '',
    this.metadata,
  });

  factory MetricsSnapshot.fromJson(Map<String, dynamic> json) {
    return MetricsSnapshot(
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

  @override
  List<Object?> get props => [
        timestamp, activeAgents, requestsPerSec, tokenUsageRate,
        queueDepth, totalSessions, totalJobs, runningJobs, pendingJobs,
        version,
      ];
}
