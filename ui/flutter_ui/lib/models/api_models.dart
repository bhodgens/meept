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
  final String title;
  final DateTime createdAt;
  final DateTime updatedAt;
  final String? lastAgentId;
  final int messageCount;
  final Map<String, dynamic>? metadata;

  const Session({
    required this.id,
    required this.title,
    required this.createdAt,
    required this.updatedAt,
    this.lastAgentId,
    this.messageCount = 0,
    this.metadata,
  });

  factory Session.fromJson(Map<String, dynamic> json) => Session(
        id: json['id'] as String,
        title: json['title'] as String,
        createdAt: DateTime.parse(json['created_at'] as String),
        updatedAt: DateTime.parse(json['updated_at'] as String),
        lastAgentId: json['last_agent_id'] as String?,
        messageCount: json['message_count'] as int? ?? 0,
        metadata: json['metadata'] as Map<String, dynamic>?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'title': title,
        'created_at': createdAt.toIso8601String(),
        'updated_at': updatedAt.toIso8601String(),
        if (lastAgentId != null) 'last_agent_id': lastAgentId,
        'message_count': messageCount,
        if (metadata != null) 'metadata': metadata,
      };

  @override
  List<Object?> get props => [id, title, createdAt, updatedAt, lastAgentId];
}

// ===== Task Models =====

class Task extends Equatable {
  final String id;
  final String title;
  final String description;
  final String status; // 'pending', 'in_progress', 'completed', 'failed'
  final String? agentId;
  final String? sessionId;
  final DateTime createdAt;
  final DateTime? completedAt;
  final List<TaskStep>? steps;

  const Task({
    required this.id,
    required this.title,
    required this.description,
    required this.status,
    this.agentId,
    this.sessionId,
    required this.createdAt,
    this.completedAt,
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
        completedAt: json['completed_at'] != null
            ? DateTime.parse(json['completed_at'] as String)
            : null,
        steps: (json['steps'] as List?)
            ?.map((s) => TaskStep.fromJson(s as Map<String, dynamic>))
            .toList(),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'title': title,
        'description': description,
        'status': status,
        if (agentId != null) 'agent_id': agentId,
        if (sessionId != null) 'session_id': sessionId,
        'created_at': createdAt.toIso8601String(),
        if (completedAt != null)
          'completed_at': completedAt!.toIso8601String(),
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
  final String prompt;
  final bool enabled;
  final Map<String, dynamic>? frontmatter;

  const Agent({
    required this.id,
    required this.name,
    required this.description,
    required this.prompt,
    required this.enabled,
    this.frontmatter,
  });

  factory Agent.fromJson(Map<String, dynamic> json) => Agent(
        id: json['id'] as String,
        name: json['name'] as String,
        description: json['description'] as String,
        prompt: json['prompt'] as String,
        enabled: json['enabled'] as bool,
        frontmatter: json['frontmatter'] as Map<String, dynamic>?,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'description': description,
        'prompt': prompt,
        'enabled': enabled,
        if (frontmatter != null) 'frontmatter': frontmatter,
      };

  @override
  List<Object?> get props => [id, name, description, prompt, enabled];
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
