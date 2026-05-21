import 'package:equatable/equatable.dart';

/// Task status enum
enum TaskStatus { pending, running, complete, error }

/// Task data model - represents a task with agents
class Task extends Equatable {
  final String id;
  final String title;
  final TaskStatus status;
  final DateTime createdAt;
  final DateTime? lastActivityAt;
  final List<String> agentIds;
  final String? sessionId;

  const Task({
    required this.id,
    required this.title,
    required this.status,
    required this.createdAt,
    this.lastActivityAt,
    required this.agentIds,
    this.sessionId,
  });

  /// Create Task from JSON map
  factory Task.fromJson(Map<String, dynamic> json) {
    return Task(
      id: json['id'] as String,
      title: json['title'] as String? ?? 'untitled task',
      status: _parseStatus(json['status'] as String?),
      createdAt: DateTime.parse(json['created_at'] as String),
      lastActivityAt: json['last_activity_at'] != null
          ? DateTime.parse(json['last_activity_at'] as String)
          : null,
      agentIds: (json['agent_ids'] as List<dynamic>?)?.cast<String>() ?? [],
      sessionId: json['session_id'] as String?,
    );
  }

  /// Convert Task to JSON map
  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'status': _statusToString(status),
      'created_at': createdAt.toIso8601String(),
      'last_activity_at': lastActivityAt?.toIso8601String(),
      'agent_ids': agentIds,
      'session_id': sessionId,
    };
  }

  static TaskStatus _parseStatus(String? status) {
    if (status == null) return TaskStatus.pending;
    switch (status.toLowerCase()) {
      case 'running':
        return TaskStatus.running;
      case 'complete':
      case 'completed':
        return TaskStatus.complete;
      case 'error':
      case 'failed':
        return TaskStatus.error;
      default:
        return TaskStatus.pending;
    }
  }

  String _statusToString(TaskStatus status) {
    switch (status) {
      case TaskStatus.pending:
        return 'pending';
      case TaskStatus.running:
        return 'running';
      case TaskStatus.complete:
        return 'complete';
      case TaskStatus.error:
        return 'error';
    }
  }

  @override
  List<Object?> get props => [
        id,
        title,
        status,
        createdAt,
        lastActivityAt,
        agentIds,
        sessionId,
      ];
}
