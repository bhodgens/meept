// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'api_models.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_$ChatMessageImpl _$$ChatMessageImplFromJson(Map<String, dynamic> json) =>
    _$ChatMessageImpl(
      id: json['id'] as String,
      role: json['role'] as String,
      content: json['content'] as String,
      timestamp: DateTime.parse(json['timestamp'] as String),
      sessionId: json['session_id'] as String?,
      toolCalls: (json['tool_calls'] as List<dynamic>?)
          ?.map((e) => e as String)
          .toList(),
    );

Map<String, dynamic> _$$ChatMessageImplToJson(_$ChatMessageImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'role': instance.role,
      'content': instance.content,
      'timestamp': instance.timestamp.toIso8601String(),
      'session_id': instance.sessionId,
      'tool_calls': instance.toolCalls,
    };

_$ChatRequestImpl _$$ChatRequestImplFromJson(Map<String, dynamic> json) =>
    _$ChatRequestImpl(
      message: json['message'] as String,
      conversationId: json['conversation_id'] as String?,
      agentId: json['agent_id'] as String?,
      history: (json['history'] as List<dynamic>?)
          ?.map((e) => ChatMessage.fromJson(e as Map<String, dynamic>))
          .toList(),
    );

Map<String, dynamic> _$$ChatRequestImplToJson(_$ChatRequestImpl instance) =>
    <String, dynamic>{
      'message': instance.message,
      'conversation_id': instance.conversationId,
      'agent_id': instance.agentId,
      'history': instance.history,
    };

_$SessionImpl _$$SessionImplFromJson(Map<String, dynamic> json) =>
    _$SessionImpl(
      id: json['id'] as String,
      title: json['name'] as String,
      description: json['description'] as String?,
      conversationId: json['conversation_id'] as String?,
      createdAt: DateTime.parse(json['created_at'] as String),
      lastActivity: json['last_activity'] == null
          ? null
          : DateTime.parse(json['last_activity'] as String),
      attachedClients: (json['attached_clients'] as List<dynamic>?)
          ?.map((e) => e as String)
          .toList(),
    );

Map<String, dynamic> _$$SessionImplToJson(_$SessionImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'name': instance.title,
      'description': instance.description,
      'conversation_id': instance.conversationId,
      'created_at': instance.createdAt.toIso8601String(),
      'last_activity': instance.lastActivity?.toIso8601String(),
      'attached_clients': instance.attachedClients,
    };

_$TaskImpl _$$TaskImplFromJson(Map<String, dynamic> json) => _$TaskImpl(
      id: json['id'] as String,
      title: json['name'] as String,
      description: json['description'] as String,
      status: json['state'] as String,
      agentId: json['agent_id'] as String?,
      sessionId: json['session_id'] as String?,
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: json['updated_at'] == null
          ? null
          : DateTime.parse(json['updated_at'] as String),
      completedAt: json['completed_at'] == null
          ? null
          : DateTime.parse(json['completed_at'] as String),
      metadata: json['metadata'] as Map<String, dynamic>?,
      totalJobs: (json['total_jobs'] as num?)?.toInt(),
      completedJobs: (json['completed_jobs'] as num?)?.toInt(),
      failedJobs: (json['failed_jobs'] as num?)?.toInt(),
      steps: (json['steps'] as List<dynamic>?)
          ?.map((e) => TaskStep.fromJson(e as Map<String, dynamic>))
          .toList(),
    );

Map<String, dynamic> _$$TaskImplToJson(_$TaskImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'name': instance.title,
      'description': instance.description,
      'state': instance.status,
      'agent_id': instance.agentId,
      'session_id': instance.sessionId,
      'created_at': instance.createdAt.toIso8601String(),
      'updated_at': instance.updatedAt?.toIso8601String(),
      'completed_at': instance.completedAt?.toIso8601String(),
      'metadata': instance.metadata,
      'total_jobs': instance.totalJobs,
      'completed_jobs': instance.completedJobs,
      'failed_jobs': instance.failedJobs,
      'steps': instance.steps,
    };

_$TaskStepImpl _$$TaskStepImplFromJson(Map<String, dynamic> json) =>
    _$TaskStepImpl(
      id: json['id'] as String,
      taskId: json['task_id'] as String,
      description: json['description'] as String,
      status: json['status'] as String,
      output: json['output'] as String?,
      completedAt: json['completed_at'] == null
          ? null
          : DateTime.parse(json['completed_at'] as String),
    );

Map<String, dynamic> _$$TaskStepImplToJson(_$TaskStepImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'task_id': instance.taskId,
      'description': instance.description,
      'status': instance.status,
      'output': instance.output,
      'completed_at': instance.completedAt?.toIso8601String(),
    };

