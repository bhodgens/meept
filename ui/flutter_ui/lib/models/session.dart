import 'package:equatable/equatable.dart';

/// Session data model - represents a chat session
class Session extends Equatable {
  final String id;
  final String title;
  final DateTime createdAt;
  final DateTime lastActivityAt;
  final Duration duration;
  final int tokenCount;
  final List<String> taskIds;
  final String status;

  const Session({
    required this.id,
    required this.title,
    required this.createdAt,
    required this.lastActivityAt,
    required this.duration,
    required this.tokenCount,
    required this.taskIds,
    required this.status,
  });

  /// Create Session from JSON map
  factory Session.fromJson(Map<String, dynamic> json) {
    return Session(
      id: json['id'] as String,
      title: json['title'] as String? ?? 'untitled session',
      createdAt: DateTime.parse(json['created_at'] as String),
      lastActivityAt: DateTime.parse(json['last_activity_at'] as String),
      duration: Duration(seconds: json['duration_seconds'] as int? ?? 0),
      tokenCount: json['token_count'] as int? ?? 0,
      taskIds: (json['task_ids'] as List<dynamic>?)?.cast<String>() ?? [],
      status: json['status'] as String? ?? 'unknown',
    );
  }

  /// Convert Session to JSON map
  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'title': title,
      'created_at': createdAt.toIso8601String(),
      'last_activity_at': lastActivityAt.toIso8601String(),
      'duration_seconds': duration.inSeconds,
      'token_count': tokenCount,
      'task_ids': taskIds,
      'status': status,
    };
  }

  @override
  List<Object?> get props => [
        id,
        title,
        createdAt,
        lastActivityAt,
        duration,
        tokenCount,
        taskIds,
        status,
      ];
}
