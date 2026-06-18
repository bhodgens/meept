//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_import

import 'package:one_of_serializer/any_of_serializer.dart';
import 'package:one_of_serializer/one_of_serializer.dart';
import 'package:built_collection/built_collection.dart';
import 'package:built_value/json_object.dart';
import 'package:built_value/serializer.dart';
import 'package:built_value/standard_json_plugin.dart';
import 'package:built_value/iso_8601_date_time_serializer.dart';
import 'package:meept_client/src/date_serializer.dart';
import 'package:meept_client/src/model/date.dart';

import 'package:meept_client/src/model/add_job_request.dart';
import 'package:meept_client/src/model/add_job_response.dart';
import 'package:meept_client/src/model/add_worker_request.dart';
import 'package:meept_client/src/model/agent_job_config.dart';
import 'package:meept_client/src/model/agent_progress_event.dart';
import 'package:meept_client/src/model/apply_improvement_request.dart';
import 'package:meept_client/src/model/approve_plan_request.dart';
import 'package:meept_client/src/model/attach_session_request.dart';
import 'package:meept_client/src/model/attendee_info.dart';
import 'package:meept_client/src/model/audit_entry.dart';
import 'package:meept_client/src/model/audit_request.dart';
import 'package:meept_client/src/model/branch_session_request.dart';
import 'package:meept_client/src/model/bus_service.dart';
import 'package:meept_client/src/model/bus_stats_response.dart';
import 'package:meept_client/src/model/cache_inspect_result.dart';
import 'package:meept_client/src/model/cache_service.dart';
import 'package:meept_client/src/model/cache_stats_response.dart';
import 'package:meept_client/src/model/calendar_event.dart';
import 'package:meept_client/src/model/calendar_service.dart';
import 'package:meept_client/src/model/cancel_request.dart';
import 'package:meept_client/src/model/cancel_task_request.dart';
import 'package:meept_client/src/model/chat_request.dart';
import 'package:meept_client/src/model/chat_response.dart';
import 'package:meept_client/src/model/chat_service.dart';
import 'package:meept_client/src/model/check_request.dart';
import 'package:meept_client/src/model/check_response.dart';
import 'package:meept_client/src/model/claim_request.dart';
import 'package:meept_client/src/model/clear_cache_request.dart';
import 'package:meept_client/src/model/command_history.dart';
import 'package:meept_client/src/model/compact_session_request.dart';
import 'package:meept_client/src/model/complete_request.dart';
import 'package:meept_client/src/model/config.dart';
import 'package:meept_client/src/model/confirm_plan_request.dart';
import 'package:meept_client/src/model/create_event_request.dart';
import 'package:meept_client/src/model/create_pipeline_request.dart';
import 'package:meept_client/src/model/create_plan_request.dart';
import 'package:meept_client/src/model/create_session_request.dart';
import 'package:meept_client/src/model/create_task_request.dart';
import 'package:meept_client/src/model/daemon_service.dart';
import 'package:meept_client/src/model/daemon_status.dart';
import 'package:meept_client/src/model/delete_pipeline_request.dart';
import 'package:meept_client/src/model/delete_session_request.dart';
import 'package:meept_client/src/model/delete_task_request.dart';
import 'package:meept_client/src/model/detach_session_request.dart';
import 'package:meept_client/src/model/enable_job_request.dart';
import 'package:meept_client/src/model/enqueue_request.dart';
import 'package:meept_client/src/model/execute_request.dart';
import 'package:meept_client/src/model/execute_result.dart';
import 'package:meept_client/src/model/fail_request.dart';
import 'package:meept_client/src/model/follow_up_request.dart';
import 'package:meept_client/src/model/fork_session_request.dart';
import 'package:meept_client/src/model/generate_improvement_request.dart';
import 'package:meept_client/src/model/get_messages_request.dart';
import 'package:meept_client/src/model/get_request.dart';
import 'package:meept_client/src/model/get_session_request.dart';
import 'package:meept_client/src/model/get_task_request.dart';
import 'package:meept_client/src/model/get_task_steps_request.dart';
import 'package:meept_client/src/model/get_tree_request.dart';
import 'package:meept_client/src/model/invalidate_request.dart';
import 'package:meept_client/src/model/list_branches_request.dart';
import 'package:meept_client/src/model/list_events_request.dart';
import 'package:meept_client/src/model/list_events_response.dart';
import 'package:meept_client/src/model/list_jobs_response.dart';
import 'package:meept_client/src/model/list_options.dart';
import 'package:meept_client/src/model/list_request.dart';
import 'package:meept_client/src/model/list_sessions_request.dart';
import 'package:meept_client/src/model/memory_query_request.dart';
import 'package:meept_client/src/model/memory_result.dart';
import 'package:meept_client/src/model/memory_service.dart';
import 'package:meept_client/src/model/model_info.dart';
import 'package:meept_client/src/model/model_service.dart';
import 'package:meept_client/src/model/paginated_response.dart';
import 'package:meept_client/src/model/pause_job_request.dart';
import 'package:meept_client/src/model/pipeline.dart';
import 'package:meept_client/src/model/pipeline_info.dart';
import 'package:meept_client/src/model/pipeline_list_request.dart';
import 'package:meept_client/src/model/pipeline_service.dart';
import 'package:meept_client/src/model/pipeline_status_response.dart';
import 'package:meept_client/src/model/pipeline_step.dart';
import 'package:meept_client/src/model/pipeline_step_status.dart';
import 'package:meept_client/src/model/plan_service.dart';
import 'package:meept_client/src/model/project_service.dart';
import 'package:meept_client/src/model/provider_info.dart';
import 'package:meept_client/src/model/publish_request.dart';
import 'package:meept_client/src/model/queue_service.dart';
import 'package:meept_client/src/model/queue_status_request.dart';
import 'package:meept_client/src/model/queue_status_response.dart';
import 'package:meept_client/src/model/register_project_request.dart';
import 'package:meept_client/src/model/reject_improvement_request.dart';
import 'package:meept_client/src/model/reject_plan_request.dart';
import 'package:meept_client/src/model/remove_job_request.dart';
import 'package:meept_client/src/model/remove_worker_request.dart';
import 'package:meept_client/src/model/resume_job_request.dart';
import 'package:meept_client/src/model/resume_session_request.dart';
import 'package:meept_client/src/model/retry_request.dart';
import 'package:meept_client/src/model/revise_plan_request.dart';
import 'package:meept_client/src/model/runtime_service.dart';
import 'package:meept_client/src/model/runtime_status_response.dart';
import 'package:meept_client/src/model/scale_workers_request.dart';
import 'package:meept_client/src/model/scheduler_service.dart';
import 'package:meept_client/src/model/search_request.dart';
import 'package:meept_client/src/model/search_result.dart';
import 'package:meept_client/src/model/search_service.dart';
import 'package:meept_client/src/model/security_service.dart';
import 'package:meept_client/src/model/self_improve_service.dart';
import 'package:meept_client/src/model/service_error.dart';
import 'package:meept_client/src/model/service_registry.dart';
import 'package:meept_client/src/model/session_service.dart';
import 'package:meept_client/src/model/set_project_request.dart';
import 'package:meept_client/src/model/shard_detail.dart';
import 'package:meept_client/src/model/shell_job_config.dart';
import 'package:meept_client/src/model/skill_info.dart';
import 'package:meept_client/src/model/skill_ui_descriptor.dart';
import 'package:meept_client/src/model/skills_get_request.dart';
import 'package:meept_client/src/model/skills_list_request.dart';
import 'package:meept_client/src/model/skills_service.dart';
import 'package:meept_client/src/model/status_request.dart';
import 'package:meept_client/src/model/status_response.dart';
import 'package:meept_client/src/model/steer_request.dart';
import 'package:meept_client/src/model/task_list_request.dart';
import 'package:meept_client/src/model/task_service.dart';
import 'package:meept_client/src/model/template_info.dart';
import 'package:meept_client/src/model/templates_clear_request.dart';
import 'package:meept_client/src/model/templates_clear_result.dart';
import 'package:meept_client/src/model/templates_get_request.dart';
import 'package:meept_client/src/model/templates_invoke_request.dart';
import 'package:meept_client/src/model/templates_invoke_result.dart';
import 'package:meept_client/src/model/templates_list_request.dart';
import 'package:meept_client/src/model/templates_service.dart';
import 'package:meept_client/src/model/terminal_service.dart';
import 'package:meept_client/src/model/terminal_session.dart';
import 'package:meept_client/src/model/trigger_request.dart';
import 'package:meept_client/src/model/ui_action_def.dart';
import 'package:meept_client/src/model/ui_field_def.dart';
import 'package:meept_client/src/model/update_event_request.dart';
import 'package:meept_client/src/model/update_status_request.dart';
import 'package:meept_client/src/model/update_task_request.dart';
import 'package:meept_client/src/model/validate_improvement_request.dart';
import 'package:meept_client/src/model/vector_search_request.dart';
import 'package:meept_client/src/model/vector_search_result.dart';
import 'package:meept_client/src/model/vector_stats.dart';
import 'package:meept_client/src/model/vector_store_request.dart';
import 'package:meept_client/src/model/ws_subscribe_message.dart';
import 'package:meept_client/src/model/ws_unsubscribe_message.dart';
import 'package:meept_client/src/model/worker_service.dart';
import 'package:meept_client/src/model/worker_stats_response.dart';

