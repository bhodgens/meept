import 'package:flutter/foundation.dart';

import 'sdk_client.dart';

/// Thread model matching the backend session.Thread struct.
class Thread {
  final String id;
  final String sessionId;
  final String topicLabel;
  final String conversationId;
  final DateTime createdAt;
  final DateTime lastActivityAt;
  final String? summary;
  final bool isActive;

  Thread({
    required this.id,
    required this.sessionId,
    required this.topicLabel,
    required this.conversationId,
    required this.createdAt,
    required this.lastActivityAt,
    this.summary,
    required this.isActive,
  });

  factory Thread.fromJson(Map<String, dynamic> json) {
    return Thread(
      id: json['id'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      topicLabel: json['topic_label'] as String? ?? 'general',
      conversationId: json['conversation_id'] as String? ?? '',
      createdAt: json['created_at'] != null
          ? DateTime.tryParse(json['created_at'] as String) ?? DateTime.now()
          : DateTime.now(),
      lastActivityAt: json['last_activity'] != null
          ? DateTime.tryParse(json['last_activity'] as String) ??
              DateTime.now()
          : DateTime.now(),
      summary: json['summary'] as String?,
      isActive: json['is_active'] == true || json['is_active'] == 1,
    );
  }
}

/// ThreadService manages conversation threads via the HTTP API.
///
/// Wraps [SdkApiClient] calls to the thread endpoints under
/// `/api/v1/sessions/{id}/threads`.
class ThreadService {
  final SdkApiClient _client;

  ThreadService(this._client);

  /// List all threads for a session.
  Future<List<Thread>> listThreads(String sessionId) async {
    try {
      final raw =
          await _client.dio.get('/api/v1/sessions/$sessionId/threads');
      final data = raw.data as Map<String, dynamic>?;
      final threadsRaw = data?['threads'] as List?;
      if (threadsRaw == null) return [];
      return threadsRaw
          .whereType<Map>()
          .map((t) => Thread.fromJson(Map<String, dynamic>.from(t)))
          .toList(growable: false);
    } catch (e) {
      debugPrint('[thread-service] listThreads failed: $e');
      return [];
    }
  }

  /// Create a new thread for a session.
  Future<Thread?> createThread(
    String sessionId, {
    String topicLabel = 'general',
    String? conversationId,
  }) async {
    try {
      final body = <String, dynamic>{
        'topic_label': topicLabel,
        if (conversationId != null) 'conversation_id': conversationId,
      };
      final raw = await _client.dio.post(
        '/api/v1/sessions/$sessionId/threads',
        data: body,
      );
      final data = raw.data as Map<String, dynamic>?;
      if (data == null) return null;
      return Thread.fromJson(data);
    } catch (e) {
      debugPrint('[thread-service] createThread failed: $e');
      return null;
    }
  }

  /// Get the active thread for a session.
  Future<Thread?> getActiveThread(String sessionId) async {
    try {
      final raw = await _client.dio
          .get('/api/v1/sessions/$sessionId/threads/active');
      final data = raw.data as Map<String, dynamic>?;
      if (data == null) return null;
      return Thread.fromJson(data);
    } catch (e) {
      debugPrint('[thread-service] getActiveThread failed: $e');
      return null;
    }
  }

  /// Set the active thread for a session.
  Future<Thread?> setActiveThread(
    String sessionId,
    String threadId,
  ) async {
    try {
      final raw = await _client.dio.put(
        '/api/v1/sessions/$sessionId/threads/active',
        data: {'thread_id': threadId},
      );
      final data = raw.data as Map<String, dynamic>?;
      if (data == null) return null;
      return Thread.fromJson(data);
    } catch (e) {
      debugPrint('[thread-service] setActiveThread failed: $e');
      return null;
    }
  }

  /// Delete a thread by its ID.
  Future<bool> deleteThread(String sessionId, String threadId) async {
    try {
      await _client.dio
          .delete('/api/v1/sessions/$sessionId/threads/$threadId');
      return true;
    } catch (e) {
      debugPrint('[thread-service] deleteThread failed: $e');
      return false;
    }
  }
}