_$AgentImpl _$$AgentImplFromJson(Map<String, dynamic> json) => _$AgentImpl(
      id: json['id'] as String,
      name: json['name'] as String,
      description: json['description'] as String,
      enabled: json['enabled'] as bool,
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
      status: json['state'] as String,
      agentId: json['agent_id'] as String?,
      payload: json['payload'] as Map<String, dynamic>?,
      createdAt: DateTime.parse(json['created_at'] as String),
      completedAt: json['completed_at'] == null
          ? null
          : DateTime.parse(json['completed_at'] as String),
      retryCount: (json['retry_count'] as num?)?.toInt() ?? 0,
      error: json['error'] as String?,
    );

Map<String, dynamic> _$$JobImplToJson(_$JobImpl instance) => <String, dynamic>{
      'id': instance.id,
      'type': instance.type,
      'state': instance.status,
      'agent_id': instance.agentId,
      'payload': instance.payload,
      'created_at': instance.createdAt.toIso8601String(),
      'completed_at': instance.completedAt?.toIso8601String(),
      'retry_count': instance.retryCount,
      'error': instance.error,
    };

_$SkillImpl _$$SkillImplFromJson(Map<String, dynamic> json) => _$SkillImpl(
      slug: json['slug'] as String,
      name: json['name'] as String,
      description: json['description'] as String,
      category: json['category'] as String? ?? '',
      capabilities: (json['capabilities'] as List<dynamic>?)
              ?.map((e) => e as String)
              .toList() ??
          const [],
      tags:
          (json['tags'] as List<dynamic>?)?.map((e) => e as String).toList() ??
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
      'tags': instance.tags,
      'enabled': instance.enabled,
    };

_$MetricsSnapshotImpl _$$MetricsSnapshotImplFromJson(
        Map<String, dynamic> json) =>
    _$MetricsSnapshotImpl(
      timestamp: DateTime.parse(json['timestamp'] as String),
      activeAgents: (json['active_agents'] as num?)?.toInt() ?? 0,
      requestsPerSec: (json['requests_per_sec'] as num?)?.toDouble() ?? 0.0,
      tokenUsageRate: (json['token_usage_rate'] as num?)?.toDouble() ?? 0.0,
      queueDepth: (json['queue_depth'] as num?)?.toInt() ?? 0,
      totalSessions: (json['total_sessions'] as num?)?.toInt() ?? 0,
      totalJobs: (json['total_jobs'] as num?)?.toInt() ?? 0,
      runningJobs: (json['running_jobs'] as num?)?.toInt() ?? 0,
      pendingJobs: (json['pending_jobs'] as num?)?.toInt() ?? 0,
      version: json['version'] as String? ?? '',
      metadata: json['metadata'] as Map<String, dynamic>?,
    );

Map<String, dynamic> _$$MetricsSnapshotImplToJson(
        _$MetricsSnapshotImpl instance) =>
    <String, dynamic>{
      'timestamp': instance.timestamp.toIso8601String(),
      'active_agents': instance.activeAgents,
      'requests_per_sec': instance.requestsPerSec,
      'token_usage_rate': instance.tokenUsageRate,
      'queue_depth': instance.queueDepth,
      'total_sessions': instance.totalSessions,
      'total_jobs': instance.totalJobs,
      'running_jobs': instance.runningJobs,
      'pending_jobs': instance.pendingJobs,
      'version': instance.version,
      'metadata': instance.metadata,
    };

_$PlanImpl _$$PlanImplFromJson(Map<String, dynamic> json) => _$PlanImpl(
      id: json['id'] as String,
      title: json['title'] as String,
      description: json['description'] as String? ?? '',
      filePath: json['file_path'] as String? ?? '',
      projectID: json['project_id'] as String?,
      state: json['state'] as String,
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: DateTime.parse(json['updated_at'] as String),
      approvedAt: json['approved_at'] == null
          ? null
          : DateTime.parse(json['approved_at'] as String),
      confirmedAt: json['confirmed_at'] == null
          ? null
          : DateTime.parse(json['confirmed_at'] as String),
      approvedBy: json['approved_by'] as String?,
      confirmedBy: json['confirmed_by'] as String?,
      taskID: json['task_id'] as String?,
      sourceSession: json['source_session'] as String?,
      revisionCount: (json['revision_count'] as num?)?.toInt() ?? 0,
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
      'file_path': instance.filePath,
      'project_id': instance.projectID,
      'state': instance.state,
      'created_at': instance.createdAt.toIso8601String(),
      'updated_at': instance.updatedAt.toIso8601String(),
      'approved_at': instance.approvedAt?.toIso8601String(),
      'confirmed_at': instance.confirmedAt?.toIso8601String(),
      'approved_by': instance.approvedBy,
      'confirmed_by': instance.confirmedBy,
      'task_id': instance.taskID,
      'source_session': instance.sourceSession,
      'revision_count': instance.revisionCount,
      'phases': instance.phases,
    };