part 'serializers.g.dart';

@SerializersFor([
  AddJobRequest,
  AddJobResponse,
  AddWorkerRequest,
  AgentJobConfig,
  AgentProgressEvent,
  ApplyImprovementRequest,
  ApprovePlanRequest,
  AttachSessionRequest,
  AttendeeInfo,
  AuditEntry,
  AuditRequest,
  BranchSessionRequest,
  BusService,
  BusStatsResponse,
  CacheInspectResult,
  CacheService,
  CacheStatsResponse,
  CalendarEvent,
  CalendarService,
  CancelRequest,
  CancelTaskRequest,
  ChatRequest,
  ChatResponse,
  ChatService,
  CheckRequest,
  CheckResponse,
  ClaimRequest,
  ClearCacheRequest,
  CommandHistory,
  CompactSessionRequest,
  CompleteRequest,
  Config,
  ConfirmPlanRequest,
  CreateEventRequest,
  CreatePipelineRequest,
  CreatePlanRequest,
  CreateSessionRequest,
  CreateTaskRequest,
  DaemonService,
  DaemonStatus,
  DeletePipelineRequest,
  DeleteSessionRequest,
  DeleteTaskRequest,
  DetachSessionRequest,
  EnableJobRequest,
  EnqueueRequest,
  ExecuteRequest,
  ExecuteResult,
  FailRequest,
  FollowUpRequest,
  ForkSessionRequest,
  GenerateImprovementRequest,
  GetMessagesRequest,
  GetRequest,
  GetSessionRequest,
  GetTaskRequest,
  GetTaskStepsRequest,
  GetTreeRequest,
  InvalidateRequest,
  ListBranchesRequest,
  ListEventsRequest,
  ListEventsResponse,
  ListJobsResponse,
  ListOptions,
  ListRequest,
  ListSessionsRequest,
  MemoryQueryRequest,
  MemoryResult,
  MemoryService,
  ModelInfo,
  ModelService,
  PaginatedResponse,
  PauseJobRequest,
  Pipeline,
  PipelineInfo,
  PipelineListRequest,
  PipelineService,
  PipelineStatusResponse,
  PipelineStep,
  PipelineStepStatus,
  PlanService,
  ProjectService,
  ProviderInfo,
  PublishRequest,
  QueueService,
  QueueStatusRequest,
  QueueStatusResponse,
  RegisterProjectRequest,
  RejectImprovementRequest,
  RejectPlanRequest,
  RemoveJobRequest,
  RemoveWorkerRequest,
  ResumeJobRequest,
  ResumeSessionRequest,
  RetryRequest,
  RevisePlanRequest,
  RuntimeService,
  RuntimeStatusResponse,
  ScaleWorkersRequest,
  SchedulerService,
  SearchRequest,
  SearchResult,
  SearchService,
  SecurityService,
  SelfImproveService,
  ServiceError,
  ServiceRegistry,
  SessionService,
  SetProjectRequest,
  ShardDetail,
  ShellJobConfig,
  SkillInfo,
  SkillUIDescriptor,
  SkillsGetRequest,
  SkillsListRequest,
  SkillsService,
  StatusRequest,
  StatusResponse,
  SteerRequest,
  TaskListRequest,
  TaskService,
  TemplateInfo,
  TemplatesClearRequest,
  TemplatesClearResult,
  TemplatesGetRequest,
  TemplatesInvokeRequest,
  TemplatesInvokeResult,
  TemplatesListRequest,
  TemplatesService,
  TerminalService,
  TerminalSession,
  TriggerRequest,
  UIActionDef,
  UIFieldDef,
  UpdateEventRequest,
  UpdateStatusRequest,
  UpdateTaskRequest,
  ValidateImprovementRequest,
  VectorSearchRequest,
  VectorSearchResult,
  VectorStats,
  VectorStoreRequest,
  WSSubscribeMessage,
  WSUnsubscribeMessage,
  WorkerService,
  WorkerStatsResponse,
])
Serializers serializers = (_$serializers.toBuilder()
      ..add(const OneOfSerializer())
      ..add(const AnyOfSerializer())
      ..add(const DateSerializer())
      ..add(Iso8601DateTimeSerializer())
    ).build();

Serializers standardSerializers =
    (serializers.toBuilder()..addPlugin(StandardJsonPlugin())).build();
