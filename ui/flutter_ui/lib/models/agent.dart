import 'package:equatable/equatable.dart';

/// Agent status enum
enum AgentStatus { idle, working, complete, error }

/// Agent data model - represents an agent working on tasks
class Agent extends Equatable {
  final String id;
  final String name;
  final AgentStatus status;
  final String? currentTaskId;
  final String? transcript;
  final DateTime? lastActiveAt;

  const Agent({
    required this.id,
    required this.name,
    required this.status,
    this.currentTaskId,
    this.transcript,
    this.lastActiveAt,
  });

  /// Create Agent from JSON map
  factory Agent.fromJson(Map<String, dynamic> json) {
    return Agent(
      id: json['id'] as String,
      name: json['name'] as String? ?? 'agent-${json['id']}',
      status: _parseStatus(json['status'] as String?),
      currentTaskId: json['current_task_id'] as String?,
      transcript: json['transcript'] as String?,
      lastActiveAt: json['last_active_at'] != null
          ? DateTime.parse(json['last_active_at'] as String)
          : null,
    );
  }

  /// Convert Agent to JSON map
  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'name': name,
      'status': _statusToString(status),
      'current_task_id': currentTaskId,
      'transcript': transcript,
      'last_active_at': lastActiveAt?.toIso8601String(),
    };
  }

  static AgentStatus _parseStatus(String? status) {
    if (status == null) return AgentStatus.idle;
    switch (status.toLowerCase()) {
      case 'working':
      case 'busy':
        return AgentStatus.working;
      case 'complete':
      case 'completed':
        return AgentStatus.complete;
      case 'error':
      case 'failed':
        return AgentStatus.error;
      default:
        return AgentStatus.idle;
    }
  }

  String _statusToString(AgentStatus status) {
    switch (status) {
      case AgentStatus.idle:
        return 'idle';
      case AgentStatus.working:
        return 'working';
      case AgentStatus.complete:
        return 'complete';
      case AgentStatus.error:
        return 'error';
    }
  }

  @override
  List<Object?> get props => [
        id,
        name,
        status,
        currentTaskId,
        transcript,
        lastActiveAt,
      ];
}
