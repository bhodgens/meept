// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'api_models.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_$ChatMessageImpl _$$ChatMessageImplFromJson(Map<String, dynamic> json) =>
    _$ChatMessageImpl(
      id: json['id'] as String,
      role: json['role'] as String? ?? 'user',
      content: json['content'] as String? ?? '',
      timestamp: DateTime.parse(json['timestamp'] as String),
      sessionId: json['sessionId'] as String?,
      toolCalls: (json['toolCalls'] as List<dynamic>?)
          ?.map((e) => e as String)
          .toList(),
    );

Map<String, dynamic> _$$ChatMessageImplToJson(_$ChatMessageImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'role': instance.role,
      'content': instance.content,
      'timestamp': instance.timestamp.toIso8601String(),
      'sessionId': instance.sessionId,
      'toolCalls': instance.toolCalls,
    };

_$TaskImpl _$$TaskImplFromJson(Map<String, dynamic> json) => _$TaskImpl(
      id: json['id'] as String,
      title: json['title'] as String? ?? '',
      description: json['description'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      agentId: json['agentId'] as String?,
      sessionId: json['sessionId'] as String?,
      createdAt: DateTime.parse(json['createdAt'] as String),
      updatedAt: json['updatedAt'] == null
          ? null
          : DateTime.parse(json['updatedAt'] as String),
      completedAt: json['completedAt'] == null
          ? null
          : DateTime.parse(json['completedAt'] as String),
      metadata: json['metadata'] as Map<String, dynamic>?,
      totalJobs: (json['totalJobs'] as num?)?.toInt(),
      completedJobs: (json['completedJobs'] as num?)?.toInt(),
      failedJobs: (json['failedJobs'] as num?)?.toInt(),
      steps: (json['steps'] as List<dynamic>?)
          ?.map((e) => TaskStep.fromJson(e as Map<String, dynamic>))
          .toList(),
    );

Map<String, dynamic> _$$TaskImplToJson(_$TaskImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'title': instance.title,
      'description': instance.description,
      'status': instance.status,
      'agentId': instance.agentId,
      'sessionId': instance.sessionId,
      'createdAt': instance.createdAt.toIso8601String(),
      'updatedAt': instance.updatedAt?.toIso8601String(),
      'completedAt': instance.completedAt?.toIso8601String(),
      'metadata': instance.metadata,
      'totalJobs': instance.totalJobs,
      'completedJobs': instance.completedJobs,
      'failedJobs': instance.failedJobs,
      'steps': instance.steps,
    };

_$TaskStepImpl _$$TaskStepImplFromJson(Map<String, dynamic> json) =>
    _$TaskStepImpl(
      id: json['id'] as String,
      taskId: json['taskId'] as String,
      description: json['description'] as String,
      status: json['status'] as String? ?? 'pending',
      output: json['output'] as String?,
      completedAt: json['completedAt'] == null
          ? null
          : DateTime.parse(json['completedAt'] as String),
    );

Map<String, dynamic> _$$TaskStepImplToJson(_$TaskStepImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'taskId': instance.taskId,
      'description': instance.description,
      'status': instance.status,
      'output': instance.output,
      'completedAt': instance.completedAt?.toIso8601String(),
    };

_$AgentImpl _$$AgentImplFromJson(Map<String, dynamic> json) => _$AgentImpl(
      id: json['id'] as String,
      name: json['name'] as String,
      description: json['description'] as String? ?? '',
      enabled: json['enabled'] as bool? ?? true,
      prompt: json['prompt'] as String?,
      frontmatter: json['frontmatter'] as Map<String, dynamic>?,
    );

Map<String, dynamic> _$$AgentImplToJson(_$AgentImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'name': instance.name,
      'description': instance.description,
      'enabled': instance.enabled,
      'prompt': instance.prompt,
      'frontmatter': instance.frontmatter,
    };

_$JobImpl _$$JobImplFromJson(Map<String, dynamic> json) => _$JobImpl(
      id: json['id'] as String,
      type: json['type'] as String,
      status: json['status'] as String? ?? 'pending',
      agentId: json['agentId'] as String?,
      payload: json['payload'] as Map<String, dynamic>? ?? const {},
      createdAt: DateTime.parse(json['createdAt'] as String),
      completedAt: json['completedAt'] == null
          ? null
          : DateTime.parse(json['completedAt'] as String),
      retryCount: (json['retryCount'] as num?)?.toInt() ?? 0,
      error: json['error'] as String?,
    );

Map<String, dynamic> _$$JobImplToJson(_$JobImpl instance) => <String, dynamic>{
      'id': instance.id,
      'type': instance.type,
      'status': instance.status,
      'agentId': instance.agentId,
      'payload': instance.payload,
      'createdAt': instance.createdAt.toIso8601String(),
      'completedAt': instance.completedAt?.toIso8601String(),
      'retryCount': instance.retryCount,
      'error': instance.error,
    };

_$SkillImpl _$$SkillImplFromJson(Map<String, dynamic> json) => _$SkillImpl(
      slug: json['slug'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      category: json['category'] as String? ?? '',
      capabilities: (json['capabilities'] as List<dynamic>?)
              ?.map((e) => e as String)
              .toList() ??
          const [],
      enabled: json['enabled'] as bool? ?? true,
    );

Map<String, dynamic> _$$SkillImplToJson(_$SkillImpl instance) =>
    <String, dynamic>{
      'slug': instance.slug,
      'name': instance.name,
      'description': instance.description,
      'category': instance.category,
      'capabilities': instance.capabilities,
      'enabled': instance.enabled,
    };

_$MetricsSnapshotImpl _$$MetricsSnapshotImplFromJson(
        Map<String, dynamic> json) =>
    _$MetricsSnapshotImpl(
      timestamp: DateTime.parse(json['timestamp'] as String),
      activeAgents: (json['activeAgents'] as num?)?.toInt() ?? 0,
      requestsPerSec: (json['requestsPerSec'] as num?)?.toDouble() ?? 0.0,
      tokenUsageRate: (json['tokenUsageRate'] as num?)?.toDouble() ?? 0.0,
      queueDepth: (json['queueDepth'] as num?)?.toInt() ?? 0,
      totalSessions: (json['totalSessions'] as num?)?.toInt() ?? 0,
      totalJobs: (json['totalJobs'] as num?)?.toInt() ?? 0,
      runningJobs: (json['runningJobs'] as num?)?.toInt() ?? 0,
      pendingJobs: (json['pendingJobs'] as num?)?.toInt() ?? 0,
      version: json['version'] as String? ?? '',
      metadata: json['metadata'] as Map<String, dynamic>?,
    );

Map<String, dynamic> _$$MetricsSnapshotImplToJson(
        _$MetricsSnapshotImpl instance) =>
    <String, dynamic>{
      'timestamp': instance.timestamp.toIso8601String(),
      'activeAgents': instance.activeAgents,
      'requestsPerSec': instance.requestsPerSec,
      'tokenUsageRate': instance.tokenUsageRate,
      'queueDepth': instance.queueDepth,
      'totalSessions': instance.totalSessions,
      'totalJobs': instance.totalJobs,
      'runningJobs': instance.runningJobs,
      'pendingJobs': instance.pendingJobs,
      'version': instance.version,
      'metadata': instance.metadata,
    };

_$PlanImpl _$$PlanImplFromJson(Map<String, dynamic> json) => _$PlanImpl(
      id: json['id'] as String,
      title: json['title'] as String,
      description: json['description'] as String? ?? '',
      filePath: json['filePath'] as String? ?? '',
      projectID: json['projectID'] as String?,
      state: json['state'] as String,
      createdAt: DateTime.parse(json['createdAt'] as String),
      updatedAt: DateTime.parse(json['updatedAt'] as String),
      approvedAt: json['approvedAt'] == null
          ? null
          : DateTime.parse(json['approvedAt'] as String),
      confirmedAt: json['confirmedAt'] == null
          ? null
          : DateTime.parse(json['confirmedAt'] as String),
      approvedBy: json['approvedBy'] as String?,
      confirmedBy: json['confirmedBy'] as String?,
      taskID: json['taskID'] as String?,
      sourceSession: json['sourceSession'] as String?,
      revisionCount: (json['revisionCount'] as num?)?.toInt() ?? 0,
      phases: (json['phases'] as List<dynamic>?)
              ?.map((e) => PlanPhase.fromJson(e as Map<String, dynamic>))
              .toList() ??
          const [],
    );

Map<String, dynamic> _$$PlanImplToJson(_$PlanImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'title': instance.title,
      'description': instance.description,
      'filePath': instance.filePath,
      'projectID': instance.projectID,
      'state': instance.state,
      'createdAt': instance.createdAt.toIso8601String(),
      'updatedAt': instance.updatedAt.toIso8601String(),
      'approvedAt': instance.approvedAt?.toIso8601String(),
      'confirmedAt': instance.confirmedAt?.toIso8601String(),
      'approvedBy': instance.approvedBy,
      'confirmedBy': instance.confirmedBy,
      'taskID': instance.taskID,
      'sourceSession': instance.sourceSession,
      'revisionCount': instance.revisionCount,
      'phases': instance.phases,
    };

_$PlanPhaseImpl _$$PlanPhaseImplFromJson(Map<String, dynamic> json) =>
    _$PlanPhaseImpl(
      id: json['id'] as String,
      planID: json['planID'] as String,
      name: json['name'] as String,
      sequence: (json['sequence'] as num?)?.toInt() ?? 0,
      totalSteps: (json['totalSteps'] as num?)?.toInt() ?? 0,
      completedSteps: (json['completedSteps'] as num?)?.toInt() ?? 0,
      failedSteps: (json['failedSteps'] as num?)?.toInt() ?? 0,
      state: json['state'] as String,
    );

Map<String, dynamic> _$$PlanPhaseImplToJson(_$PlanPhaseImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'planID': instance.planID,
      'name': instance.name,
      'sequence': instance.sequence,
      'totalSteps': instance.totalSteps,
      'completedSteps': instance.completedSteps,
      'failedSteps': instance.failedSteps,
      'state': instance.state,
    };