_$PlanPhaseImpl _$$PlanPhaseImplFromJson(Map<String, dynamic> json) =>
    _$PlanPhaseImpl(
      id: json['id'] as String,
      planID: json['plan_id'] as String,
      name: json['name'] as String,
      sequence: (json['sequence'] as num).toInt(),
      totalSteps: (json['total_steps'] as num?)?.toInt() ?? 0,
      completedSteps: (json['completed_steps'] as num?)?.toInt() ?? 0,
      failedSteps: (json['failed_steps'] as num?)?.toInt() ?? 0,
      state: json['state'] as String,
    );

Map<String, dynamic> _$$PlanPhaseImplToJson(_$PlanPhaseImpl instance) =>
    <String, dynamic>{
      'id': instance.id,
      'plan_id': instance.planID,
      'name': instance.name,
      'sequence': instance.sequence,
      'total_steps': instance.totalSteps,
      'completed_steps': instance.completedSteps,
      'failed_steps': instance.failedSteps,
      'state': instance.state,
    };

_$SearchResultsImpl _$$SearchResultsImplFromJson(Map<String, dynamic> json) =>
    _$SearchResultsImpl(
      results: (json['results'] as List<dynamic>?)
              ?.map((e) => SearchResultItem.fromJson(e as Map<String, dynamic>))
              .toList() ??
          const [],
    );

Map<String, dynamic> _$$SearchResultsImplToJson(_$SearchResultsImpl instance) =>
    <String, dynamic>{
      'results': instance.results,
    };

_$SearchResultItemImpl _$$SearchResultItemImplFromJson(
        Map<String, dynamic> json) =>
    _$SearchResultItemImpl(
      type: $enumDecode(_$SearchResultTypeEnumMap, json['type']),
      id: json['id'] as String,
      title: json['title'] as String,
      snippet: json['snippet'] as String? ?? '',
    );

Map<String, dynamic> _$$SearchResultItemImplToJson(
        _$SearchResultItemImpl instance) =>
    <String, dynamic>{
      'type': _$SearchResultTypeEnumMap[instance.type]!,
      'id': instance.id,
      'title': instance.title,
      'snippet': instance.snippet,
    };

const _$SearchResultTypeEnumMap = {
  SearchResultType.session: 'session',
  SearchResultType.task: 'task',
  SearchResultType.memory: 'memory',
  SearchResultType.plan: 'plan',
};

_$BranchInfoImpl _$$BranchInfoImplFromJson(Map<String, dynamic> json) =>
    _$BranchInfoImpl(
      name: json['name'] as String,
      isCurrent: json['is_current'] as bool? ?? false,
      isHead: json['is_head'] as bool? ?? false,
    );

Map<String, dynamic> _$$BranchInfoImplToJson(_$BranchInfoImpl instance) =>
    <String, dynamic>{
      'name': instance.name,
      'is_current': instance.isCurrent,
      'is_head': instance.isHead,
    };

_$SkillFormFieldImpl _$$SkillFormFieldImplFromJson(Map<String, dynamic> json) =>
    _$SkillFormFieldImpl(
      name: json['name'] as String,
      label: json['label'] as String,
      type: json['type'] as String? ?? 'text',
      required: json['required'] as bool? ?? false,
      defaultValue: json['default_value'] as String?,
      options: (json['options'] as List<dynamic>?)
              ?.map((e) => e as String)
              .toList() ??
          const [],
    );

Map<String, dynamic> _$$SkillFormFieldImplToJson(
        _$SkillFormFieldImpl instance) =>
    <String, dynamic>{
      'name': instance.name,
      'label': instance.label,
      'type': instance.type,
      'required': instance.required,
      'default_value': instance.defaultValue,
      'options': instance.options,
    };

_$SkillUiDescriptorImpl _$$SkillUiDescriptorImplFromJson(
        Map<String, dynamic> json) =>
    _$SkillUiDescriptorImpl(
      uiType: json['ui_type'] as String? ?? 'form',
      formFields: (json['form_fields'] as List<dynamic>?)
              ?.map((e) => SkillFormField.fromJson(e as Map<String, dynamic>))
              .toList() ??
          const [],
      actions:
          (json['actions'] as List<dynamic>?)?.map((e) => e as String).toList(),
    );

Map<String, dynamic> _$$SkillUiDescriptorImplToJson(
        _$SkillUiDescriptorImpl instance) =>
    <String, dynamic>{
      'ui_type': instance.uiType,
      'form_fields': instance.formFields,
      'actions': instance.actions,
    };

_$SkillExecuteResultImpl _$$SkillExecuteResultImplFromJson(
        Map<String, dynamic> json) =>
    _$SkillExecuteResultImpl(
      output: json['output'] as String,
      success: json['success'] as bool,
      error: json['error'] as String?,
    );

Map<String, dynamic> _$$SkillExecuteResultImplToJson(
        _$SkillExecuteResultImpl instance) =>
    <String, dynamic>{
      'output': instance.output,
      'success': instance.success,
      'error': instance.error,
    };
